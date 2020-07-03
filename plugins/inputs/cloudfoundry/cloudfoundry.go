package cloudfoundry

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

// init registers the input plugin
func init() {
	inputs.Add("cloudfoundry", func() telegraf.Input {
		return &Cloudfoundry{}
	})

}

var usage = `
  ## Set the HTTP gateway URL to the cloudfoundry reverse log proxy gateway
  gateway_address = "https://log-stream.your-cloudfoundry-system-domain"

  ## Set the API URL to the cloudfoundry API endpoint for your platform
  api_address = "https://api.your-cloudfoundry-system-domain"

  ## All instances with the same shard_id will receive an exclusive
  ## subset of the data
  shard_id = "telegraf"

  ## Username and password for user authentication
  # username = ""
  # password = ""

  ## Client ID and secret for client authentication
  # client_id = ""
  # client_secret ""

  ## Token for short lived token authentication
  # token = ""

  ## Skip verification of TLS certificates (insecure!)
  # insecure_skip_verify = false

  ## Application event source discovery
  ##
  ## When discovery is enabled (default) event source streams will be
  ## automatically discovered from the cloudfoundry API.
  ##
  ## Unlike the firehose event source, ordinary cloudfoundry user
  ## credentials can use discovery to connect to the event streams.
  discovery_enabled = true
  discovery_interval = "60s"

  ## Firehose event source
  ##
  ## The firehose event source is intended for cloudfoundry platform
  ## operators to export events from ALL components, applications and
  ## services.
  ##
  ## You must have a UAA client_id/secret with the "doppler.firehose" or
  ## "logs.admin" scope to enable the firehose event source.
  firehose_enabled = false

  ## Select which types of events to collect
  ## counters,timers and gauges report application cpu, requests and memory usage
  ## events are platform events
  ## logs will collect log lines into the "syslog" measurement.
  counters = true
  timers = true
  gauges = true
  events = true
  logs = true
`

var (
	Spaces            = regexp.MustCompile(`\s+`)
	InvalidCharacters = regexp.MustCompile(`[^-a-zA-Z0-9]+`)
	TrailingDashes    = regexp.MustCompile(`-+$`)
)

const (
	FirehoseSourceGUID = ""
)

type Cloudfoundry struct {
	FirehoseEnabled   bool              `toml:"firehose_enabled"`
	DiscoveryEnabled  bool              `toml:"discovery_enabled"`
	DiscoveryInterval internal.Duration `toml:"discovery_interval"`
	ClientConfig
	StreamConfig

	Log           telegraf.Logger
	client        CloudfoundryClient
	ctx           context.Context
	shutdown      func()
	acc           telegraf.Accumulator
	in            chan *loggregator_v2.Envelope
	streams       map[string]*Stream
	activeStreams sync.WaitGroup
	activeWorkers sync.WaitGroup
	sync.RWMutex
}

func (_ *Cloudfoundry) Description() string {
	return "Read metrics and logs from cloudfoundry platform"
}

// SampleConfig returns default configuration example
func (_ *Cloudfoundry) SampleConfig() string {
	return usage
}

// Gather is no-op for service input plugin, see metricWriter
func (s *Cloudfoundry) Gather(_ telegraf.Accumulator) error {
	return nil
}

// Start connects to cloudfoundry reverse log proxy streams to receive
// log, gauge, timer, event and counter metrics
func (s *Cloudfoundry) Start(acc telegraf.Accumulator) (err error) {
	// create a client
	client, err := NewClient(s.ClientConfig, s.Log)
	if err != nil {
		return err
	}

	// configure input plugin
	s.Log.Infof("starting the cloudfoundry service")
	if s.ShardID == "" {
		s.ShardID = "telegraf"
	}
	if s.DiscoveryInterval.Duration < 1 {
		s.DiscoveryInterval.Duration = time.Second * 60
	}
	s.streams = map[string]*Stream{}
	s.acc = acc
	s.ctx, s.shutdown = context.WithCancel(context.Background())
	s.client = client
	s.in = make(chan *loggregator_v2.Envelope, 1024)

	// start workers
	s.activeWorkers.Add(2)
	go s.readMetricsWorker()
	go s.writeMetricsWorker()

	return nil
}

// Stop shutsdown all streams
func (s *Cloudfoundry) Stop() {
	s.Log.Debugf("stopping the cloudfoundry service")
	defer s.Log.Debugf("stopped the cloudfoundry service")
	s.Log.Debugf("draining streams")
	s.shutdown()
	s.activeStreams.Wait()
	s.Log.Debugf("draining worker queues")
	close(s.in)
	s.activeWorkers.Wait()
}

// readMetricsWorker ensures that relevent metrics streams are connected
func (s *Cloudfoundry) readMetricsWorker() {
	s.Log.Debugf("starting metric reader")
	defer s.Log.Debugf("stopped metric reader")
	defer s.activeWorkers.Done()

	delay := time.Second * 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(delay):
			if err := s.updateStreams(); err != nil {
				s.Log.Errorf("update error: %s", err)
			}
			delay = s.DiscoveryInterval.Duration
		}
	}
}

// writeMetricsWorker reads events as they arrive from streams, convert to
// telegraf metric and adds to the accumulator
func (s *Cloudfoundry) writeMetricsWorker() {
	s.Log.Debugf("starting metric writer")
	defer s.Log.Debugf("stopped metric writer")
	defer s.activeWorkers.Done()

	for env := range s.in {
		if env == nil {
			continue
		}
		m, err := NewMetric(env)
		if err != nil {
			s.acc.AddError(err)
			continue
		}
		s.acc.AddMetric(m)
	}
}

// updateStreams syncs all active event streams with the environment
func (s *Cloudfoundry) updateStreams() error {
	if s.FirehoseEnabled {
		if err := s.updateFirehoseStream(); err != nil {
			return err
		}
	}
	if s.DiscoveryEnabled {
		if err := s.updateApplicationStreams(); err != nil {
			return err
		}
	}
	return nil
}

// updateFirehoseStream connects to rlp stream without source id
// this operation requires logs.admin or doppler.firehose scope
func (s *Cloudfoundry) updateFirehoseStream() error {
	s.RLock()
	_, exists := s.streams[FirehoseSourceGUID]
	s.RUnlock()
	if !exists {
		s.addStream(FirehoseSourceGUID)
	}
	return nil
}

// updateApplicationStreams discovers rlp source ids from the api
// this operation can be performed by any cloudfoundry user
func (s *Cloudfoundry) updateApplicationStreams() error {
	s.Log.Debugf("starting application stream discovery")
	defer s.Log.Debugf("completed application stream discovery")

	// fetch list of applications
	apps, err := s.client.ListApps()
	if err != nil {
		return fmt.Errorf("application stream discovery failure: %s", err)
	}

	// fetch active sources
	s.RLock()
	sourceGUIDs := map[string]bool{}
	for guid := range s.streams {
		sourceGUIDs[guid] = true
	}
	s.RUnlock()

	// add any new streams for any new applications
	for _, app := range apps {
		if _, exists := sourceGUIDs[app.Guid]; !exists {
			s.Log.Debugf("discovered stream for application %s", app.Guid)
			s.addStream(app.Guid)
		}
	}

	// remove any old streams from applications that no longer exist
	for sourceGUID := range sourceGUIDs {
		found := false
		for _, app := range apps {
			if app.Guid == sourceGUID {
				found = true
				break
			}
		}
		if !found {
			s.Log.Debugf("pruning stream for application %s", sourceGUID)
			s.removeStream(sourceGUID)
		}
	}

	return nil
}

// addStream starts an event accumulation stream for the given source id
func (s *Cloudfoundry) addStream(sourceGUID string) {
	s.Lock()
	defer s.Unlock()

	if _, exists := s.streams[sourceGUID]; exists {
		return
	}

	stream := &Stream{
		Client:    s.client,
		EventChan: s.in,
		StreamConfig: StreamConfig{
			SourceID: sourceGUID,
			ShardID:  s.ShardID,
			Counters: s.Counters,
			Timers:   s.Timers,
			Gauges:   s.Gauges,
			Events:   s.Events,
			Logs:     s.Logs,
		},
		Log: s.Log,
	}
	s.streams[sourceGUID] = stream

	s.activeStreams.Add(1)
	go func() {
		s.Log.Debugf("stream added %s", sourceGUID)
		defer s.Log.Debugf("stream removed %s", sourceGUID)
		defer s.activeStreams.Done()
		defer s.removeStream(sourceGUID)
		stream.Run(s.ctx)
	}()
}

// removeStream stops an event accumulation stream for the given source id
func (s *Cloudfoundry) removeStream(sourceGUID string) {
	s.Lock()
	defer s.Unlock()

	stream, exists := s.streams[sourceGUID]
	if exists {
		stream.Stop()
		delete(s.streams, sourceGUID)
	}
}
