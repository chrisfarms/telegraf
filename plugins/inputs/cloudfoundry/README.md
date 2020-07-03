# Cloudfoundry Input Plugin

The `cloudfoundry` plugin gather metrics and logs from the Cloudfoundry platform.

#### Configuration

```toml
[[inputs.cloudfoundry]]
  ## Set the HTTP gateway URL to the cloudfoundry reverse log proxy gateway
  gateway_address = "https://log-stream.your-cloudfoundry-system-domain"

  ## Set the API URL to the cloudfoundry API endpoint for your platform
  api_address = "https://api.your-cloudfoundry-system-domain"

  ## All instances with the same shard_id will receive an exclusive
  ## subset of the data
  shard_id = "telegraf"

  ## Username and password for user authentication
  username = ""
  password = ""

  ## Client ID and secret for client authentication
  # client_id = ""
  # client_secret ""

  ## Skip verification of TLS certificates (insecure!)
  # insecure_skip_verify = false

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
  counters = true
  timers = true
  gauges = true
  events = true
  logs = true
```

### Metrics

Metrics

### Logs

Logs will be collected into the syslog measurement with syslog-compatible fields


### Example Output

```
??
```
