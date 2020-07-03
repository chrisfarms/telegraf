package cloudfoundry

import (
	"context"
	"sync"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8"
	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/influxdata/telegraf"
)

type StreamConfig struct {
	SourceID string
	ShardID  string
	Counters bool
	Timers   bool
	Gauges   bool
	Events   bool
	Logs     bool
}

type Stream struct {
	Client CloudfoundryClient
	Log    telegraf.Logger
	ctx    context.Context
	cancel func()
	StreamConfig
	EventChan chan *loggregator_v2.Envelope
	sync.Mutex
}

func (s *Stream) Run(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Second * 1): // avoid hammering API on failure
			req := s.newBatchRequest()
			events := s.Client.Stream(s.ctx, req)
			s.send(events)
		}
	}
}

func (s *Stream) Stop() {
	s.cancel()
}

func (s *Stream) send(events loggregator.EnvelopeStream) {
	s.Log.Debugf("stream connected %s", s.SourceID)
	defer s.Log.Debugf("stream disconnected %s", s.SourceID)
	for {
		batch := events()
		if batch == nil {
			return
		}
		for _, event := range batch {
			select {
			case <-s.ctx.Done():
				return
			case s.EventChan <- event:
			}
		}
	}
}

func (s *Stream) newBatchRequest() *loggregator_v2.EgressBatchRequest {
	req := &loggregator_v2.EgressBatchRequest{
		ShardId: s.ShardID,
	}
	if s.Logs {
		req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
			SourceId: s.SourceID,
			Message: &loggregator_v2.Selector_Log{
				Log: &loggregator_v2.LogSelector{},
			},
		})
	}
	if s.Counters {
		req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
			SourceId: s.SourceID,
			Message: &loggregator_v2.Selector_Counter{
				Counter: &loggregator_v2.CounterSelector{},
			},
		})
	}
	if s.Gauges {
		req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
			SourceId: s.SourceID,
			Message: &loggregator_v2.Selector_Gauge{
				Gauge: &loggregator_v2.GaugeSelector{},
			},
		})
	}
	if s.Timers {
		req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
			SourceId: s.SourceID,
			Message: &loggregator_v2.Selector_Timer{
				Timer: &loggregator_v2.TimerSelector{},
			},
		})
	}
	if s.Events {
		req.Selectors = append(req.Selectors, &loggregator_v2.Selector{
			SourceId: s.SourceID,
			Message: &loggregator_v2.Selector_Event{
				Event: &loggregator_v2.EventSelector{},
			},
		})
	}
	return req
}
