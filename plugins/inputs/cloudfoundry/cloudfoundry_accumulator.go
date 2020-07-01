package cloudfoundry

import (
	"fmt"
	"strings"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/influxdata/telegraf"
)

type Accumulator struct {
	telegraf.Accumulator
}

func (a *Accumulator) AddEnvelope(env *loggregator_v2.Envelope) {
	ts := time.Unix(0, env.GetTimestamp()).UTC()

	tags := env.GetTags()

	switch m := env.GetMessage().(type) {
	case *loggregator_v2.Envelope_Log:
		flds := map[string]interface{}{
			"message":       env.GetLog().Payload,
			"facility_code": int(1),
			"severity_code": formatSeverity(env.GetLog().Type),
			"procid": formatProcID(
				env.Tags["source_type"],
				env.InstanceId,
			),
			"version": int(1),
		}
		tags["hostname"] = formatHostname(env)
		tags["appname"] = tags["app_name"]
		a.AddFields("cloudfoundry", flds, tags, ts)
	case *loggregator_v2.Envelope_Counter:
		name := m.Counter.GetName()
		a.AddCounter("cloudfoundry", map[string]interface{}{
			name: m.Counter.GetTotal(),
		}, tags, ts)
	case *loggregator_v2.Envelope_Gauge:
		flds := map[string]interface{}{}
		for name, gauge := range m.Gauge.GetMetrics() {
			flds[name] = gauge.GetValue()
		}
		a.AddGauge("cloudfoundry", flds, tags, ts)
	case *loggregator_v2.Envelope_Timer:
		name := m.Timer.GetName()
		flds := map[string]interface{}{
			fmt.Sprintf("%s_start", name):    m.Timer.GetStart(),
			fmt.Sprintf("%s_stop", name):     m.Timer.GetStop(),
			fmt.Sprintf("%s_duration", name): m.Timer.GetStop() - m.Timer.GetStart(),
		}
		a.AddFields("cloudfoundry", flds, tags, ts)
	case *loggregator_v2.Envelope_Event:
		// ignore events
	default:
		a.AddError(fmt.Errorf("cannot convert envelope %v to telegraf metric", m))
	}

}

// formatSeverity sets the syslog-compatible severity code based on the log stream
func formatSeverity(logType loggregator_v2.Log_Type) int {
	switch logType {
	case loggregator_v2.Log_OUT:
		return 6
	case loggregator_v2.Log_ERR:
		return 3
	default:
		return 5
	}
}

// formatProcID creates a string replicating the form of procid when using
// a cloudfoundry syslog-drain
func formatProcID(sourceType, sourceInstance string) string {
	sourceType = strings.ToUpper(sourceType)
	if sourceInstance != "" {
		tmp := make([]byte, 0, 3+len(sourceType)+len(sourceInstance))
		tmp = append(tmp, '[')
		tmp = append(tmp, []byte(strings.Replace(sourceType, " ", "-", -1))...)
		tmp = append(tmp, '/')
		tmp = append(tmp, []byte(sourceInstance)...)
		tmp = append(tmp, ']')

		return string(tmp)
	}

	return fmt.Sprintf("[%s]", sourceType)
}

func formatHostname(env *loggregator_v2.Envelope) string {
	hostname := env.GetSourceId()
	envTags := env.GetTags()
	orgName, orgOk := envTags["organization_name"]
	spaceName, spaceOk := envTags["space_name"]
	appName, appOk := envTags["app_name"]
	if orgOk || spaceOk || appOk {
		hostname = fmt.Sprintf("%s.%s.%s",
			sanitize(orgName),
			sanitize(spaceName),
			sanitize(appName),
		)
	}
	return hostname
}

func sanitize(s string) string {
	s = Spaces.ReplaceAllString(s, "-")
	s = InvalidCharacters.ReplaceAllString(s, "")
	s = TrailingDashes.ReplaceAllString(s, "")
	return s
}
