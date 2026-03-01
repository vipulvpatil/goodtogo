# goodtogo

Automated deployment gate for canary releases. Queries Prometheus, compares canary metrics against the baseline, and exits `0` (GOOD TO GO) or `1` (NOT GOOD TO GO).

Designed to run alongside [whatfpl](https://github.com/vipulvpatil/whatfpl), but works with any service that exposes Prometheus metrics with `job="baseline"` and `job="canary"` labels.

---

## How it works

```
Prometheus
    │
    ├── job="baseline"  ──┐
    └── job="canary"    ──┴──► goodtogo check ──► GOOD TO GO / NOT GOOD TO GO
```

1. **Wait for canary** — polls Prometheus until `job="canary"` has data.
2. **Stabilise** — waits 5 minutes for the canary to accumulate a meaningful traffic window.
3. **Collect** — queries the last 5 minutes of metrics for both jobs.
4. **Compare** — runs three checks (see below) and prints a summary.
5. **Exit** — `0` if all checks pass, `1` if any fail.

### Checks

| Check | Pass condition |
|---|---|
| 5xx error rate | canary ≤ baseline |
| 4xx error rate | canary ≤ baseline × 2 |
| p95 latency | canary ≤ baseline × 1.20 |

---

## Usage

```bash
# against a local Prometheus (default)
go run ./cmd/check

# against a remote Prometheus
PROMETHEUS_URL=http://prometheus:9090 go run ./cmd/check
```

Or build and run the binary:

```bash
go build -o goodtogo ./cmd/check
./goodtogo
PROMETHEUS_URL=http://prometheus:9090 ./goodtogo
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `PROMETHEUS_URL` | `http://localhost:9090` | Base URL of the Prometheus instance |

### Exit codes

| Code | Meaning |
|---|---|
| `0` | All checks passed — safe to promote canary |
| `1` | One or more checks failed — do not promote |

### Example output

```
goodtogo: waiting for canary — prometheus=http://localhost:9090
canary detected — stabilising for 5m0s
running check...
GOOD TO GO
  ✓ 5xx rate: baseline 0.00% → canary 0.00%
  ✓ 4xx rate: baseline 4.00% → canary 3.90% (threshold 2×)
  ✓ avg p95: baseline 210ms → canary 220ms (threshold +20%)
```

```
NOT GOOD TO GO
  ✓ 5xx rate: baseline 0.00% → canary 0.00%
  ✗ 4xx rate: baseline 4.00% → canary 12.50% (threshold 2×)
  ✗ avg p95: baseline 210ms → canary 310ms (threshold +20%)
```

---

## Timing constants

| Constant | Value | Purpose |
|---|---|---|
| `promWindow` | `5m` | Lookback window used in all PromQL queries |
| `stabilize` | `5m` | How long to wait after canary is first seen |
| `pollInterval` | `15s` | How often to poll while waiting for canary |

---

## Prometheus requirements

The Prometheus instance must scrape both services and attach a `job` label:

- `job="baseline"` — current production traffic
- `job="canary"` — new version under test

Expected metrics (from [whatfpl](https://github.com/vipulvpatil/whatfpl) or any compatible service):

| Metric | Type |
|---|---|
| `whatfpl_requests_total` | Counter |
| `whatfpl_errors_4xx_total` | Counter |
| `whatfpl_errors_5xx_total` | Counter |
| `whatfpl_request_duration_ms_bucket` | Histogram |
