# Log Buffer Sizing Tiers

In-memory log channel sizing per outage acceptance window. Per-line avg = 500 B.

Formula:
- `lines_to_hold = containers × lines_per_sec × outage_seconds`
- `perContainerBuffer = lines_to_hold / 4` (channel cap is `perContainerBuffer × 4`)
- `buffer_RAM = lines_to_hold × 500 B`
- `total_RAM = buffer_RAM + ~20 MB` (Go runtime + Docker SDK + goroutines)
- `compose_limit ≈ total_RAM × 1.5` (GC headroom)

---

## Tier 1 — 10 containers × 1 line/sec
Aggregate: **10 lines/sec**

| Outage | Lines | perContainerBuffer | Buffer RAM | Total RAM | Compose limit |
|---|---|---|---|---|---|
| 1 min | 600 | 150 | 0.3 MB | ~20 MB | 32 MB |
| 5 min | 3,000 | 750 | 1.5 MB | ~22 MB | 32 MB |
| 15 min | 9,000 | 2,250 | 4.5 MB | ~25 MB | 48 MB |
| **1 hour** | **36,000** | **9,000** | **18 MB** | **~40 MB** | **64 MB** |

Cheap tier. RAM-only buffer fine.

---

## Tier 2 — 50 containers × 10 lines/sec
Aggregate: **500 lines/sec**

| Outage | Lines | perContainerBuffer | Buffer RAM | Total RAM | Compose limit |
|---|---|---|---|---|---|
| 1 min | 30,000 | 7,500 | 15 MB | ~35 MB | 64 MB |
| **5 min** | **150,000** | **37,500** | **75 MB** | **~95 MB** | **128 MB** |
| 15 min | 450,000 | 112,500 | 225 MB | ~250 MB | 384 MB |
| 1 hour | 1,800,000 | 450,000 | 900 MB | ~950 MB | 1 GB |

Recommend cap at 5-min outage. Beyond that, switch to disk spool.

---

## Tier 3 — 100 containers × 20 lines/sec
Aggregate: **2,000 lines/sec**

| Outage | Lines | perContainerBuffer | Buffer RAM | Total RAM | Compose limit |
|---|---|---|---|---|---|
| 1 min | 120,000 | 30,000 | 60 MB | ~80 MB | 128 MB |
| **5 min** | **600,000** | **150,000** | **300 MB** | **~325 MB** | **512 MB** |
| 15 min | 1,800,000 | 450,000 | 900 MB | ~925 MB | 1.5 GB |
| 1 hour | 7,200,000 | 1,800,000 | 3.6 GB | ~3.65 GB | 5.5 GB |

RAM-only viable up to ~5 min. Past that, disk spool mandatory.


---

## When to switch from RAM to disk spool

| Lines/sec aggregate | Max RAM-only outage |
|---|---|
| ≤ 10 (tier 1) | 1 hour |
| ≤ 500 (tier 2) | 5 min |
| ≤ 2,000 (tier 3) | 5 min (high RAM) or 1 min (low RAM) |

Beyond these limits: write to local file first, ship from disk. Bounded by disk, not RAM.not implemented

---

## Knobs

Constants in [agent/logs/collector.go](logs/collector.go):
- `perContainerBuffer` — change per tier
- Channel cap = `perContainerBuffer × 4`

Compose memory limit in [docker-compose.yaml](../docker-compose.yaml) under agent service.
