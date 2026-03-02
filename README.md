# goodtogo

Automated deployment gate for canary releases. Queries Prometheus, compares canary metrics against baseline, and exits `0` (GOOD TO GO) or `1` (NOT GOOD TO GO). Also runs an independent LLM-based analysis via Claude for comparison.

Designed to run alongside [whatfpl](https://github.com/vipulvpatil/whatfpl), but works with any service that exposes Prometheus metrics with `job="baseline"` and `job="canary"` labels.

---

## How it works

```
Prometheus
    │
    ├── job="baseline"  ──┐
    └── job="canary"    ──┴──► goodtogo check ──► GOOD TO GO / NOT GOOD TO GO
                                    │
                                    └──► LLM analysis (Claude, informational)
```

1. **Identify** — reads the canary's build tag from Docker image metadata.
2. **Wait for canary** — polls Prometheus until `job="canary"` has data.
3. **Stabilise** — waits 5 minutes for the canary to accumulate a meaningful traffic window.
4. **Collect** — queries the last 5 minutes of metrics for both jobs.
5. **Compare** — runs three rule-based checks (see below) and prints a verdict.
6. **Analyse** — independently sends the same metrics to Claude for a second opinion (informational only).
7. **Record** — appends the full decision (both verdicts) to `decisions.jsonl`.
8. **Exit** — `0` if all rule-based checks pass, `1` if any fail. The LLM verdict does not affect the exit code.

### Rule-based checks

| Check | Pass condition |
|---|---|
| 5xx error rate | canary ≤ baseline |
| 4xx error rate | canary ≤ baseline × 2 |
| p95 latency | canary ≤ baseline × 1.20 |

---

## Tools

| Command | Description |
|---|---|
| `cmd/goodtogo` | Main gate — compare metrics, record decision |
| `cmd/label` | Interactive tool to label past decisions as `promoted` or `rolled-back` |
| `cmd/eval` | Offline evaluator — computes confusion matrix for rule-based vs LLM verdicts |

---

## Usage

```bash
# against a local Prometheus and Docker (defaults)
go run ./cmd/goodtogo

# against a remote Prometheus
PROMETHEUS_URL=http://prometheus:9090 go run ./cmd/goodtogo
```

Or build binaries:

```bash
go build -o goodtogo ./cmd/goodtogo
go build -o label    ./cmd/label
go build -o eval     ./cmd/eval

./goodtogo
./label
./eval
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `PROMETHEUS_URL` | `http://localhost:9090` | Prometheus base URL |
| `ANTHROPIC_API_KEY` | *(unset)* | API key for LLM analysis. If unset, LLM step is skipped |
| `GOODTOGO_DATA_DIR` | `~/.local/share/goodtogo` | Directory for `decisions.jsonl` and `outcomes.jsonl` |

### Exit codes

| Code | Meaning |
|---|---|
| `0` | All rule-based checks passed — safe to promote |
| `1` | One or more checks failed, or metrics unavailable |

### Example output

```
canary build: a1b2c3d4-20260302-143012
GOOD TO GO
  ✓ 5xx rate: baseline 0.00% → canary 0.00%
  ✓ 4xx rate: baseline 4.00% → canary 3.90% (threshold 2×)
  ✓ avg p95: baseline 210ms → canary 220ms (threshold +20%)

--- LLM analysis ---
verdict : GOOD TO GO
reason  : Canary shows comparable error rates and latency within acceptable bounds.
```

```
canary build: a1b2c3d4-20260302-150045
NOT GOOD TO GO
  ✓ 5xx rate: baseline 0.00% → canary 0.00%
  ✗ 4xx rate: baseline 4.00% → canary 12.50% (threshold 2×)
  ✗ avg p95: baseline 210ms → canary 310ms (threshold +20%)

--- LLM analysis ---
verdict : NOT GOOD TO GO
reason  : Canary 4xx rate is 3× baseline and p95 latency is 47% higher — not safe to promote.
```

---

## Labelling outcomes

After a canary is promoted or rolled back, record the true outcome so decisions can be evaluated:

```bash
GOODTOGO_DATA_DIR=/path/to/data ./label
```

The label tool reads `decisions.jsonl`, shows each unlabelled decision, and prompts:

```
─────────────────────────────────────────
build:   a1b2c3d4-20260302-143012
time:    2026-03-02T14:30:12Z
verdict: GOOD TO GO
  ✓ 5xx rate: baseline 0.00% → canary 0.00%
  ✓ 4xx rate: baseline 4.00% → canary 3.90% (threshold 2×)
  ✓ avg p95: baseline 210ms → canary 220ms (threshold +20%)

[p]romoted / [r]olled-back / [s]kip:
```

Labelled decisions are written to `outcomes.jsonl` (self-contained — includes all metrics and both verdicts).

---

## Evaluating accuracy

Once you have labelled outcomes, compare rule-based vs LLM performance:

```bash
GOODTOGO_DATA_DIR=/path/to/data ./eval
```

Example output:

```
outcomes: 10

rule-based  (n=10)
                 promoted  rolled-back
GOOD TO GO          7 TP      1 FP
NOT GOOD TO GO      0 FN      2 TN

  accuracy    90.0%  (TP+TN / total)
  precision   87.5%  (TP / TP+FP  — of approvals, how many were right)
  recall     100.0%  (TP / TP+FN  — of good deploys, how many approved)
  FP rate     33.3%  (FP / FP+TN  — of bad deploys, how many slipped)

LLM  (n=10)
                 promoted  rolled-back
GOOD TO GO          7 TP      0 FP
NOT GOOD TO GO      0 FN      3 TN

  accuracy   100.0%  (TP+TN / total)
  precision  100.0%  (TP / TP+FP  — of approvals, how many were right)
  recall     100.0%  (TP / TP+FN  — of good deploys, how many approved)
  FP rate      0.0%  (FP / FP+TN  — of bad deploys, how many slipped)
```

---

## Data files

Both files live in `GOODTOGO_DATA_DIR` (default `~/.local/share/goodtogo/`):

| File | Written by | Read by | Description |
|---|---|---|---|
| `decisions.jsonl` | `goodtogo` | `label` | One record per check run: metrics, rule verdict, LLM verdict |
| `outcomes.jsonl` | `label` | `eval` | Labelled decisions with ground-truth (`promoted`/`rolled-back`) |

---

## Timing constants

| Constant | Value | Purpose |
|---|---|---|
| `promWindow` | `5m` | Lookback window for all PromQL queries |
| `stabilize` | `5m` | Wait after canary first appears before running check |
| `pollInterval` | `15s` | Poll frequency while waiting for canary |

---

## Prometheus requirements

The Prometheus instance must scrape both services with a `job` label:

- `job="baseline"` — current production traffic
- `job="canary"` — new version under test

Expected metrics (from [whatfpl](https://github.com/vipulvpatil/whatfpl) or any compatible service):

| Metric | Type |
|---|---|
| `whatfpl_requests_total` | Counter |
| `whatfpl_errors_4xx_total` | Counter |
| `whatfpl_errors_5xx_total` | Counter |
| `whatfpl_request_duration_ms_bucket` | Histogram |
