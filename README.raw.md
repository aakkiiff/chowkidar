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
| Memory footprint | ~50 MB server | 2–4 GB Prometheus alone |
| Disk footprint | ~50 MB/day per agent | tens of GB/week |
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

### Dashboard
- Project-grouped agent view
- Real-time charts (CPU / RAM / disk per container)
- Live log tail via Server-Sent Events
- Container search + range picker (10m → 7d)
- Notification bell with persistent history + configurable retention

### Auth & access
- bcrypt passwords (12–72 chars)
- JWT in `httpOnly` cookie (5h session, `SameSite=Strict`)
- Admin / developer roles
- Per-project, per-agent permissions for developers
- Rate limiting per IP + per agent token

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
  RETENTION_DAYS_CONTAINERS: "7"
  LOG_DIR: ./db/logs
  LOG_RETENTION_DAYS: "5"
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
AGENT_TOKEN=agt_paste-the-token-here
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

Or run as a container with the Docker socket mounted:

```bash
docker run -d --name chowkidar-agent \
  --env-file agent/.env \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/technonext/chowkidar-agent:latest
```

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

**Server internals:**
- `store/` — SQLite with auto-migrations. Container metrics rolled up to 1-minute averages every minute.
- `logstore/` — per (agent, container) active log file + gzipped rotations.
- `alert/` — sustain-based threshold evaluator; fires webhooks; persists events for the notification bell.
- `probe/` — HTTP endpoint health checks with incident tracking.

---

## API reference

Public:
```
GET    /api/v1/health
GET    /api/v1/setup/status
POST   /api/v1/setup
POST   /api/v1/auth/login
POST   /api/v1/auth/logout
```

Auth (JWT cookie or bearer):
```
GET    /api/v1/auth/me
GET    /api/v1/agents
GET    /api/v1/agents/:id/containers
GET    /api/v1/agents/:id/containers/:name/logs
GET    /api/v1/agents/:id/containers/:name/logs/tail        # SSE
GET    /api/v1/projects
GET    /api/v1/projects/:id/agents
GET    /api/v1/alerts/recent
POST   /api/v1/alerts/seen
GET    /api/v1/alerts/stream                                # SSE
```

Admin only:
```
POST   /api/v1/agents/register
DELETE /api/v1/agents/:id
POST   /api/v1/projects
PATCH  /api/v1/projects/:id
PUT    /api/v1/agents/:id/project
POST   /api/v1/users
DELETE /api/v1/users/:id
GET/PUT /api/v1/settings/*
POST   /api/v1/webhooks
```

Agent token (bearer):
```
POST   /api/v1/report
POST   /api/v1/logs/ingest
```

---

## Development

### Stack
- **Server:** Go 1.25, `modernc.org/sqlite` (pure Go, CGO-free), `net/http`, `golang-jwt/v5`
- **Agent:** Go 1.25, `shirou/gopsutil/v4`, `docker/docker` client
- **Frontend:** React 19, TypeScript 6, Vite 6, Chart.js, react-router-dom v7

### Run locally without Docker

```bash
# Server
cd server
cp .env.example .env       # edit JWT_SECRET
go run .                   # listens on :8080

# Agent (register one first via dashboard)
cd agent
cp .env.example .env       # edit AGENT_TOKEN, SERVER_URL
go run .

# Frontend dev (hot reload)
cd frontend
npm install
npm run dev                # :5173, proxies /api → :8080
```

### Project layout

```
chowkidar/
├── server/
│   ├── api/           # HTTP handlers, routes, auth, rate limit
│   ├── store/         # SQLite + migrations + queries
│   ├── alert/         # evaluator, broker, webhook poster
│   ├── probe/         # endpoint prober
│   ├── logstore/      # log file rotation
│   ├── logbroker/     # in-memory log pub/sub
│   ├── clusterbroker/ # in-memory cluster snapshot pub/sub
│   ├── config/        # env loading
│   └── main.go
├── agent/
│   ├── collect/       # system + docker collectors
│   ├── logs/          # log collector + shipper
│   ├── report/        # metric reporter
│   ├── config/
│   ├── types/
│   └── main.go
├── frontend/
│   └── src/
│       ├── api/       # HTTP client
│       ├── pages/     # routed pages
│       └── components/
└── docker-compose.yaml
```

---

## Testing

```bash
# Server
cd server && go test ./...

# Agent
cd agent && go test ./...

# Frontend
cd frontend && npm test
```

Run all three before opening a PR.

---

## Security

- **Passwords:** bcrypt with default cost, 12–72 character range
- **Sessions:** JWT signed with HS256, 5-hour expiry, `httpOnly` cookie, `SameSite=Strict`
- **Agent tokens:** 160 bits of entropy, stored as SHA-256 hashes
- **Rate limiting:** per-IP for auth endpoints, per-token-hash for ingest
- **Body size limits:** 1 MB report, 4 KB mutations, 64 KB per log line
- **CORS:** none — frontend served same-origin via nginx reverse proxy
- **CSP:** default-src `'self'`, no `unsafe-inline` for scripts

**Production checklist:**
- [ ] Set `JWT_SECRET` to ≥ 32 random characters
- [ ] Terminate TLS at a reverse proxy in front of the server
- [ ] Set `COOKIE_SECURE=true`
- [ ] Set strong admin password on first-boot setup
- [ ] Restrict who has admin role

Report security issues privately — do **not** open a public issue.

---

## Roadmap

- [ ] Pluggable storage backend (Postgres)
- [ ] OpenTelemetry-compatible push protocol
- [ ] Custom alert rules in dashboard UI
- [ ] Generic webhook integrations (Slack, MS Teams, PagerDuty)
- [ ] Audit log for admin actions

---

## Contributing

Pull requests welcome. For non-trivial changes, open an issue first to discuss.

1. Fork + branch from `main`
2. Make changes
3. Run `go test ./...` (server + agent) and `npm test` (frontend)
4. Open a PR with a description of what changed and why

Coding conventions:
- Conventional commits (`feat:`, `fix:`, `chore:`, …)
- No co-author lines in commits
- Brief comments only when "why" is non-obvious — well-named code over prose

---

## License

[MIT](LICENSE)