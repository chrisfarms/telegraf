package cloudfoundry

import (
	"context"
	"regexp"
	"sync"
	"time"

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

  ## By default event streams are discovered from the API periodically
  ## If you have a UAA client_id/secret with the "doppler.firehose" or
  ## "logs.admin" scope then you can instead enable the firehose you will
  ## recieve events from ALL available components, applications and services.
  # firehose = false

  ## When firehose=false source event streams will be periodically updated
  ## set the
  discovery_interval = "60s"

  ## Select which types of events to collect
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

type Cloudfoundry struct {
	Firehose          bool              `toml:"firehose"`
	DiscoveryInterval internal.Duration `toml:"discovery_interval"`
	ClientConfig
	StreamConfig

	Log    telegraf.Logger
	ctx    context.Context
	cancel func()
	acc    telegraf.Accumulator
	wg     sync.WaitGroup
	stream CloudfoundryStream
	sync.Mutex
}

func (_ *Cloudfoundry) Description() string {
	return "Read metrics and logs from cloudfoundry platform"
}

// SampleConfig returns default configuration example
func (_ *Cloudfoundry) SampleConfig() string {
	return usage
}

// Gather is a no-op see collect() for metrics collection
func (s *Cloudfoundry) Gather(_ telegraf.Accumulator) error {
	return nil
}

// Start connects to cloudfoundry reverse log proxy endpoints to receive
// log, gauge, timer and counter events
func (s *Cloudfoundry) Start(acc telegraf.Accumulator) (err error) {
	s.Lock()
	defer s.Unlock()
	s.Log.Infof("starting the cloudfoundry service")
	if s.ShardID == "" {
		s.ShardID = "telegraf"
	}
	if s.DiscoveryInterval.Duration < 1 {
		s.DiscoveryInterval.Duration = time.Second * 60
	}
	s.acc = acc
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// create a client
	client, err := NewClient(s.ClientConfig, s.Log)
	if err != nil {
		return err
	}

	if s.Firehose {
		s.stream = &Stream{
			Client:       client,
			StreamConfig: s.StreamConfig,
			Accumulator:  &Accumulator{acc},
			Log:          s.Log,
		}
	} else {
		s.stream = &DiscoveryStream{
			Client:       client,
			StreamConfig: s.StreamConfig,
			Accumulator:  &Accumulator{acc},
			Log:          s.Log,
			Interval:     s.DiscoveryInterval.Duration,
		}
	}
	s.Log.Debugf("cfg %v", s.stream)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.stream.Run(s.ctx)
	}()

	return nil
}

// Stop shutsdown all streams
func (s *Cloudfoundry) Stop() {
	s.Lock()
	defer s.Unlock()
	s.Log.Infof("stopping the cloudfoundry service")
	s.cancel()
	s.wg.Wait()
}
