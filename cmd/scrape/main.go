package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
	"encoding/json"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

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

func scrape(url string) (map[string]*dto.MetricFamily, error) {
	resp, err := httpClient.Get(url) //nolint:gosec // URL comes from trusted env var
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	var parser expfmt.TextParser
	return parser.TextToMetricFamilies(resp.Body)
}

func counterValue(mf *dto.MetricFamily) int64 {
	if mf == nil || len(mf.Metric) == 0 {
		return 0
	}
	return int64(mf.Metric[0].Counter.GetValue())
}

// histogramQuantile computes a quantile from a Prometheus histogram metric family
// using linear interpolation between bucket boundaries.
func histogramQuantile(mf *dto.MetricFamily, q float64) int64 {
	if mf == nil || len(mf.Metric) == 0 {
		return 0
	}
	h := mf.Metric[0].Histogram
	total := float64(h.GetSampleCount())
	if total == 0 {
		return 0
	}
	target := q * total

	var prevCount float64
	var prevUpper float64
	for _, b := range h.Bucket {
		upper := b.GetUpperBound()
		if math.IsInf(upper, 1) {
			break
		}
		count := float64(b.GetCumulativeCount())
		if count >= target {
			if count == prevCount {
				return int64(prevUpper)
			}
			v := prevUpper + (target-prevCount)/(count-prevCount)*(upper-prevUpper)
			return int64(math.Round(v))
		}
		prevCount = count
		prevUpper = upper
	}
	return int64(prevUpper)
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
	// Normalise: strip trailing slash, ensure path is /metrics
	metricsURL = strings.TrimRight(metricsURL, "/")
	if !strings.HasSuffix(metricsURL, "/metrics") {
		metricsURL += "/metrics"
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
		families, err := scrape(metricsURL)
		if err != nil {
			log.Printf("scrape error: %v", err)
			continue
		}
		r := Record{
			Ts:             time.Now().UTC(),
			RequestsTotal:  counterValue(families["whatfpl_requests_total"]),
			Errors4xxTotal: counterValue(families["whatfpl_errors_4xx_total"]),
			Errors5xxTotal: counterValue(families["whatfpl_errors_5xx_total"]),
			P50Ms:          histogramQuantile(families["whatfpl_request_duration_ms"], 0.50),
			P95Ms:          histogramQuantile(families["whatfpl_request_duration_ms"], 0.95),
		}
		if err := appendRecord(dataFile, r); err != nil {
			log.Printf("write error: %v", err)
			continue
		}
		log.Printf("recorded — requests=%d 4xx=%d 5xx=%d p50=%dms p95=%dms",
			r.RequestsTotal, r.Errors4xxTotal, r.Errors5xxTotal, r.P50Ms, r.P95Ms)
	}
}
