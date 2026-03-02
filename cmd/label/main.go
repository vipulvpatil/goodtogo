package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	decisionsFile = "decisions.jsonl"
	outcomesFile  = "outcomes.jsonl"
)

type windowStats struct {
	Requests  int64 `json:"requests"`
	Errors4xx int64 `json:"errors_4xx"`
	Errors5xx int64 `json:"errors_5xx"`
	AvgP95Ms  int64 `json:"avg_p95_ms"`
}

type check struct {
	Ok     bool   `json:"ok"`
	Reason string `json:"reason"`
}

type decision struct {
	Time     string      `json:"time"`
	BuildTag string      `json:"build_tag"`
	Verdict  string      `json:"verdict"`
	Baseline windowStats `json:"baseline"`
	Canary   windowStats `json:"canary"`
	Checks   []check     `json:"checks"`
}

// outcome embeds decision so outcomes.jsonl is self-contained.
type outcome struct {
	decision
	Label      string `json:"label"`
	LabelledAt string `json:"labelled_at"`
}

func binDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func readDecisions() ([]decision, error) {
	f, err := os.Open(filepath.Join(binDir(), decisionsFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []decision
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			var d decision
			if err := json.Unmarshal([]byte(line), &d); err != nil {
				return nil, fmt.Errorf("parse decision: %w", err)
			}
			out = append(out, d)
		}
	}
	return out, sc.Err()
}

func readLabelled() (map[string]bool, error) {
	f, err := os.Open(filepath.Join(binDir(), outcomesFile))
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			var o outcome
			if json.Unmarshal([]byte(line), &o) == nil {
				seen[o.BuildTag] = true
			}
		}
	}
	return seen, sc.Err()
}

func appendOutcome(o outcome) error {
	f, err := os.OpenFile(filepath.Join(binDir(), outcomesFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

func main() {
	decisions, err := readDecisions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", decisionsFile, err)
		os.Exit(1)
	}
	if len(decisions) == 0 {
		fmt.Println("no decisions found")
		return
	}

	labelled, err := readLabelled()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", outcomesFile, err)
		os.Exit(1)
	}

	var pending []decision
	for _, d := range decisions {
		if !labelled[d.BuildTag] {
			pending = append(pending, d)
		}
	}
	if len(pending) == 0 {
		fmt.Println("all decisions already labelled")
		return
	}
	fmt.Printf("%d decision(s) to label\n\n", len(pending))

	reader := bufio.NewReader(os.Stdin)
	done := 0

	for _, d := range pending {
		fmt.Println("─────────────────────────────────────────")
		fmt.Printf("build:   %s\n", d.BuildTag)
		fmt.Printf("time:    %s\n", d.Time)
		fmt.Printf("verdict: %s\n", d.Verdict)
		for _, c := range d.Checks {
			sym := "✓"
			if !c.Ok {
				sym = "✗"
			}
			fmt.Printf("  %s %s\n", sym, c.Reason)
		}
		fmt.Println()

		var label string
		for {
			fmt.Print("[p]romoted / [r]olled-back / [s]kip: ")
			answer, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println()
				goto done
			}
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "p", "promoted":
				label = "promoted"
			case "r", "rolled-back":
				label = "rolled-back"
			case "s", "skip":
				label = ""
			default:
				fmt.Println("  enter p, r, or s")
				continue
			}
			break
		}
		fmt.Println()

		if label == "" {
			continue
		}

		if err := appendOutcome(outcome{
			decision:   d,
			Label:      label,
			LabelledAt: time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "error writing outcome: %v\n", err)
			os.Exit(1)
		}
		done++
	}

done:
	fmt.Printf("labelled %d decision(s) → %s\n", done, outcomesFile)
}
