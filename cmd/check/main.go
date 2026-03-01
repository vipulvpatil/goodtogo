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

	log.Printf("goodtogo: waiting for canary — prometheus=%s", promURL)

	for {
		_, ok, err := queryInstant(promURL, `sum(increase(whatfpl_requests_total{job="canary"}[1m]))`)
		if err != nil {
			log.Printf("prometheus error: %v", err)
		} else if ok {
			break
		}
		time.Sleep(pollInterval)
	}

	log.Printf("canary detected — stabilising for %s", stabilize)
	time.Sleep(stabilize)

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
