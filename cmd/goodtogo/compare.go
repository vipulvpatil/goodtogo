package main

import "fmt"

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
	canary4xx := rate(canary.errors4xx, canary.requests)
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
