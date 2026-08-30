# dydns

Dynamic DNS updater for [NameSilo](https://www.namesilo.com). It resolves the host's
public IP on an interval and, when it differs, rewrites a single `A` record via the
NameSilo API. Idempotent: if the record already matches, nothing is written.

## How it works

Each cycle:

1. `GET $PUBLIC_IP_URL` to resolve the current public IP.
2. `dnsListRecords` for `$NAMESILO_DOMAIN`, and find the `A` record whose host matches
   `$NAMESILO_HOST`.
3. If the record's value already equals the public IP, stop.
4. Otherwise `dnsUpdateRecord` with the new value and `$NAMESILO_RECORD_TTL`.

Each cycle is bounded by `$NAMESILO_UPDATE_TIMEOUT`. A cycle failure is logged and
counted; the loop then waits `$NAMESILO_UPDATE_INTERVAL` and tries again. `SIGINT` and
`SIGTERM` cancel the in-flight cycle and shut down cleanly.

## Configuration

All configuration is via environment variables.

| Variable | Required | Default | Description |
|---|---|---|---|
| `NAMESILO_API_KEY` | yes | — | NameSilo API key |
| `NAMESILO_DOMAIN` | yes | — | Domain the record belongs to, e.g. `example.com` |
| `NAMESILO_HOST` | yes | — | Host of the `A` record to update, as NameSilo reports it |
| `NAMESILO_UPDATE_INTERVAL` | no | `24h` | Time between update cycles |
| `NAMESILO_UPDATE_TIMEOUT` | no | `30s` | Deadline for a single cycle |
| `NAMESILO_RECORD_TTL` | no | `7207` | TTL written with the record |
| `PUBLIC_IP_URL` | no | `https://ifconfig.me/ip` | Endpoint returning the public IP as a bare string |
| `NAMESILO_BASE_URL` | no | `https://www.namesilo.com/api` | API root. Override to point at a stub for testing |
| `DYDNS_METRICS_PORT` | no | `8080` | Port for the metrics and health endpoints |
| `LOG_LEVEL` | no | `info` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` |
| `LOG_FORMAT` | no | `auto` | `auto`, `json` or `console`. `auto` uses `console` only when stdout is a terminal |

## Endpoints

Served on `:$DYDNS_METRICS_PORT`.

| Path | Description |
|---|---|
| `/metrics` | Prometheus metrics |
| `/healthz` | Liveness. Always `200` while the process is running |
| `/readyz` | Readiness. `503` until the first cycle succeeds, then `200` |

`/healthz` intentionally does not fail on a stale record: a NameSilo outage would
crashloop the pod without fixing anything. Alert on `dydns_last_success_timestamp_seconds`
instead.

## Metrics

All metrics are namespaced `dydns_`, alongside the default Go and process collectors.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `dydns_build_info` | gauge | `version`, `commit`, `go_version` | Always 1 |
| `dydns_target_info` | gauge | `domain`, `host` | Always 1 |
| `dydns_public_ip_info` | gauge | `ip` | Always 1. Reset each check, so exactly one series exists |
| `dydns_update_cycles_total` | counter | — | Cycles attempted |
| `dydns_update_cycle_errors_total` | counter | `stage` | Failed cycles, by the stage that failed |
| `dydns_update_cycle_duration_seconds` | histogram | — | Duration of a full cycle |
| `dydns_last_success_timestamp_seconds` | gauge | — | Unix time of the last successful cycle; 0 if none |
| `dydns_last_change_timestamp_seconds` | gauge | — | Unix time the record was last rewritten |
| `dydns_consecutive_failures` | gauge | — | Back-to-back failures; reset to 0 on success |
| `dydns_record_up_to_date` | gauge | — | 1 if the record matches the public IP, 0 otherwise |
| `dydns_record_changes_total` | counter | — | Successful record rewrites |
| `dydns_namesilo_requests_total` | counter | `operation`, `reply_code` | API requests. `reply_code` is the NameSilo code, or `error` for transport/decode failures |
| `dydns_namesilo_request_duration_seconds` | histogram | `operation` | API request duration |
| `dydns_public_ip_checks_total` | counter | `result` | Public IP lookups, `success` or `error` |
| `dydns_public_ip_check_duration_seconds` | histogram | — | Public IP lookup duration |

`stage` is one of `public_ip`, `list_records`, `list_records_reply`, `record_not_found`,
`update_record`, `update_record_reply`. Every label combination is materialized at zero
at startup, so `rate()` and `absent()` behave before the first event.

Note that `dydns_record_up_to_date` is left untouched when a cycle fails before it can
read the record — a NameSilo blip says nothing about whether the record is correct.

## Dashboard

A Grafana dashboard is in [`dashboards/`](dashboards/), versioned alongside the
metrics it queries. See [`dashboards/README.md`](dashboards/README.md) for how to
load it and why it looks the way it does.

### Alerting

The one to page on is staleness, not individual failures — a single failed cycle is
retried on the next interval:

```yaml
- alert: DydnsStale
  expr: time() - dydns_last_success_timestamp_seconds > 3 * 86400
  for: 15m
```

## Build and run

```sh
make build          # -> bin/dydns, version-stamped from git
make test           # go test -race -cover ./...
make vet
make dbuild         # docker image dydns:dev
```

```sh
NAMESILO_API_KEY=... \
NAMESILO_DOMAIN=example.com \
NAMESILO_HOST=home.example.com \
NAMESILO_UPDATE_INTERVAL=1h \
bin/dydns
```

Container images are published to `ghcr.io/setheck/dydns`. Kubernetes manifests live in
the `k8-apps` repo under `dydns/`.

To exercise the whole thing locally without touching real DNS, point it at a stub with
`NAMESILO_BASE_URL` and `PUBLIC_IP_URL` — see `dashboards/README.md`.
