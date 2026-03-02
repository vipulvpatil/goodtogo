package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const decisionsFile = "decisions.jsonl"

func dataDir() string {
	if dir := os.Getenv("GOODTOGO_DATA_DIR"); dir != "" {
		_ = os.MkdirAll(dir, 0755)
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	dir := filepath.Join(home, ".local", "share", "goodtogo")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func decisionsPath() string {
	return filepath.Join(dataDir(), decisionsFile)
}

const (
	promWindow   = "5m"
	stabilize    = 5 * time.Minute
	pollInterval = 15 * time.Second
)

type checkRecord struct {
	Time       string      `json:"time"`
	BuildTag   string      `json:"build_tag"`
	Verdict    string      `json:"verdict"`
	Baseline   windowStats `json:"baseline"`
	Canary     windowStats `json:"canary"`
	Checks     []result    `json:"checks"`
	LLMVerdict string      `json:"llm_verdict"`
	LLMReason  string      `json:"llm_reason"`
}

func appendResult(buildTag, verdict, llmVerdict, llmReason string, base, canary windowStats, checks []result) {
	rec := checkRecord{
		Time:       time.Now().UTC().Format(time.RFC3339),
		BuildTag:   buildTag,
		Verdict:    verdict,
		Baseline:   base,
		Canary:     canary,
		Checks:     checks,
		LLMVerdict: llmVerdict,
		LLMReason:  llmReason,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		log.Printf("warning: could not marshal result: %v", err)
		return
	}
	path := decisionsPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("warning: could not open %s: %v", path, err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\n", line)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	promURL := envOrDefault("PROMETHEUS_URL", "http://localhost:9090")

	buildTag := canaryBuildTag()
	log.Printf("goodtogo: canary build: %s", buildTag)
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
		} else {
			log.Printf("canary not found in prometheus — waiting")
		}
		time.Sleep(pollInterval)
	}

	log.Println("running check...")
	if !check(promURL, buildTag) {
		os.Exit(1)
	}
}

func check(promURL, buildTag string) bool {
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
		if !r.Ok {
			allOK = false
		}
	}

	verdict := "GOOD TO GO"
	if !allOK {
		verdict = "NOT GOOD TO GO"
	}

	fmt.Printf("canary build: %s\n", buildTag)
	fmt.Println(verdict)
	for _, r := range results {
		status := "✓"
		if !r.Ok {
			status = "✗"
		}
		fmt.Printf("  %s %s\n", status, r.Reason)
	}

	fmt.Println()
	fmt.Println("--- LLM analysis ---")
	llmVerdict, llmReason := llmAnalyze(base, canary)
	fmt.Printf("verdict : %s\n", llmVerdict)
	fmt.Printf("reason  : %s\n", llmReason)

	appendResult(buildTag, verdict, llmVerdict, llmReason, base, canary, results)
	return allOK
}
