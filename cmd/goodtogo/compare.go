package main

import "fmt"

const minCanaryRequests = 100

type windowStats struct {
	Requests  int64 `json:"requests"`
	Errors4xx int64 `json:"errors_4xx"`
	Errors5xx int64 `json:"errors_5xx"`
	AvgP95Ms  int64 `json:"avg_p95_ms"`
}

type result struct {
	Ok     bool   `json:"ok"`
	Reason string `json:"reason"`
}

func compare(base, canary windowStats) []result {
	if canary.Requests < minCanaryRequests {
		return []result{{
			Ok:     false,
			Reason: fmt.Sprintf("canary traffic too low: %d requests in window (minimum %d) — HOLD", canary.Requests, minCanaryRequests),
		}}
	}

	var results []result

	// 5xx: any increase is bad.
	base5xx := rate(base.Errors5xx, base.Requests)
	canary5xx := rate(canary.Errors5xx, canary.Requests)
	results = append(results, result{
		Ok: canary5xx <= base5xx,
		Reason: fmt.Sprintf("5xx rate: baseline %.2f%% → canary %.2f%%",
			base5xx*100, canary5xx*100),
	})

	// 4xx: bad if canary rate is more than 2× baseline.
	base4xx := rate(base.Errors4xx, base.Requests)
	canary4xx := rate(canary.Errors4xx, canary.Requests)
	results = append(results, result{
		Ok: canary4xx <= base4xx*2,
		Reason: fmt.Sprintf("4xx rate: baseline %.2f%% → canary %.2f%% (threshold 2×)",
			base4xx*100, canary4xx*100),
	})

	// p95: bad if canary avg p95 is more than 20% higher than baseline.
	results = append(results, result{
		Ok: float64(canary.AvgP95Ms) <= float64(base.AvgP95Ms)*1.20,
		Reason: fmt.Sprintf("avg p95: baseline %dms → canary %dms (threshold +20%%)",
			base.AvgP95Ms, canary.AvgP95Ms),
	})

	return results
}

func rate(errors, requests int64) float64 {
	if requests == 0 {
		return 0
	}
	return float64(errors) / float64(requests)
}
