package cloudfoundry

import (
	"fmt"
	"strings"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
)

var (
	// list of known loggregator envelope tags that suitable
	// for use as metric tags along with a normalized name
	tagSafelistMapping = map[string]string{
		"app":                 "app_name",
		"app_name":            "app_name",
		"component":           "component",
		"deployment":          "deployment",
		"instance":            "instance_id",
		"instance_id":         "instance_id",
		"ip":                  "ip",
		"job":                 "job",
		"organization_id":     "organization_id",
		"organization_name":   "organization_name",
		"origin":              "origin", // maybe same as component
		"process_id":          "process_id",
		"process_instance_id": "process_instance_id",
		"process_type":        "process_type",
		"routing_instance_id": "routing_instance_id",
		"source_id":           "source_id",
		"source_type":         "source_type",
		"space_name":          "space_name",
		"space":               "space_name",
		"space_id":            "space_id",
		"status_code":         "status_code",
	}
)

// NewMetric casts a loggregator event envelope to a telegraf.Metric
func NewMetric(env *loggregator_v2.Envelope) (telegraf.Metric, error) {
	ts := time.Unix(0, env.GetTimestamp()).UTC()

	// many loggregator tags make for poor metric tags so we safelist the
	// common suitable ones and everything else becomes a field
	tags := map[string]string{}
	flds := map[string]interface{}{}
	for k, v := range env.GetTags() {
		if strings.HasPrefix(k, "__") {
			continue
		}
		if normalizedTagName, ok := tagSafelistMapping[k]; ok {
			tags[normalizedTagName] = v
		} else {
			flds[k] = v
		}
	}

	// source id is the only stable value representing the origin of
	// the metric accross all metric types
	tags["host"] = env.GetSourceId()

	switch m := env.GetMessage().(type) {
	case *loggregator_v2.Envelope_Log:
		flds["message"] = env.GetLog().Payload
		flds["timestamp"] = env.GetTimestamp()
		flds["facility_code"] = int(1)
		flds["severity_code"] = formatSeverityCode(m.Log.Type)
		flds["procid"] = tags["source_type"]
		flds["version"] = int(1)
		tags["hostname"] = formatHostname(env)
		tags["appname"] = tags["app_name"]
		tags["severity"] = formatSeverityTag(m.Log.Type)
		tags["facility"] = "user"
		return metric.New("syslog", tags, flds, ts, telegraf.Untyped)
	case *loggregator_v2.Envelope_Counter:
		flds[m.Counter.Name] = m.Counter.Total
		return metric.New("cloudfoundry", tags, flds, ts, telegraf.Counter)
	case *loggregator_v2.Envelope_Gauge:
		flds := map[string]interface{}{}
		for name, gauge := range m.Gauge.Metrics {
			flds[name] = gauge.Value
		}
		return metric.New("cloudfoundry", tags, flds, ts, telegraf.Gauge)
	case *loggregator_v2.Envelope_Timer:
		flds := map[string]interface{}{
			fmt.Sprintf("%s_start", m.Timer.Name):    m.Timer.GetStart(),
			fmt.Sprintf("%s_stop", m.Timer.Name):     m.Timer.GetStop(),
			fmt.Sprintf("%s_duration", m.Timer.Name): m.Timer.GetStop() - m.Timer.GetStart(),
		}
		return metric.New("cloudfoundry", tags, flds, ts, telegraf.Untyped)
	case *loggregator_v2.Envelope_Event:
		flds["body"] = m.Event.GetBody()
		flds["title"] = m.Event.GetTitle()
		return metric.New("cloudfoundry", tags, flds, ts, telegraf.Untyped)
	default:
		return nil, fmt.Errorf("cannot convert envelope %T to telegraf metric", m)
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
		return "notice"
	}
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
