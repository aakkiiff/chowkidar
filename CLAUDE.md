# Chowkidar

Distributed infrastructure monitoring: Go agent + Go API server + React dashboard. Replaces Prometheus/Loki/Grafana for small-to-mid deployments.

# requirement
- this project should be very lightweight
- agent should be able to scrape docker/system/kubernetes metrics and logs with minimal overhead
- server should store data efficiently and support real-time streaming to frontend
- frontend should be responsive and provide clear insights into container health and logs

## Layout

```
server/          Go API server (SQLite, HTTP probes, log storage)
agent/           Go monitoring agent (Docker metrics + log streaming)
frontend/        React 19 + TypeScript + Vite dashboard
loadtest/        Load testing harness (agent simulator, orchestrator)
```

## Tech stack

- **Server**: Go 1.25, modernc.org/sqlite (pure-Go, no CGO), net/http, JWT auth
- **Agent**: Go 1.25, Docker Engine API, gopsutil
- **Frontend**: React 19, TypeScript 6, Vite 8, react-router-dom v7, Chart.js
- **Deployment**: Docker Compose (server + frontend), agent runs standalone or as container

## Build & run

```bash
# Server
cd server && go build -o chowkidar .
./chowkidar   # reads .env from cwd, server/ subdir, or binary dir

# Agent
cd agent && go build -o chowkidar-agent .
./chowkidar-agent   # reads .env from cwd

# Frontend
cd frontend && npm install && npm run dev    # dev at :5173, proxies /api → :8080
cd frontend && npm run build                 # production build, served by nginx

# Docker Compose (server + frontend)
docker compose up --build
```

## Configuration

### Server (`server/.env`) — all required, no defaults

```
SERVER_PORT=8080
JWT_SECRET=...
DB_PATH=./db/chowkidar.db
ADMIN_USERNAME=admin
ADMIN_PASSWORD=...
RETENTION_DAYS_CONTAINERS=7
LOG_DIR=./db/logs
LOG_RETENTION_DAYS=14
LOG_MAX_FILE_MB=100
LOG_MAX_ROTATIONS=10
```

Server fails at startup if any var is missing. No silent defaults.

### Agent (`agent/.env`)

```
AGENT_IDENTITY=my-server-01      # required
AGENT_TOKEN=agt_...              # required
SERVER_URL=http://localhost:8080  # required
COLLECT_INTERVAL=10s              # optional, default 10s
LOG_BATCH_MS=200                  # optional, default 200ms
LOG_BATCH_BYTES=8192              # optional, default 8KB
```

## Architecture

### Agent → Server: two independent paths

1. **Metrics**: `POST /api/v1/report` every `COLLECT_INTERVAL`. Synchronous, 4 retries with backoff. Missed ticks = gaps in graph (no queue).
2. **Logs**: `POST /api/v1/logs/ingest` streaming NDJSON. Bounded channel (20,480 lines), drop-oldest on overflow. 30s POST timeout, infinite retry on 5xx/network.

Agent stays healthy when server is unreachable. No cascading failure to monitored containers.

### Server subsystems

- **Store** (`server/store/`): SQLite with WAL mode. Auto-migrates on startup. Container metrics rolled up to 1-min averages every minute.
- **Log store** (`server/logstore/`): Per `(agent, container)` active file + gzipped rotations. Pruned hourly by age and count.
- **Prober** (`server/probe/`): Server-side HTTP endpoint probing. Stores probe results (1h retention) and incidents (configurable retention, default 30d).
- **Alerts** (`server/alert/`): Evaluates rules every 15s. Sustained threshold → fires webhook (Discord). SSE stream to frontend for toasts.
- **Log broker** (`server/logbroker/`): Fans ingested log lines to SSE subscribers (real-time tail in dashboard).

### Frontend routing

- `/login` — auth page
- `/agents` — agent list
- `/agents/:id/:tab` — agent detail (overview, containers, endpoints, logs, alerts)
- `/agents/:id/endpoints/:eid` — endpoint detail (uptime gantt, latency chart, incidents)
- `/settings` — global settings (endpoint probe interval, incident retention, alert timing, webhooks, users)

### Auth model

- **Dashboard users**: JWT (admin or developer role). Admin sees all endpoints; both see agents/containers/logs.
- **Agents**: Bearer token (`agt_...`) issued at registration. Used for `/api/v1/report` and `/api/v1/logs/ingest`.

## Key patterns

- Server config is strict: `config.Load()` returns error listing all missing vars. No defaults.
- Agent config uses defaults for optional vars, but `main.go` validates required ones (IDENTITY, TOKEN, SERVER_URL) and fatals.
- SQLite migrations run in a single transaction on startup. Schema changes are append-only `CREATE TABLE IF NOT EXISTS`.
- Endpoint uptime model: stores state transitions (incidents), not samples. Up periods = gaps between incidents. Probe data kept 1h (hardcoded) for latency charts only.
- Log files: append-only NDJSON, rotate at `LOG_MAX_FILE_MB`, gzip on rotation, prune oldest by count and age.
- Frontend API client at `frontend/src/api/client.ts` — single `request<T>()` helper, handles 401 → session clear.
- Charts use inline SVG (gantt, latency) except Chart.js for container history graphs.

## Log sizing reference

Per-container hard ceiling: `LOG_MAX_FILE_MB + LOG_MAX_ROTATIONS × ~10 MB gzipped ≈ 200-300 MB`.

See detailed tier calculations in:
- `server/GUIDES/LOG-STORAGE.md` — disk planning, rotation math, sensitivity to line size
- `agent/GUIDES/SERVER-UNREACHABLE.md` — behavior during outages
- `agent/GUIDES/Agent->Server.md` — communication details, retry/backoff specs

## Testing

```bash
# Server
cd server && go test ./...

# Agent
cd agent && go test ./...

# Frontend
cd frontend && npm run lint
```

Load test harness in `loadtest/` — simulates N agents sending metrics + logs concurrently.
