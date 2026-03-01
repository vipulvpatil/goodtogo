package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

const (
	promWindow   = "5m"
	stabilize    = 5 * time.Minute
	pollInterval = 15 * time.Second
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	promURL := envOrDefault("PROMETHEUS_URL", "http://localhost:9090")

	log.Printf("goodtogo: waiting for canary to stabilise — prometheus=%s", promURL)

	for {
		ageSeconds, ok, err := queryInstant(promURL, `time() - process_start_time_seconds{job="canary"}`)
		if err != nil {
			log.Printf("prometheus error: %v", err)
		} else if ok {
			age := time.Duration(ageSeconds) * time.Second
			if age >= stabilize {
				log.Printf("canary stable (%s) — running check", age.Round(time.Second))
				break
			}
			log.Printf("canary up for %s — waiting until %s", age.Round(time.Second), stabilize)
		}
		time.Sleep(pollInterval)
	}

	log.Println("running check...")
	if !check(promURL) {
		os.Exit(1)
	}
}

func check(promURL string) bool {
	base, baseOK, err := queryJob(promURL, "baseline")
	if err != nil {
		log.Printf("baseline query error: %v", err)
		return false
	}
	if !baseOK {
		log.Println("no baseline data")
		return false
	}

	canary, canaryOK, err := queryJob(promURL, "canary")
	if err != nil {
		log.Printf("canary query error: %v", err)
		return false
	}
	if !canaryOK {
		log.Println("no canary data")
		return false
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

	return allOK
}
