# Chowkidar Agent

Lightweight monitoring agent that collects system and container metrics and reports to Chowkidar Server.

## Architecture

```
agent/
├── collect/
│   ├── system.go     # System metrics (CPU, RAM, disk)
│   └── docker.go     # Container metrics via Docker API
├── report/
│   └── reporter.go   # Send metrics to server
├── config/
│   └── config.go     # Configuration from env
└── types/
    └── types.go        # Shared types
```

## Environment Variables

Required:
```bash
AGENT_IDENTITY=my-server-01          # Unique identifier for this agent
AGENT_TOKEN=agt_abc123...             # Token from server registration
```

Optional:
```bash
SERVER_URL=http://localhost:8080      # Chowkidar server URL
AGENT_TYPE=docker                     # Agent type (docker, k8s)
COLLECT_INTERVAL=10s                   # Metrics collection interval
```

## Building

```bash
go build -o chowkidar-agent .
```

## Running

### Standalone (on host)

```bash
./chowkidar-agent
```

### As Docker Container

```bash
# Mount Docker socket from host
docker run -v /var/run/docker.sock:/var/run/docker.sock \
  -e AGENT_IDENTITY=my-server-01 \
  -e AGENT_TOKEN=agt_abc123... \
  chowkidar-agent
```

## Metrics Collected

### System Metrics
- **CPU Usage**: Average of all cores
- **Memory**: Total GB, Used GB
- **Disk**: Total GB, Used GB (root partition)

### Container Metrics
- **Name**: Container name
- **ID**: First 12 chars of container ID
- **Image**: Image name (shortened)
- **Status**: Container status (running, exited, etc.)
- **Memory**: Used MB, Limit MB
- **CPU**: 0% (requires stats API, TODO)

## Output Format

Agent sends JSON payload to `/api/v1/report`:

```json
{
  "server_name": "my-host",
  "identity": "my-server-01",
  "timestamp": "2026-04-22T10:00:00Z",
  "system": {
    "cpu_percent": 45.2,
    "mem_total_gb": 16.0,
    "mem_used_gb": 8.2,
    "disk_total_gb": 500.0,
    "disk_used_gb": 250.0
  },
  "containers": [
    {
      "name": "nginx",
      "id": "abc123def456",
      "image": "nginx:latest",
      "status": "running",
      "cpu_percent": 0,
      "mem_used_mb": 128,
      "mem_limit_mb": 512
    }
  ]
}
```

## Authorization

Bearer token in `Authorization` header:

```
Authorization: Bearer agt_abc123...
```

## Future Agent Types

- `k8s`: Kubernetes agent using client-go
- `prometheus`: Prometheus metrics agent

To add a new agent type:
1. Create `collect/k8s.go` implementing same `Collector` interface
2. Update `main.go` switch statement to handle new type
3. Add required Go dependencies
