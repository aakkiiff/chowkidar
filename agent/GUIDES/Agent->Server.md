# Agent → Server Communication

Two independent paths. Each has own goroutine, HTTP client, payload format, retry/backoff/timeout. Outage on one does not block the other.

## Endpoints

| Path | Endpoint | Cadence | Code |
|---|---|---|---|
| Metrics | `POST /api/v1/report` | every `COLLECT_INTERVAL` (default 10s) | [`report.Reporter.Send`](../report/reporter.go) |
| Logs | `POST /api/v1/logs/ingest` | streaming, batched by `LOG_BATCH_MS` / `LOG_BATCH_BYTES` | [`logs.Shipper.Run`](../logs/shipper.go) |

## Payloads

- **Metrics**: single JSON object per request. Holds `system{cpu,mem,disk}` + `containers[]{name,id,cpu,mem,...}`. Few KB per report.
- **Logs**: NDJSON stream (one JSON line per log entry). Batched on time + byte threshold. ~400 B per line avg.

## Auth

Both use `Authorization: Bearer <AGENT_TOKEN>`. Token issued at agent registration, stored in agent `.env`.

## Retry behavior

### Metrics ([reporter.go](../report/reporter.go))
- Per-request timeout: 10s
- Retries: 4 attempts (1s, 2s, 4s, 8s backoff)
- 4xx/5xx response → no retry, return error
- Network error → retry
- Honors `ctx` during sleeps (shutdown breaks out immediately)
- Worst-case Send blocks: ~55s on hung server

### Logs ([shipper.go](../logs/shipper.go))
- Per-request timeout: 30s
- Reconnect on stream end with exponential backoff (1s → 30s cap)
- 401 / 408 / 429 retryable
- Other 4xx fatal — shipper exits
- 5xx + network errors → infinite retry with backoff

## Backpressure

Metrics: synchronous. Tick blocks until Send returns or fails. `time.Ticker` channel cap = 1, so missed ticks coalesce silently. Lost reports during outage = gaps in graph. No queue.

Logs: bounded channel between collector and shipper.
- Capacity: `perContainerBuffer × 4` lines (current default 5,120 × 4 = 20,480)
- Drop-oldest on overflow → `dropped` atomic counter increments
- Counter logged every 30s when non-zero
- See [LOG-OUTAGE.md](../LOG-OUTAGE.md) for sizing per tier




## Failure isolation

- Server unreachable → metrics drop ticks, logs queue + drop-oldest. Agent stays healthy.
- 401 on metrics → reporter errors per tick, no retries.
- 401 on logs → shipper retries (token may be transient).
- Other 4xx on logs → shipper exits permanently. Restart agent to recover.
- Cgroup-isolated. OOM kills only the agent, not co-located containers.
