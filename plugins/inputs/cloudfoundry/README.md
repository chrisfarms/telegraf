# Cloudfoundry Input Plugin

The `cloudfoundry` plugin gather metrics and logs from the Cloudfoundry platform.

#### Configuration

```toml
[[inputs.cloudfoundry]]
  ## Set rlp_endpoint to the URL of the cloudfoundry reverse log proxy
  rlp_endpoint = "https://log-stream.your-cloudfoundry-system-domain"

  ## Set api_endpoint tp the URL of the cloudfoundry api for your platform
  api_endpoint = "https://api.your-cloudfoundry-system-domain"

  ## All instances with the same shard_id will receive an exclusive
  ## subset of the data
  shard_id = "telegraf"

  ## Username and password for user authentication
  # username = ""
  # password = ""

  ## Client ID and secret for client authentication
  # client_id = ""
  # client_secret ""

  ## By default event streams are discovered from the API periodically
  ## If you have a UAA client_id/secret with the "doppler.firehose" or
  ## "logs.admin" scope then you can instead enable the firehose you will
  ## recieve events from ALL available components, applications and services.
  firehose = false

  ## Select which types of events to collect
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
