package cloudfoundry

import (
	"testing"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/stretchr/testify/require"
)

func TestCastTimerMetric(t *testing.T) {
	ts := time.Now()
	sourceGUID := "0000-source-guid-0000"
	env := &loggregator_v2.Envelope{
		SourceId:   sourceGUID,
		InstanceId: "instance",
		Timestamp:  ts.UnixNano(),
		Message: &loggregator_v2.Envelope_Timer{
			Timer: &loggregator_v2.Timer{
				Name:  "http",
				Start: 1 * int64(time.Second),
				Stop:  8 * int64(time.Second),
			},
		},
		Tags: map[string]string{
			"uri":      "http://example/uri",
			"app_name": "app1",
		},
	}

	m, err := NewMetric(env)
	require.NoError(t, err)

	require.Equal(t, CloudfoundryMeasurement, m.Name())
	require.Equal(t, ts.UTC(), m.Time())

	require.EqualValues(t, map[string]string{
		"host":     sourceGUID,
		"app_name": "app1",
	}, m.Tags())

	require.EqualValues(t, map[string]interface{}{
		"uri":           "http://example/uri",
		"http_start":    int64(1000000000),
		"http_stop":     int64(8000000000),
		"http_duration": int64(7000000000),
	}, m.Fields())
}

func TestCastLogMetric(t *testing.T) {
	ts := time.Now()
	sourceGUID := "0000-source-guid-0000"
	env := &loggregator_v2.Envelope{
		SourceId:   sourceGUID,
		InstanceId: "instance",
		Timestamp:  ts.UnixNano(),
		Message: &loggregator_v2.Envelope_Log{
			Log: &loggregator_v2.Log{
				Type:    loggregator_v2.Log_OUT,
				Payload: []byte("stdout log msg"),
			},
		},
		Tags: map[string]string{
			"source_type":       "APP/WEB/0",
			"organization_name": "org1",
			"space_name":        "space1",
			"app_name":          "app1",
		},
	}

	m, err := NewMetric(env)
	require.NoError(t, err)

	require.Equal(t, SyslogMeasurement, m.Name())
	require.Equal(t, ts.UTC(), m.Time())

	require.EqualValues(t, map[string]string{
		"host":              sourceGUID,
		"source_type":       "APP/WEB/0",
		"organization_name": "org1",
		"space_name":        "space1",
		"app_name":          "app1",
		"hostname":          "org1.space1.app1",
		"appname":           "app1",
		"severity":          "info",
		"facility":          "user",
	}, m.Tags())

	require.EqualValues(t, map[string]interface{}{
		"message":       "stdout log msg",
		"timestamp":     ts.UnixNano(),
		"facility_code": int64(1),
		"severity_code": int64(6),
		"procid":        "APP/WEB/0",
		"version":       int64(1),
	}, m.Fields())
}

func TestCastCounterMetric(t *testing.T) {
	ts := time.Now()
	sourceGUID := "0000-source-guid-0000"
	env := &loggregator_v2.Envelope{
		SourceId:   sourceGUID,
		InstanceId: "instance",
		Timestamp:  ts.UnixNano(),
		Message: &loggregator_v2.Envelope_Counter{
			Counter: &loggregator_v2.Counter{
				Name:  "counter",
				Total: 100,
				Delta: 1,
			},
		},
		Tags: map[string]string{
			"organization_name": "org1",
			"space_name":        "space1",
			"app_name":          "app1",
		},
	}

	m, err := NewMetric(env)
	require.NoError(t, err)

	require.Equal(t, CloudfoundryMeasurement, m.Name())
	require.Equal(t, ts.UTC(), m.Time())

	require.EqualValues(t, map[string]string{
		"host":              sourceGUID,
		"organization_name": "org1",
		"space_name":        "space1",
		"app_name":          "app1",
	}, m.Tags())

	require.EqualValues(t, map[string]interface{}{
		"counter_total": uint64(100),
		"counter_delta": uint64(1),
	}, m.Fields())
}

func TestCastGaugeMetric(t *testing.T) {
	ts := time.Now()
	sourceGUID := "0000-source-guid-0000"
	env := &loggregator_v2.Envelope{
		SourceId:   sourceGUID,
		InstanceId: "instance",
		Timestamp:  ts.UnixNano(),
		Message: &loggregator_v2.Envelope_Gauge{
			Gauge: &loggregator_v2.Gauge{
				Metrics: map[string]*loggregator_v2.GaugeValue{
					"cpu": {
						Unit:  "ns",
						Value: float64(1),
					},
					"mem": {
						Unit:  "mb",
						Value: float64(1024),
					},
				},
			},
		},
		Tags: map[string]string{
			"organization_name": "org1",
			"space_name":        "space1",
			"app_name":          "app1",
		},
	}

	m, err := NewMetric(env)
	require.NoError(t, err)

	require.Equal(t, CloudfoundryMeasurement, m.Name())
	require.Equal(t, ts.UTC(), m.Time())

	require.EqualValues(t, map[string]string{
		"host":              sourceGUID,
		"organization_name": "org1",
		"space_name":        "space1",
		"app_name":          "app1",
	}, m.Tags())

	require.EqualValues(t, map[string]interface{}{
		"cpu": float64(1),
		"mem": float64(1024),
	}, m.Fields())
}
