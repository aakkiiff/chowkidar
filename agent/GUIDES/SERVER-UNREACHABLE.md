# Server Unreachable — What Happens

Agent stays healthy. Other containers untouched. No cascading failure.

## Metrics path

- Tick fires every `COLLECT_INTERVAL` (default 10s).
- `Send` is synchronous: 4 retries with 1s + 2s + 4s + 8s backoff and 10s post timeout each.
- Worst-case `Send` blocks ~55s on hung server.
- During that block, `time.Ticker` (cap = 1) silently coalesces missed ticks.
- After `Send` fails or returns, loop picks up next tick → fresh metrics → retries.
- **No queue, no spool, no replay.** Missed reports = gaps in graph.
- Memory: each report ~few KB. One in flight. Zero pressure.

At `COLLECT_INTERVAL=3s`: ~18 ticks fire during a 55s outage; 17 dropped silently, 1 retained, then retry cycle. Graph sparse, not broken.

## Logs path

- Bounded channel between collector and shipper. Cap = `perContainerBuffer × 4` (default 20,480 lines).
- Server slow / down → shipper POST stalls.
- New lines fill channel. When full → **drop-oldest**, increment `dropped` counter.
- Drop counter logged every 30s if non-zero: `[logs] dropped N lines in last 30s`.
- Shipper: 30s POST timeout, backoff 1s → 30s, retries forever on 5xx / network / 401 / 408 / 429.
- Other 4xx (400 / 403 / 404 / etc.) → shipper exits. Agent restart needed.
- **No disk spool.** Lost lines are lost forever.
- Memory bounded: ~8–10 MB worst case at default sizing.

## Resource isolation

- Agent runs in own cgroup-limited container.
- OOM kills only the agent. Other containers unaffected.
- Docker daemon load stays light (list + stats every 10s, streaming log tails).
- No disk writes from agent → cannot fill host disk.

## Recovery

- Server back → next metrics tick succeeds → reports resume.
- Server back → shipper next retry succeeds → drains backlog.
- Drop counter does not reset (cumulative). Diff per 30s shows current loss rate.

## Worst case during outage

- Lost log lines (whatever didn't fit in 20,480 buffer)
- Lost metrics ticks (every tick after the first during outage)
- Agent process: stays healthy, auto-recovers

## Tuning

- Bigger log buffer → see [LOG-OUTAGE.md](../LOG-OUTAGE.md) per-tier sizing
- Real durability across long outages → add disk spool (not yet implemented)
- Faster failure detection → lower `COLLECT_INTERVAL` (more ticks during outage = more drops, but quicker recovery signal)
