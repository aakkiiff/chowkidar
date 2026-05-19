<div align="center">

# Chowkidar

**Lightweight infrastructure monitoring for small and mid-sized deployments.**

One Go binary per role. One SQLite file. One Docker Compose command to deploy.
Replaces the Prometheus + Grafana + Loki + Alertmanager stack when you don't
need all that — and don't want to operate all that.

[![Go](https://img.shields.io/badge/go-1.25.9-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/react-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![SQLite](https://img.shields.io/badge/sqlite-WAL-003B57?logo=sqlite&logoColor=white)](https://sqlite.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

---

## Why Chowkidar

| | Chowkidar | Prometheus stack |
|--|-----------|------------------|
| Components to operate | 3 (server, agent, frontend) | 5+ (Prom, Grafana, Loki, Alertmanager, exporters) |
| Setup time | 5 minutes | days of YAML |
| Query language | none — opinionated UI | PromQL (steep learning curve) |

**Built for:** 5–50 hosts, teams without dedicated SRE, edge deployments,
self-hosted setups where "one binary" matters.

**Not built for:** PromQL flexibility, 100+ host fleets, multi-region
federation, custom metrics ecosystems.

---

## Features

### Monitoring
- **System metrics** — CPU, memory, disk per host, sampled every 10s
- **Container metrics** — per-container CPU / RAM / network / restart count via Docker Engine API
- **Container logs** — live tail with search; gzipped rotation on disk, retention by age + count
- **HTTP endpoint probes** — uptime gantt, p99 latency, incident history
- **Alerts** — sustain-window thresholds, Discord webhooks, persistent notification log
- Role-based access control with admin / developer roles, per-project permissions

---

## Quick start

### Prerequisites
- Docker + Docker Compose
- A host you want to monitor (can be the same machine)

### 1. Clone + configure

```bash
git clone https://github.com/technonext/chowkidar.git
cd chowkidar
```

Edit [docker-compose.yaml](docker-compose.yaml):

```yaml
environment:
  SERVER_PORT: "8080"
  JWT_SECRET: "change-me-to-32-chars-or-more"   # required
  DB_PATH: ./db/chowkidar.db
  RETENTION_DAYS_CONTAINERS: "7" # container metric rollup retention
  LOG_DIR: ./db/logs
  LOG_RETENTION_DAYS: "5" # max age for rotated .gz log files
  LOG_MAX_FILE_MB: "200"
  LOG_MAX_ROTATIONS: "1"
```

### 2. Start the dashboard

```bash
docker compose up -d --build
```

- Dashboard: `http://localhost:9999`
- API: `http://localhost:9998`

On first visit, the setup screen prompts you to create the admin password.

### 3. Register an agent

In the dashboard:
1. **Projects → New project**
2. Click into the project → **Add agent**
3. Copy the issued token (shown **once**)

On the host you want to monitor, create `agent/.env`:

```bash
AGENT_IDENTITY=prod-server-01           # hostname shown on dashboard
AGENT_TOKEN=agt_paste-the-token-here    # issued from dashboard
SERVER_URL=http://your-chowkidar-host:9998
COLLECT_INTERVAL=10s                    # optional
LOG_BATCH_MS=200                        # optional
LOG_BATCH_BYTES=8192                    # optional
```

Build + run the agent:

```bash
cd agent
go build -o chowkidar-agent .
./chowkidar-agent
```

Or run as a container with the Docker socket mounted using docker compose

The agent will appear in the dashboard within ~15 seconds.

---

## Configuration

### Server (`docker-compose.yaml` env)

| Variable | Default | Purpose |
|----------|---------|---------|
| `SERVER_PORT` | `8080` | HTTP listener port (inside container) |
| `JWT_SECRET` | — | **Required.** ≥ 32 chars. Signs session JWTs |
| `DB_PATH` | `./db/chowkidar.db` | SQLite file path |
| `RETENTION_DAYS_CONTAINERS` | `7` | Container metric 1m-aggregate retention |
| `RAW_RETENTION_MINUTES` | `5` | Raw sample retention (live chart head) |
| `LOG_DIR` | `./db/logs` | Log file root |
| `LOG_RETENTION_DAYS` | `5` | Max age for rotated `.gz` log files |
| `LOG_MAX_FILE_MB` | `200` | Active log file size before rotation |
| `LOG_MAX_ROTATIONS` | `1` | Number of gzipped rotations to keep |
| `COOKIE_SECURE` | `false` | Set `true` when behind HTTPS |
| `MAX_SSE_CONNS` | `200` | Cap on concurrent SSE clients |
| `GOMEMLIMIT` | `100MiB` | Go runtime soft memory cap |

Server fails fast at startup if any required variable is missing — no silent defaults.

### Agent (`agent/.env`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `AGENT_IDENTITY` | — | **Required.** Human hostname |
| `AGENT_TOKEN` | — | **Required.** Bearer token from dashboard |
| `SERVER_URL` | `http://localhost:8080` | Chowkidar server URL |
| `COLLECT_INTERVAL` | `10s` | Metric collection cadence |
| `LOG_BATCH_MS` | `200ms` | Log flush time threshold |
| `LOG_BATCH_BYTES` | `8192` | Log flush size threshold |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Monitored host                                                 │
│  ┌────────────────────────────────────────────────────────┐    │
│  │  chowkidar-agent (Go binary)                            │    │
│  │   ├─ system metrics (gopsutil)                          │    │
│  │   ├─ docker metrics (Docker Engine API)                 │    │
│  │   └─ docker logs (long-lived stream)                    │    │
│  └────────────────────────────────────────────────────────┘    │
└──────────────────┬──────────────────────────────────────────────┘
                   │  HTTP POST  +  Bearer token
                   │  • /api/v1/report           (metrics, every 10s)
                   │  • /api/v1/logs/ingest      (NDJSON stream)
                   ▼
┌─────────────────────────────────────────────────────────────────┐
│  chowkidar-server (Go binary)                                   │
│   ├─ HTTP API (REST + SSE)                                      │
│   ├─ SQLite (WAL mode)                                          │
│   ├─ Alert evaluator (every 15s)                                │
│   ├─ Endpoint prober (configurable)                             │
│   └─ Log broker + log file rotation                             │
└──────────────────┬──────────────────────────────────────────────┘
                   │  REST + SSE
                   ▼
┌─────────────────────────────────────────────────────────────────┐
│  Dashboard (React + Vite, served by nginx)                      │
└─────────────────────────────────────────────────────────────────┘
```

**Agent → server:**
- Metrics: synchronous POST every `COLLECT_INTERVAL`, 4 retries with backoff, drop tick on failure (no queue)
- Logs: bounded channel (20,480 lines), drop-oldest on overflow, exponential backoff retry on failed POSTs

The agent **never blocks** the host's Docker daemon, regardless of server state. Failure isolation by design.


## Development

### Stack
- **Server:** Go 1.25, `modernc.org/sqlite` (pure Go, CGO-free), `net/http`, `golang-jwt/v5`
- **Agent:** Go 1.25, `shirou/gopsutil/v4`, `docker/docker` client
- **Frontend:** React 19, TypeScript 6, Vite 6, Chart.js, react-router-dom v7


## License

[MIT](LICENSE)