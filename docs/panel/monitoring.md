# Monitoring

The panel answers three paths outside `/api` for monitoring. None of them reveal accounts or servers beyond counts.

| Path | Answers | Use |
|---|---|---|
| `/healthz` | `200 ok` while the process serves requests | Liveness: restart the panel when this fails |
| `/readyz` | `200 ok` when the database also answers, `503` otherwise | Readiness: send traffic only when this passes |
| `/metrics` | Prometheus metrics, with a token | Dashboards and alerts |

`panel healthcheck` asks `/readyz` on `GAMEPANEL_ADDR` and exits `0` when it is ready. The Docker image uses it as its `HEALTHCHECK`, and [Docker Compose](/panel/docker) starts Caddy only once the panel is healthy.

## Metrics

Set a long random token, and restart the panel:

```ini
GAMEPANEL_METRICS_TOKEN=<openssl rand -hex 32>
```

Without it, `/metrics` answers `404`. Scrape it with the token as a bearer token:

```yaml
scrape_configs:
  - job_name: gamepanel
    scheme: https
    authorization:
      credentials: <the token>
    static_configs:
      - targets: [panel.example.com]
```

| Metric | Type | |
|---|---|---|
| `gamepanel_build_info{version}` | gauge | Always 1; the label is the panel's version |
| `gamepanel_start_time_seconds` | gauge | When the panel started |
| `gamepanel_database_up` | gauge | 1 when the counts below could be read |
| `gamepanel_nodes`, `gamepanel_nodes_connected` | gauge | Nodes, and how many daemons are connected |
| `gamepanel_servers`, `gamepanel_servers_suspended` | gauge | Game servers |
| `gamepanel_users` | gauge | Accounts |
| `gamepanel_webhook_deliveries{status}` | gauge | `pending` (waiting for a try or retry) and `failed` |
| `gamepanel_http_request_duration_seconds{area,method,code}` | histogram | Requests by area (`api`, `daemon`, `web`, `ops`), method and status class (`2xx`...). Time is until the response starts, so long downloads and websockets do not skew it |
| `go_goroutines`, `go_memstats_heap_alloc_bytes`, `go_memstats_sys_bytes` | gauge | The process |

Alerts worth having:

```yaml
- alert: GamePanelNodeOffline
  expr: gamepanel_nodes_connected < gamepanel_nodes
  for: 5m
- alert: GamePanelWebhooksFailing
  expr: increase(gamepanel_webhook_deliveries{status="failed"}[1h]) > 0
- alert: GamePanelSlowAPI
  expr: histogram_quantile(0.95, sum by (le) (rate(gamepanel_http_request_duration_seconds_bucket{area="api"}[5m]))) > 1
  for: 10m
```

The Nodes page shows each node's daemon version and warns when it differs from the panel's: upgrade the panel and every node together.
