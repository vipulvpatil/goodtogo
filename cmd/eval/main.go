package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const outcomesFile = "outcomes.jsonl"

type outcome struct {
	BuildTag   string `json:"build_tag"`
	Verdict    string `json:"verdict"`     // rule-based: "GOOD TO GO" | "NOT GOOD TO GO"
	LLMVerdict string `json:"llm_verdict"` // "GOOD TO GO" | "NOT GOOD TO GO" | "skipped" | "error"
	Label      string `json:"label"`       // "promoted" | "rolled-back"
}

func dataDir() string {
	if dir := os.Getenv("GOODTOGO_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share", "goodtogo")
}

func readOutcomes() ([]outcome, error) {
	f, err := os.Open(filepath.Join(dataDir(), outcomesFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []outcome
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			var o outcome
			if err := json.Unmarshal([]byte(line), &o); err != nil {
				return nil, fmt.Errorf("parse outcome: %w", err)
			}
			out = append(out, o)
		}
	}
	return out, sc.Err()
}

type counts struct{ tp, tn, fp, fn int }

func (c counts) total() int { return c.tp + c.tn + c.fp + c.fn }

func score(verdict string, label string) (tp, tn, fp, fn int) {
	approved := verdict == "GOOD TO GO"
	good := label == "promoted"
	switch {
	case approved && good:
		tp = 1
	case !approved && !good:
		tn = 1
	case approved && !good:
		fp = 1 // dangerous: approved a bad deploy
	case !approved && good:
		fn = 1 // annoying: blocked a good deploy
	}
	return
}

func pct(num, den int) string {
	if den == 0 {
		return "  n/a"
	}
	return fmt.Sprintf("%5.1f%%", 100*float64(num)/float64(den))
}

func printMatrix(label string, c counts) {
	fmt.Printf("%s  (n=%d)\n", label, c.total())
	fmt.Printf("                 promoted  rolled-back\n")
	fmt.Printf("GOOD TO GO       %4d TP   %4d FP\n", c.tp, c.fp)
	fmt.Printf("NOT GOOD TO GO   %4d FN   %4d TN\n", c.fn, c.tn)
	fmt.Println()
	fmt.Printf("  accuracy   %s  (TP+TN / total)\n", pct(c.tp+c.tn, c.total()))
	fmt.Printf("  precision  %s  (TP / TP+FP  — of approvals, how many were right)\n", pct(c.tp, c.tp+c.fp))
	fmt.Printf("  recall     %s  (TP / TP+FN  — of good deploys, how many approved)\n", pct(c.tp, c.tp+c.fn))
	fmt.Printf("  FP rate    %s  (FP / FP+TN  — of bad deploys, how many slipped)\n", pct(c.fp, c.fp+c.tn))
}

func main() {
	outcomes, err := readOutcomes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", outcomesFile, err)
		os.Exit(1)
	}
	if len(outcomes) == 0 {
		fmt.Println("no labelled outcomes found — run the label tool first")
		return
	}

	var rules, llm counts
	var llmSkipped int

	for _, o := range outcomes {
		tp, tn, fp, fn := score(o.Verdict, o.Label)
		rules.tp += tp
		rules.tn += tn
		rules.fp += fp
		rules.fn += fn

		switch o.LLMVerdict {
		case "GOOD TO GO", "NOT GOOD TO GO":
			tp, tn, fp, fn = score(o.LLMVerdict, o.Label)
			llm.tp += tp
			llm.tn += tn
			llm.fp += fp
			llm.fn += fn
		default:
			llmSkipped++ // "skipped", "error", or empty (older records)
		}
	}

	fmt.Printf("outcomes: %d\n\n", len(outcomes))

	printMatrix("rule-based", rules)

	fmt.Println()

	if llm.total() == 0 {
		fmt.Println("LLM  — no verdicts recorded (set ANTHROPIC_API_KEY and re-run checks)")
	} else {
		if llmSkipped > 0 {
			fmt.Printf("LLM  (%d outcome(s) excluded: no LLM verdict)\n\n", llmSkipped)
		}
		printMatrix("LLM", llm)
	}
}
