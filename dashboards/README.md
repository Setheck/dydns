# Dashboards

Grafana dashboards for dydns. The canonical source lives here, versioned alongside
the metrics it queries — if you rename a metric in `cmd/dydns/metrics.go`, update
the dashboard in the same commit.

| File | UID | Description |
|---|---|---|
| `dydns.json` | `dydns-overview` | Health and behaviour of the updater |

## Loading it

**Import by hand:** Grafana → Dashboards → New → Import → upload `dydns.json`, then
pick your Prometheus data source.

**Provision from a file** (`grafana.ini` dashboard provider path):

```sh
cp dashboards/dydns.json /var/lib/grafana/dashboards/
```

**kube-prometheus-stack**, whose Grafana sidecar watches for labelled ConfigMaps:

```sh
kubectl create configmap dydns-dashboard \
  --from-file=dydns.json=dashboards/dydns.json \
  --dry-run=client -o yaml \
  | kubectl label -f- --local --dry-run=client -o yaml grafana_dashboard=1 \
  > dydns-dashboard-configmap.yaml
```

The dashboard takes its data source from a `datasource` template variable, so it is
not pinned to a particular Prometheus UID.

## Layout

Reading order is top-left to bottom-right, most-actionable first.

1. **KPI row** — time since last success (lead with this), record status, current
   public IP, consecutive failures, time since last change.
2. **Cycle health** — failures attributed by stage, and cycle duration quantiles.
3. **Dependencies** — NameSilo requests by reply code, NameSilo p95 latency, and
   public IP lookups.
4. **Changes and identity** — record rewrites over time, and a build/target table.

The metric to alert on is `dydns_last_success_timestamp_seconds`, not any individual
failure counter: one failed cycle is retried on the next interval, sustained
staleness is not. See the alerting section of the top-level `README.md`.

## Design notes

These are the non-obvious choices, recorded so they are not "fixed" back:

- **Colors are selected for Grafana's dark theme** (`#181b1f` panel surface), which
  is the default and what kube-prometheus-stack ships. Grafana bakes fixed colors
  into the dashboard JSON with no per-theme mechanism, so one surface had to be
  chosen. In the light theme the palette still reads, but with less separation.

- **Categorical hues are assigned in a fixed order and validated**, not picked by
  eye. The six failure stages use slots 1–6 of the reference palette; worst adjacent
  CVD separation is ΔE 8.4, worst normal-vision ΔE 19.3, all above the floors.

- **Red/green is never the sole channel.** The obvious choice for success-vs-error
  fails colorblind separation badly (ΔE 4.1 deuteranopia), so the rate panels use
  blue/orange (ΔE 26.8) instead. Red and green appear only on single-value stat
  tiles, where a text label ("Up to date" / "Stale") carries the meaning and the
  color merely reinforces it.

- **The reply-code panel colors the outcome *class*, not the code.** Blue is 300,
  orange is a transport failure, aqua is any API-level rejection. Two rejection
  codes at once therefore share a color, with the legend and tooltip carrying the
  exact value. This is deliberate: the validated palette caps all-pairs separation
  at three slots, and reply codes are unbounded.

- **The latency ramp runs tail-brightest** — p99 takes the lightest blue step, p50
  the darkest. This inverts the usual light-to-dark convention on purpose: on a dark
  surface luminance is prominence, and running it the conventional way puts the
  quantile you actually watch in near-invisible navy. Verified by rendering it both
  ways.

- **Total cycles is a dashed gray line, not a seventh hue.** It is context for the
  failure bars, and the emphasis form keeps it from competing with them. It matters
  because flatlining cycles is a failure mode the failure bars alone cannot show.

- **Stat tiles aggregate explicitly** (`max`/`min` across instances) rather than
  letting `lastNotNull` silently pick one series when several instances are selected.
