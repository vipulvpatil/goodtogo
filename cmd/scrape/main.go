package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// Metrics mirrors the shape returned by the /metrics endpoint.
type Metrics struct {
	RequestsTotal  int64 `json:"requests_total"`
	Errors4xxTotal int64 `json:"errors_4xx_total"`
	Errors5xxTotal int64 `json:"errors_5xx_total"`
	LatencyMs      struct {
		P50 int64 `json:"p50"`
		P95 int64 `json:"p95"`
	} `json:"latency_ms"`
}

// Record is one flat line written to the JSONL file.
// Counter fields are absolute cumulative totals from the scraped endpoint.
type Record struct {
	Ts             time.Time `json:"ts"`
	RequestsTotal  int64     `json:"requests_total"`
	Errors4xxTotal int64     `json:"errors_4xx_total"`
	Errors5xxTotal int64     `json:"errors_5xx_total"`
	P50Ms          int64     `json:"p50_ms"`
	P95Ms          int64     `json:"p95_ms"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func scrape(url string) (*Metrics, error) {
	resp, err := httpClient.Get(url) //nolint:gosec // URL comes from trusted env var
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	var m Metrics
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &m, nil
}

func appendRecord(path string, r Record) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	metricsURL := os.Getenv("METRICS_URL")
	if metricsURL == "" {
		log.Fatal("METRICS_URL is required")
	}

	intervalStr := envOrDefault("SCRAPE_INTERVAL", "1m")
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		log.Fatalf("invalid SCRAPE_INTERVAL %q: %v", intervalStr, err)
	}

	dataFile := envOrDefault("DATA_FILE", "metrics.jsonl")

	log.Printf("goodtogo scraper starting — url=%s interval=%s file=%s", metricsURL, interval, dataFile)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ; ; <-ticker.C {
		m, err := scrape(metricsURL)
		if err != nil {
			log.Printf("scrape error: %v", err)
			continue
		}
		r := Record{
			Ts:             time.Now().UTC(),
			RequestsTotal:  m.RequestsTotal,
			Errors4xxTotal: m.Errors4xxTotal,
			Errors5xxTotal: m.Errors5xxTotal,
			P50Ms:          m.LatencyMs.P50,
			P95Ms:          m.LatencyMs.P95,
		}
		if err := appendRecord(dataFile, r); err != nil {
			log.Printf("write error: %v", err)
			continue
		}
		log.Printf("recorded — requests=%d 4xx=%d 5xx=%d p50=%dms p95=%dms",
			r.RequestsTotal, r.Errors4xxTotal, r.Errors5xxTotal, r.P50Ms, r.P95Ms)
	}
}
