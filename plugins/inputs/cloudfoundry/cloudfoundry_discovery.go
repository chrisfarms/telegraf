package cloudfoundry

import (
	"context"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
)

// DiscoveryStream will periodically poll the cloudfoundry API for authorized sources
// and setup event streams to accumulate events from
type DiscoveryStream struct {
	Client   CloudfoundryClient
	Log      telegraf.Logger
	Interval time.Duration
	ctx      context.Context
	cancel   func()
	streams  map[string]*Stream
	wg       sync.WaitGroup
	StreamConfig
	*Accumulator
	sync.RWMutex
}

// Start discovers, connects and accumulates events from any sources
// visible to the provided Client. Blocks until stop is called or the ctx is cancelled
func (s *DiscoveryStream) Run(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.streams = map[string]*Stream{}
	s.discover()
	s.wg.Wait()
}

// Stop cancels source discovery and closes any open accumulator streams
func (s *DiscoveryStream) Stop() {
	s.cancel()
}

// discover syncs the open event accumulator streams with sources visible to client
func (s *DiscoveryStream) discover() {
	defer s.Stop()
	delay := time.Second * 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(delay):
			s.discoverStreams()
			delay = s.Interval
		}
	}
}

func (s *DiscoveryStream) discoverStreams() {
	s.RLock()
	defer s.RUnlock()
	s.Log.Debugf("starting source stream discovery")
	defer s.Log.Debugf("stopping source stream discovery")
	apps, err := s.Client.ListApps()
	if err != nil {
		s.Log.Errorf("source stream discovery failure: %s", err)
		return
	}
	// add any missing streams
	for _, app := range apps {
		if _, isActive := s.streams[app.Guid]; !isActive {
			s.Log.Debugf("discovered stream for application %s", app.Guid)
			go s.addStream(app.Guid)
		}
	}
	// remove any old streams
	for sourceGUID := range s.streams {
		found := false
		for _, app := range apps {
			if app.Guid == sourceGUID {
				found = true
				break
			}
		}
		if !found {
			s.Log.Debugf("pruning stream for application %s", sourceGUID)
			go s.removeStream(sourceGUID)
		}
	}
}

// addStream starts an event accumulation stream for the given source id
func (s *DiscoveryStream) addStream(sourceGUID string) {
	s.Log.Debugf("adding stream for source %s", sourceGUID)
	s.Lock()
	defer s.Unlock()
	stream := &Stream{
		Client:      s.Client,
		Accumulator: s.Accumulator,
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
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.removeStream(sourceGUID)
		stream.Run(s.ctx)
	}()
}

// removeStream stops an event accumulation stream for the given source id
func (s *DiscoveryStream) removeStream(sourceGUID string) {
	s.Log.Debugf("removing stream for source %s", sourceGUID)
	s.Lock()
	defer s.Unlock()
	stream, exists := s.streams[sourceGUID]
	if exists {
		stream.Stop()
		delete(s.streams, sourceGUID)
	}
}
