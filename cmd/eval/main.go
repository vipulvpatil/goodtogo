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
	BuildTag string `json:"build_tag"`
	Verdict  string `json:"verdict"` // "GOOD TO GO" | "NOT GOOD TO GO"
	Label    string `json:"label"`   // "promoted" | "rolled-back"
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

func pct(num, den int) string {
	if den == 0 {
		return "  n/a"
	}
	return fmt.Sprintf("%5.1f%%", 100*float64(num)/float64(den))
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

	var tp, tn, fp, fn int
	for _, o := range outcomes {
		approved := o.Verdict == "GOOD TO GO"
		good := o.Label == "promoted"
		switch {
		case approved && good:
			tp++ // correctly approved a good deploy
		case !approved && !good:
			tn++ // correctly blocked a bad deploy
		case approved && !good:
			fp++ // wrongly approved a bad deploy  ← dangerous
		case !approved && good:
			fn++ // wrongly blocked a good deploy  ← annoying
		}
	}

	total := tp + tn + fp + fn

	fmt.Printf("outcomes: %d\n\n", total)
	fmt.Printf("                 promoted  rolled-back\n")
	fmt.Printf("GOOD TO GO       %4d TP   %4d FP\n", tp, fp)
	fmt.Printf("NOT GOOD TO GO   %4d FN   %4d TN\n", fn, tn)
	fmt.Println()
	fmt.Printf("accuracy   %s  (TP+TN / total — overall correct rate)\n", pct(tp+tn, total))
	fmt.Printf("precision  %s  (TP / TP+FP  — of approvals, how many were right)\n", pct(tp, tp+fp))
	fmt.Printf("recall     %s  (TP / TP+FN  — of good deploys, how many were approved)\n", pct(tp, tp+fn))
	fmt.Printf("FP rate    %s  (FP / FP+TN  — of bad deploys, how many slipped through)\n", pct(fp, fp+tn))
}
