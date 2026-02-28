package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

const windowDuration = 5 * time.Minute

type Record struct {
	Ts             time.Time `json:"ts"`
	RequestsTotal  int64     `json:"requests_total"`
	Errors4xxTotal int64     `json:"errors_4xx_total"`
	Errors5xxTotal int64     `json:"errors_5xx_total"`
	P95Ms          int64     `json:"p95_ms"`
}

type windowStats struct {
	requests  int64
	errors4xx int64
	errors5xx int64
	avgP95Ms  int64
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadAll(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

func filter(records []Record, from, to time.Time) []Record {
	var out []Record
	for _, r := range records {
		if !r.Ts.Before(from) && r.Ts.Before(to) {
			out = append(out, r)
		}
	}
	return out
}

// summarise computes window stats from a slice of records with absolute counters.
// Deltas are derived from last - first so only traffic within the window is counted.
func summarise(records []Record) (windowStats, bool) {
	if len(records) < 2 {
		return windowStats{}, false
	}
	first, last := records[0], records[len(records)-1]

	var sumP95 int64
	for _, r := range records {
		sumP95 += r.P95Ms
	}

	return windowStats{
		requests:  last.RequestsTotal - first.RequestsTotal,
		errors4xx: last.Errors4xxTotal - first.Errors4xxTotal,
		errors5xx: last.Errors5xxTotal - first.Errors5xxTotal,
		avgP95Ms:  sumP95 / int64(len(records)),
	}, true
}

func rate(errors, requests int64) float64 {
	if requests == 0 {
		return 0
	}
	return float64(errors) / float64(requests)
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

func main() {
	dataFile := envOrDefault("DATA_FILE", "metrics.jsonl")

	var deployTime time.Time
	if v := os.Getenv("DEPLOY_TIME"); v != "" {
		var err error
		deployTime, err = time.Parse(time.RFC3339, v)
		if err != nil {
			log.Fatalf("invalid DEPLOY_TIME %q (want RFC3339): %v", v, err)
		}
	} else {
		deployTime = time.Now().UTC().Add(-windowDuration)
	}

	now := time.Now().UTC()

	all, err := loadAll(dataFile)
	if err != nil {
		log.Fatalf("load data: %v", err)
	}

	baselineRecords := filter(all, deployTime.Add(-windowDuration), deployTime)
	canaryRecords := filter(all, deployTime, now)

	baseline, baseOK := summarise(baselineRecords)
	canary, canaryOK := summarise(canaryRecords)

	if !baseOK {
		log.Fatalf("not enough baseline data (need ≥2 records before deploy time)")
	}
	if !canaryOK {
		log.Fatalf("not enough canary data (need ≥2 records after deploy time)")
	}

	results := compare(baseline, canary)

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

	if !allOK {
		os.Exit(1)
	}
}
