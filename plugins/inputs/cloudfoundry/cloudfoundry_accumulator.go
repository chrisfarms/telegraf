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
	flds := map[string]interface{}{}

	tags["host"] = formatHostname(env)

	switch m := env.GetMessage().(type) {
	case *loggregator_v2.Envelope_Log:
		flds["message"] = env.GetLog().Payload
		flds["facility_code"] = int(1)
		flds["severity_code"] = formatSeverityCode(env.GetLog().Type)
		flds["procid"] = formatProcID(
			env.Tags["source_type"],
			env.InstanceId,
		)
		flds["version"] = int(1)
		tags["hostname"] = tags["host"]
		tags["appname"] = tags["app_name"]
		tags["severity"] = formatSeverityTag(env.GetLog().Type)
		tags["facility"] = "user"
		a.AddFields("syslog", flds, tags, ts)
	case *loggregator_v2.Envelope_Counter:
		flds[m.Counter.GetName()] = m.Counter.GetTotal()
		a.AddCounter("cloudfoundry", flds, tags, ts)
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
		flds["body"] = m.Event.GetBody()
		flds["title"] = m.Event.GetTitle()
		a.AddFields("cloudfoundry", flds, tags, ts)
	default:
		a.AddError(fmt.Errorf("cannot convert envelope %T to telegraf metric", m))
	}

}

// formatSeverityCode sets the syslog-compatible severity code based on the log stream
func formatSeverityCode(logType loggregator_v2.Log_Type) int {
	switch logType {
	case loggregator_v2.Log_OUT:
		return 6
	case loggregator_v2.Log_ERR:
		return 3
	default:
		return 5
	}
}

// formatSeverity sets the syslog-compatible severity code based on the log stream
func formatSeverityTag(logType loggregator_v2.Log_Type) string {
	switch logType {
	case loggregator_v2.Log_OUT:
		return "info"
	case loggregator_v2.Log_ERR:
		return "err"
	default:
		return "info"
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
