# Load test results

Measured with [`loadtest/k6.js`](k6.js) via [`scripts/loadtest.sh`](../scripts/loadtest.sh) on 2026-11-22.

## Setup

| Item | Value |
| --- | --- |
| Machine | Windows laptop, Docker Desktop (WSL 2), **6 vCPUs** visible to containers |
| Co-located on those 6 vCPUs | k6 v2.4.0, the API, PostgreSQL 17, Redis 7 |
| API | Go 1.27, `GIN_MODE=release`, fake predictor (2 s delay), SQS publishing off |
| Traffic | 80% `GET /inventory/:sku`, 20% `POST /sales`, random SKU among 100 |
| Model | Open model (`constant-arrival-rate`), 60 s per run |
| Targets (README) | 2,500 req/s, p99 < 200 ms, errors < 1% |

Everything shares one machine, so these numbers are a **lower bound** for the API: at high rates k6 competes with it for the same CPU.

## Latency and throughput

| Run | Offered | Delivered¹ | p50 | p95 | p99 reads | p99 writes | Errors | Dropped² | Thresholds |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Baseline | 2,500/s | ~2,500/s | 0.65 ms | 2.69 ms | 2.39 ms | 6.90 ms | 0% | 222 (0.15%) | ✓ |
| Queue dedup | 2,500/s | ~2,500/s | 0.64 ms | 2.59 ms | 2.02 ms | 6.23 ms | 0% | 108 (0.07%) | ✓ |
| Stress | 5,000/s | ~4,980/s | 0.76 ms | 24.2 ms | 75.0 ms | 80.9 ms | 0% | 1,159 (0.4%) | ✓ |
| Saturation | 10,000/s | ~4,630/s | 625 ms | 934 ms | 3.95 s | 3.95 s | 0% | 322,169 (54%) | ✗ |

¹ Completed requests divided by the 60 s of load. k6's own `http_reqs` rate divides by the total run time, which includes the environment stalls described below.
² Requests k6 could not start on schedule because all its virtual users were busy.

Raw output: [`results-baseline.txt`](results-baseline.txt), [`results-dedup.txt`](results-dedup.txt), [`results-5000.txt`](results-5000.txt), [`results-10000.txt`](results-10000.txt).

## Findings

**The README target is met with a wide margin.** At 2,500 req/s the p99 is 2–7 ms against a 200 ms budget, with zero errors. Writes cost about 3× reads: a sale is a transaction with a row lock, an insert, an update and a commit; a read is one `SELECT` and one Redis `GET`.

**Capacity on this machine is about 4,600–5,000 req/s.** At 5,000 req/s every threshold still passes, but p95 grows tenfold (2.6 → 24 ms): the knee of the latency curve. At 10,000 req/s throughput stops at ~4,630 req/s and latency explodes.

**Saturation latency is queueing, and Little's law predicts it.** In the 10,000 req/s run k6 reached its cap of 3,000 concurrent virtual users. By Little's law (L = λ·W), 3,000 requests in flight at ~4,630 completions per second means each waits W ≈ 3,000 / 4,630 ≈ **0.65 s**. The measured p50 was **625 ms**. Past capacity, requests are not processed slower; they wait longer.

**The bottleneck is not identified yet.** With k6 on the same 6 vCPUs, the limit may be the API, PostgreSQL, or k6 itself. Running k6 on another machine, or watching `docker stats` during the run, would tell them apart.

**Every run's maximum latency (3.35–3.45 s) is an environment artifact.** About every 33 s, k6's own clock jumps by ~4.3 s while its scenarios advance by 1 s (for example `0m25.0s → 0m29.4s`): the whole Docker Desktop VM pauses, k6 included. Reads and writes share the same maximum for that reason. Percentiles are not affected.

## Background worker efficiency

Each sale enqueues a forecast for its SKU; the per-SKU Redis lock (5-minute window) then skips SKUs forecast recently.

| Counter (Prometheus) | Baseline³ | With queue dedup⁴ |
| --- | --- | --- |
| `worker_enqueue_rejected_total{reason="full"}` | 1,525 | **0** |
| `worker_enqueue_deduplicated_total` | — | 24,824 |
| `worker_jobs_total{outcome="skipped"}` | 33,664 | 156,209 |
| `worker_jobs_total{outcome="success"}` | 202 | 100 |

³ One API process: two smoke runs and the baseline run.
⁴ One API process: the dedup, 5,000 and 10,000 req/s runs (~181,000 enqueue attempts).

**Queue dedup removed queue overflow entirely**: a SKU already waiting is never queued twice, so 100 SKUs can never fill a queue of 100.

**It did not remove most of the wasted work.** Skipping a SKU costs one Redis `SET NX` (well under a millisecond), so the queue drains almost instantly and a SKU is rarely still pending when its next sale arrives. Only 14% of attempts were caught in memory; 86% still reached Redis just to be skipped.

**Next step (backlog):** a two-level check. Each instance remembers locally, for the dedup window, the SKUs it has seen forecast, and only asks Redis when it does not know. That would cut Redis lock calls to about one per SKU per window per instance, while Redis keeps guaranteeing a single forecast across instances.
