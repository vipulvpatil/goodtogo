package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	checkInterval = 5 * time.Minute
	promWindow    = "5m"
)

type windowStats struct {
	requests  int64
	errors4xx int64
	errors5xx int64
	avgP95Ms  int64
}

type result struct {
	ok     bool
	reason string
}

func compare(base, canary windowStats) []result {
	var results []result

	// 5xx: any increase is bad.
	base5xx := rate(base.errors5xx, base.requests)
	canary5xx := rate(canary.errors5xx, canary.requests)
	results = append(results, result{
		ok: canary5xx <= base5xx,
		reason: fmt.Sprintf("5xx rate: baseline %.2f%% → canary %.2f%%",
			base5xx*100, canary5xx*100),
	})

	// 4xx: bad if canary rate is more than 2× baseline.
	base4xx := rate(base.errors4xx, base.requests)
	canary4xx := rate(canary.errors4xx, base.requests)
	results = append(results, result{
		ok: canary4xx <= base4xx*2,
		reason: fmt.Sprintf("4xx rate: baseline %.2f%% → canary %.2f%% (threshold 2×)",
			base4xx*100, canary4xx*100),
	})

	// p95: bad if canary avg p95 is more than 20% higher than baseline.
	results = append(results, result{
		ok: float64(canary.avgP95Ms) <= float64(base.avgP95Ms)*1.20,
		reason: fmt.Sprintf("avg p95: baseline %dms → canary %dms (threshold +20%%)",
			base.avgP95Ms, canary.avgP95Ms),
	})

	return results
}

func rate(errors, requests int64) float64 {
	if requests == 0 {
		return 0
	}
	return float64(errors) / float64(requests)
}

// --- Prometheus query ---

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Value [2]json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// queryInstant runs an instant PromQL query and returns the first scalar result.
// Returns (0, false, nil) when the result set is empty (e.g. job not up yet).
func queryInstant(promURL, promql string) (float64, bool, error) {
	u := promURL + "/api/v1/query?query=" + url.QueryEscape(promql)
	resp, err := httpClient.Get(u) //nolint:gosec
	if err != nil {
		return 0, false, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()

	var pr promResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return 0, false, fmt.Errorf("decode: %w", err)
	}
	if pr.Status != "success" {
		return 0, false, fmt.Errorf("prometheus status %q", pr.Status)
	}
	if len(pr.Data.Result) == 0 {
		return 0, false, nil
	}

	var valStr string
	if err := json.Unmarshal(pr.Data.Result[0].Value[1], &valStr); err != nil {
		return 0, false, fmt.Errorf("unmarshal value: %w", err)
	}
	if valStr == "NaN" || valStr == "+Inf" || valStr == "-Inf" {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse float %q: %w", valStr, err)
	}
	return v, true, nil
}

func queryJob(promURL, job string) (windowStats, bool, error) {
	type metric struct {
		promql string
		dest   *int64
	}
	var stats windowStats
	queries := []metric{
		{fmt.Sprintf(`increase(whatfpl_requests_total{job=%q}[%s])`, job, promWindow), &stats.requests},
		{fmt.Sprintf(`increase(whatfpl_errors_4xx_total{job=%q}[%s])`, job, promWindow), &stats.errors4xx},
		{fmt.Sprintf(`increase(whatfpl_errors_5xx_total{job=%q}[%s])`, job, promWindow), &stats.errors5xx},
		{fmt.Sprintf(`histogram_quantile(0.95, rate(whatfpl_request_duration_ms_bucket{job=%q}[%s]))`, job, promWindow), &stats.avgP95Ms},
	}

	for _, q := range queries {
		v, ok, err := queryInstant(promURL, q.promql)
		if err != nil {
			return windowStats{}, false, fmt.Errorf("query %q: %w", q.promql, err)
		}
		if !ok {
			return windowStats{}, false, nil
		}
		*q.dest = int64(math.Round(v))
	}
	return stats, true, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func check(promURL string) {
	base, baseOK, err := queryJob(promURL, "baseline")
	if err != nil {
		log.Printf("baseline query error: %v", err)
		return
	}
	if !baseOK {
		log.Println("no baseline data yet — waiting")
		return
	}

	canary, canaryOK, err := queryJob(promURL, "canary")
	if err != nil {
		log.Printf("canary query error: %v", err)
		return
	}
	if !canaryOK {
		log.Println("no canary detected — nothing to compare")
		return
	}

	results := compare(base, canary)

	allOK := true
	for _, r := range results {
		if !r.ok {
			allOK = false
		}
	}

	if allOK {
		fmt.Println("GOOD TO GO")
	} else {
		fmt.Println("NOT GOOD TO GO")
	}
	for _, r := range results {
		status := "✓"
		if !r.ok {
			status = "✗"
		}
		fmt.Printf("  %s %s\n", status, r.reason)
	}
}

func main() {
	promURL := envOrDefault("PROMETHEUS_URL", "http://localhost:9090")

	log.Printf("goodtogo checker starting — prometheus=%s interval=%s window=%s",
		promURL, checkInterval, promWindow)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for ; ; <-ticker.C {
		check(promURL)
	}
}
