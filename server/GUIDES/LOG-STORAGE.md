# Log Storage Sizing

Server stores per `(agent, container)` pair: 1 active `current.log` (uncompressed) + N gzipped rotations (`*.gz`).

Path layout:
```
<LOG_DIR>/<agent_id>/<container_name>/
  current.log
  current.log.20260427T120000.gz
  current.log.20260427T060000.gz
  ...
```

---

## Defaults (server/.env)

```
LOG_RETENTION_DAYS=5
LOG_MAX_FILE_MB=100
LOG_MAX_ROTATIONS=20
```

Per-line size assumed: ~400 B raw, ~40 B gzipped (10× compression — typical for repetitive log text).

---

## Hard ceiling (per container)

```
ceiling = LOG_MAX_FILE_MB + LOG_MAX_ROTATIONS × gzipped_rotation_size
       ≈ 100 MB active + 20 × ~10 MB gzipped
       ≈ 300 MB
```

Actual usage = min(rotation cap, age cap). Whichever fills first wins.

---

## Pruning logic

Two caps run on the hourly prune ticker + once at startup:

1. **Rotation count** — when there are > `LOG_MAX_ROTATIONS` gzipped files, the oldest is deleted (lex-sort by timestamp in name = chronological).
2. **Age** — any `.gz` older than `LOG_RETENTION_DAYS` is deleted regardless of count.

Active `current.log` is never deleted by age — only rotated when it crosses `LOG_MAX_FILE_MB`.

Crossover formula — when does rotation cap win vs age cap:
```
rotation_cap_wins if (LOG_MAX_ROTATIONS × MAX_FILE_MB) < (lines/sec × line_size × 86400 × RETENTION_DAYS)
```

Translation: high traffic → rotations stack up before age catches them → rotation cap wins (300 MB hard). Low traffic → not enough rotations to fill the count → age cap wins (less than 300 MB).

---

## Rotation rate by load

How long the active file takes to fill 100 MB at 400 B/line:

| Lines/sec/container | 100 MB fill time | Rotations/day | 5-day count |
|---|---|---|---|
| 0.1 | ~29 days | 0.03 | 0.17 (age wins) |
| 1 | ~2.9 days | 0.34 | 1.7 (age wins) |
| 10 | ~7 hours | 3.4 | 17 (rotation cap wins at 20) |
| 50 | ~83 min | 17 | 85 (rotation cap wins quickly) |
| 100 | ~42 min | 35 | 173 (rotation cap = 20 hard) |

---

## Tier estimates (real-world breakdown)

### Tier 1 — 10 containers × 1 line/sec
Per container per day: 86,400 lines × 400 B = **~33 MB/day raw**.

Over 5-day retention:
- Active file averages ~50 MB (fills 100 MB in ~3 days, rotates once)
- Rotations: 1 in 5 days × ~10 MB gzipped = 10 MB
- **Per container ~60 MB**

**Aggregate: ~600 MB** for 10 containers.

Line size variance:
- Tiny lines (150 B): ~250 MB
- Chatty lines (800 B): ~1.2 GB

### Tier 2 — 50 containers × 10 lines/sec
Per container per day: 864,000 lines × 400 B = **~330 MB/day raw**.

Over 5 days raw = 1.65 GB — but rotation kicks in:
- Active fills 100 MB in ~7 hours → ~3.4 rotations/day
- 5 days × 3.4 = 17 rotations (under the 20 cap → age cap wins)
- 17 × ~10 MB gzipped = 170 MB rotations
- Active averages 50 MB
- **Per container ~220 MB**

**Aggregate: ~11 GB** for 50 containers.

### Tier 3 — 100 containers × 20 lines/sec
Per container per day: 1.7 M lines × 400 B = **~660 MB/day raw**.

- Active fills in ~3.5 hours → ~7 rotations/day
- 20-rotation cap hit in ~3 days → rotation cap wins (300 MB hard ceiling)
- **Per container ~300 MB**

**Aggregate: ~30 GB** for 100 containers.

### Tier 4 — 200 containers × 30 lines/sec
Per container per day: 2.6 M lines × 400 B = **~1 GB/day raw**.

- Active fills in ~2.3 hours → ~10 rotations/day
- 20-rotation cap hit in 2 days → rotation cap wins
- **Per container ~300 MB**

**Aggregate: ~60 GB** for 200 containers.


---

## Knobs

- `LOG_MAX_ROTATIONS` ↑ → longer history, more disk per container
- `LOG_MAX_FILE_MB` ↓ → faster gzip wins, smaller active footprint, more file handles churn
- `LOG_RETENTION_DAYS` ↓ → aggressive age cap, cuts disk for low-traffic containers
- Swap gzip → zstd (~15× compression) → ~33 % less disk for same retention

---

## Pruner cadence

- Hourly ticker calls `PruneOld()` (age) + `PruneOrphans(agentIDs)` (deleted agents)
- Once at startup to catch orphans accumulated while server was down
- Active log files flush every 5s
- Code: [server/logstore/logstore.go](../logstore/logstore.go)

---

## Storage planning rule of thumb

```
budget = N_containers × line_rate_per_sec × avg_line_size × 86400 × RETENTION_DAYS / 10
```

Divide by 10 = gzip ratio. Add 100 MB per container for active files.

For tier 3 (100 c × 20 l/s × 400 B): `100 × 20 × 400 × 86400 × 5 / 10 ≈ 35 GB` + 100×100 MB = **45 GB**. Matches table above (~30 GB worst, +15 GB headroom for variance).
