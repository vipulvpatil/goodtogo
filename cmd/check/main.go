package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

const (
	checkInterval = 5 * time.Minute
	promWindow    = "5m"
)

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
