package cloudfoundry

import (
	"sync"
	"testing"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/cloudfoundry-community/go-cfclient"
	"github.com/influxdata/telegraf/plugins/inputs/cloudfoundry/fakes"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
)

var (
	validClientCredential = ClientConfig{
		GatewayAddress: "https://gateway.addr",
		APIAddress:     "https://api.addr",
		ClientID:       "client_id",
		ClientSecret:   "client_secret",
	}
)

var (
	// soon is the timeout used for testing async actions that occur quickly
	soon = time.Millisecond * 250
	// tick is the period of checking async assertions
	tick = time.Millisecond * 100
)

func TestValidateMissingGatewayOnInit(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{})

	err := rec.Init()
	require.EqualError(t, err, "must provide a valid gateway_address")
}

func TestValidateMissingAPIOnInit(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig: ClientConfig{
			GatewayAddress: "https://gateway.addr",
		},
	})

	err := rec.Init()
	require.EqualError(t, err, "must provide a valid api_address")
}

func TestValidateMissingCredentialsOnInit(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig: ClientConfig{
			GatewayAddress: "https://gateway.addr",
			APIAddress:     "https://api.addr",
		},
	})

	err := rec.Init()
	require.EqualError(t, err, "must provide either username/password or client_id/client_secret or token authentication")
}

func TestValidateAtLeastOneStreamOnInit(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig: validClientCredential,
	})

	err := rec.Init()
	require.EqualError(t, err, "must enable one of firehose_enabled or discovery_enabled")
}

func TestValidateMetricTypeOnInit(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig:    validClientCredential,
		FirehoseEnabled: true,
		Types:           []string{"junk"},
	})

	err := rec.Init()
	require.EqualError(t, err, "invalid metric type 'junk' must be one of [counter timer gauge event log]")
}

func TestValidFirehoseConfig(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig:    validClientCredential,
		FirehoseEnabled: true,
	})

	err := rec.Init()
	require.NoError(t, err)
}

func TestValidDiscoveryConfig(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig:     validClientCredential,
		DiscoveryEnabled: true,
	})

	err := rec.Init()
	require.NoError(t, err)
}

func TestStartStopTerminates(t *testing.T) {
	rec, _ := withFakeClient(&Cloudfoundry{
		ClientConfig:    validClientCredential,
		FirehoseEnabled: true,
	})

	err := rec.Start(&testutil.Accumulator{})
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rec.Stop()
		wg.Done()
	}()
	wg.Wait() // should terminate
}

func TestRequestedMetricTypes(t *testing.T) {
	typesAndSelectors := []struct {
		RequestedType     string
		ExpectedSelectors []*loggregator_v2.Selector
	}{
		{
			RequestedType: Log,
			ExpectedSelectors: []*loggregator_v2.Selector{
				{
					Message: &loggregator_v2.Selector_Log{
						Log: &loggregator_v2.LogSelector{},
					},
				},
			},
		},
		{
			RequestedType: Counter,
			ExpectedSelectors: []*loggregator_v2.Selector{
				{
					Message: &loggregator_v2.Selector_Counter{
						Counter: &loggregator_v2.CounterSelector{},
					},
				},
			},
		},
		{
			RequestedType: Gauge,
			ExpectedSelectors: []*loggregator_v2.Selector{
				{
					Message: &loggregator_v2.Selector_Gauge{
						Gauge: &loggregator_v2.GaugeSelector{},
					},
				},
			},
		},
		{
			RequestedType: Timer,
			ExpectedSelectors: []*loggregator_v2.Selector{
				{
					Message: &loggregator_v2.Selector_Timer{
						Timer: &loggregator_v2.TimerSelector{},
					},
				},
			},
		},
		{
			RequestedType: Event,
			ExpectedSelectors: []*loggregator_v2.Selector{
				{
					Message: &loggregator_v2.Selector_Event{
						Event: &loggregator_v2.EventSelector{},
					},
				},
			},
		},
	}
	require.Lenf(t,
		typesAndSelectors,
		len(validMetricTypes),
		"should test for all supported metric types",
	)

	for _, tc := range typesAndSelectors {
		rec, fakeClient := withFakeClient(&Cloudfoundry{
			ClientConfig:    validClientCredential,
			FirehoseEnabled: true,
			Types:           []string{tc.RequestedType},
		})

		err := rec.Start(&testutil.Accumulator{})
		require.NoError(t, err)
		defer rec.Stop()

		require.Eventually(t, func() bool {
			return fakeClient.StreamCallCount() > 0
		}, soon, tick, "expected stream to be called")

		ctx, req := fakeClient.StreamArgsForCall(0)
		require.NotNil(t, ctx)
		require.NotNil(t, req)

		require.ElementsMatchf(t,
			req.Selectors,
			tc.ExpectedSelectors,
			"expected request for stream with '%s' enabled to contain correct selector",
			tc.RequestedType,
		)
	}
}

// newTestCloudfoundryPlugin returns a Cloudfoundry input plugin
// with a mock cloudfoundry client for testing
func withFakeClient(c *Cloudfoundry) (*Cloudfoundry, *fakes.FakeCloudfoundryClient) {
	fakeClient := &fakes.FakeCloudfoundryClient{}
	fakeClient.ListAppsReturns([]cfclient.App{}, nil)
	fakeClient.StreamReturnsOnCall(0, func() []*loggregator_v2.Envelope {
		return []*loggregator_v2.Envelope{}
	})
	c.client = fakeClient
	c.Log = testutil.Logger{}
	c.RetryInterval.Duration = time.Millisecond * 10
	return c, fakeClient
}
