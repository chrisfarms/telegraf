package cloudfoundry

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8"
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

  ## retry_interval sets the delay between reconnecting failed stream
  retry_interval = "1s"

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
  types = ["counters", "timers", "gauges", "events", "logs"]
`

var (
	Spaces            = regexp.MustCompile(`\s+`)
	InvalidCharacters = regexp.MustCompile(`[^-a-zA-Z0-9]+`)
	TrailingDashes    = regexp.MustCompile(`-+$`)
)

const (
	FirehoseSourceGUID = ""
)

const (
	Counter = "counter"
	Timer   = "timer"
	Gauge   = "gauge"
	Event   = "event"
	Log     = "log"
)

var (
	validMetricTypes = []string{Counter, Timer, Gauge, Event, Log}
)

type Cloudfoundry struct {
	FirehoseEnabled   bool              `toml:"firehose_enabled"`
	DiscoveryEnabled  bool              `toml:"discovery_enabled"`
	DiscoveryInterval internal.Duration `toml:"discovery_interval"`
	RetryInterval     internal.Duration `toml:"retry_interval"`
	ShardID           string            `toml:"shard_id"`
	Types             []string          `toml:"types"`
	ClientConfig

	Log      telegraf.Logger
	client   CloudfoundryClient
	ctx      context.Context
	shutdown context.CancelFunc
	acc      telegraf.Accumulator
	streams  map[string]context.CancelFunc
	wg       sync.WaitGroup
	sync.RWMutex
}

func (_ *Cloudfoundry) Description() string {
	return "Consume metrics and logs from cloudfoundry platform"
}

// SampleConfig returns default configuration example
func (_ *Cloudfoundry) SampleConfig() string {
	return usage
}

// Gather is no-op for service input plugin, see metricWriter
func (s *Cloudfoundry) Gather(_ telegraf.Accumulator) error {
	return nil
}

// Init validates configuration and sets up the client
func (s *Cloudfoundry) Init() error {
	// validate config
	if s.GatewayAddress == "" {
		return fmt.Errorf("must provide a valid gateway_address")
	}
	if s.APIAddress == "" {
		return fmt.Errorf("must provide a valid api_address")
	}
	if (s.Username == "" || s.Password == "") && (s.ClientID == "" || s.ClientSecret == "") && (s.Token == "") {
		return fmt.Errorf("must provide either username/password or client_id/client_secret or token authentication")
	}
	if !s.FirehoseEnabled && !s.DiscoveryEnabled {
		return fmt.Errorf("must enable one of firehose_enabled or discovery_enabled")
	}
	for _, t := range s.Types {
		isValid := false
		for _, validType := range validMetricTypes {
			if t == validType {
				isValid = true
			}
		}
		if !isValid {
			return fmt.Errorf("invalid metric type '%s' must be one of %v", t, validMetricTypes)
		}
	}
	// create a client
	if s.client == nil {
		client, err := NewClient(s.ClientConfig, s.Log)
		if err != nil {
			return err
		}
		s.client = client
	}
	return nil
}

// Start configures client and starts
func (s *Cloudfoundry) Start(acc telegraf.Accumulator) error {
	// configure input plugin
	s.Log.Infof("starting the cloudfoundry service")
	if s.ShardID == "" {
		s.ShardID = "telegraf"
	}
	if len(s.Types) < 1 {
		s.Types = validMetricTypes
	}
	if s.DiscoveryInterval.Duration < 1 {
		s.DiscoveryInterval.Duration = time.Second * 60
	}
	if s.RetryInterval.Duration < 1 {
		s.RetryInterval.Duration = time.Second * 1
	}
	s.streams = map[string]context.CancelFunc{}
	s.acc = acc
	s.ctx, s.shutdown = context.WithCancel(context.Background())

	// start stream connection manager
	s.wg.Add(1)
	go s.streamConnectionManager()

	return nil
}

// Stop shutsdown all streams
func (s *Cloudfoundry) Stop() {
	s.Log.Debugf("stopping the cloudfoundry service")
	s.shutdown()
	s.wg.Wait()
	s.Log.Debugf("stopped the cloudfoundry service")
}

// streamConnectionManager ensures that relevent metrics streams are connected
func (s *Cloudfoundry) streamConnectionManager() {
	defer s.wg.Done()

	delay := time.Second * 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(delay):
			s.updateStreams()
			delay = s.DiscoveryInterval.Duration
		}
	}
}

// updateStreams syncs all active event streams with the environment
func (s *Cloudfoundry) updateStreams() {
	if s.FirehoseEnabled {
		if err := s.updateFirehoseStream(); err != nil {
			s.Log.Errorf("failed to update firehose stream: %s", err)
		}
	}
	if s.DiscoveryEnabled {
		if err := s.updateApplicationStreams(); err != nil {
			s.Log.Errorf("failed to update application streams: %s", err)
		}
	}
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

	// fetch active source ids
	s.RLock()
	sourceGUIDs := map[string]bool{}
	for guid := range s.streams {
		if guid == FirehoseSourceGUID {
			continue
		}
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

	ctx, stopStream := context.WithCancel(s.ctx)
	s.streams[sourceGUID] = stopStream

	s.wg.Add(1)
	go s.connectStream(ctx, sourceGUID)
}

// removeStream stops an event accumulation stream for the given source id
func (s *Cloudfoundry) removeStream(sourceGUID string) {
	s.Lock()
	defer s.Unlock()

	stopStream, exists := s.streams[sourceGUID]
	if exists {
		stopStream()
		delete(s.streams, sourceGUID)
	}
}

// connectStream initiates a connection to the RLP gateway and sends event
// envelopes to the input chan
func (s *Cloudfoundry) connectStream(ctx context.Context, sourceGUID string) {
	s.Log.Debugf("stream added %s", sourceGUID)
	defer s.Log.Debugf("stream removed %s", sourceGUID)
	defer s.wg.Done()
	defer s.removeStream(sourceGUID)

	delay := time.Second * 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(delay): // avoid hammering API on failure
			req := s.newBatchRequest(sourceGUID)
			stream := s.client.Stream(s.ctx, req)
			s.writeEnvelopes(stream)
			delay = s.RetryInterval.Duration
		}
	}
}

// writeEnvelopes reads each event envelope from stream and writes it to acc
func (s *Cloudfoundry) writeEnvelopes(stream loggregator.EnvelopeStream) {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			batch := stream()
			if batch == nil {
				return
			}
			for _, env := range batch {
				if env == nil {
					continue
				}
				select {
				case <-s.ctx.Done():
					return
				default:
					s.writeEnvelope(env)
				}
			}
		}
	}
}

// writeEnvelope converts the envelope to telegraf metric and adds to acc
func (s *Cloudfoundry) writeEnvelope(env *loggregator_v2.Envelope) {
	m, err := NewMetric(env)
	if err != nil {
		s.acc.AddError(err)
		return
	}
	s.acc.AddMetric(m)
}

// newBatchRequest returns a stream configuration for a given sourceID
func (s *Cloudfoundry) newBatchRequest(sourceID string) *loggregator_v2.EgressBatchRequest {
	req := &loggregator_v2.EgressBatchRequest{
		ShardId: s.ShardID,
	}
	for _, t := range s.Types {
		switch t {
		case Log:
			req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
				SourceId: sourceID,
				Message: &loggregator_v2.Selector_Log{
					Log: &loggregator_v2.LogSelector{},
				},
			})
		case Counter:
			req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
				SourceId: sourceID,
				Message: &loggregator_v2.Selector_Counter{
					Counter: &loggregator_v2.CounterSelector{},
				},
			})
		case Gauge:
			req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
				SourceId: sourceID,
				Message: &loggregator_v2.Selector_Gauge{
					Gauge: &loggregator_v2.GaugeSelector{},
				},
			})
		case Timer:
			req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
				SourceId: sourceID,
				Message: &loggregator_v2.Selector_Timer{
					Timer: &loggregator_v2.TimerSelector{},
				},
			})
		case Event:
			req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
				SourceId: sourceID,
				Message: &loggregator_v2.Selector_Event{
					Event: &loggregator_v2.EventSelector{},
				},
			})
		}
	}
	return req
}
