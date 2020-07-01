package cloudfoundry

import (
	"context"
	"sync"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8"
	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/influxdata/telegraf"
)

type CloudfoundryStream interface {
	Run(ctx context.Context)
	Stop()
}

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
	*Accumulator
	sync.Mutex
}

func (s *Stream) Run(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			req := s.newBatchRequest()
			events := s.Client.Stream(s.ctx, req)
			s.send(events)
			time.Sleep(time.Second * 10) // avoid hammering API on failure
		}
	}
}

func (s *Stream) Stop() {
	s.cancel()
}

func (s *Stream) send(events loggregator.EnvelopeStream) {
	s.Log.Debugf("started stream for %s", s.SourceID)
	defer s.Log.Debugf("closed stream for %s", s.SourceID)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			batch := events()
			if batch == nil {
				return
			}
			for _, event := range batch {
				select {
				case <-s.ctx.Done():
					return
				default:
					if event == nil {
						continue
					}
					s.AddEnvelope(event)
				}
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
