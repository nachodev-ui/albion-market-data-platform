# Receiver performance baselines

> Scope: fictional Albion Online game data only. This project does not perform financial trading or real-money transactions.

Run the complete local matrix with:

```powershell
.\scripts\benchmark-receiver-baseline.ps1 -Samples 25 -ValidateBudgets
```

The report contains total time, mean, min, max, p50, p95, allocated bytes, allocation count, artifact sizes and operational counters. CPU, heap and mutex profiles are written next to `baseline.json`.

Covered scenarios:

- persisted captures of 1,000 and 10,000 records;
- normalization and serialization;
- normalized JSONL and embedded database updates;
- persistent outbox enqueue and restart with 1,000 and 10,000 entries;
- normalization, serialization and persistence of a 68-bucket history;
- reads of 100 current records and 100 historical snapshots;
- quantified recovery after a temporary upstream error;
- API round trip confirmed by PostgreSQL in CI.

## First observed CI baseline

| Scenario | p50 | p95 |
|---|---:|---:|
| Persisted capture 1,000 | 10.68 ms | 11.53 ms |
| Persisted capture 10,000 | 90.15 ms | 103.67 ms |
| JSONL 10,000 | 39.10 ms | 42.34 ms |
| Embedded database 10,000 | 35.70 ms | 41.27 ms |
| Outbox enqueue 10,000 | 50.19 ms | 51.87 ms |
| Outbox restart 10,000 | 41.69 ms | 49.03 ms |
| History 68 buckets | 0.07 ms | 0.19 ms |
| Read 100 records | 17.43 ms | 22.56 ms |
| Recovery after upstream error | 20.98 ms | 21.82 ms |

PostgreSQL round trip for batches of 500:

| Phase | median | p95 |
|---|---:|---:|
| New rows | 25.36 ms | 39.45 ms |
| Newer observations | 28.47 ms | 42.63 ms |
| No effective change | 17.86 ms | 20.78 ms |
| Idempotent duplicate | 5.13 ms | 5.87 ms |

Numerical limits were added only after collecting these measurements. Local limits live in `performance/receiver-budgets.json`; PostgreSQL p95 limits are validated by `scripts/validate-postgres-performance.ps1`.

The CPU profile identified JSON encoding, memory copies and garbage collection as the main costs. Mutex contention was about 12 ms across the full run and did not reveal a dominant wide lock.
