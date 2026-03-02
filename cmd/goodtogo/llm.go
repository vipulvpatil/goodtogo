package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
)

// llmAnalyze calls Claude to independently assess whether the canary looks safe.
// Returns verdict ("GOOD TO GO" | "NOT GOOD TO GO" | "error" | "skipped") and a reason.
// The result is informational only — it does not affect the gate decision.
func llmAnalyze(base, canary windowStats) (verdict, reason string) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return "skipped", "ANTHROPIC_API_KEY not set"
	}

	client := anthropic.NewClient()

	prompt := fmt.Sprintf(`You are a deployment safety expert reviewing a canary rollout.
Analyze the following production metrics and decide whether the canary is safe to promote.

Baseline (last 5 min):
  requests : %d
  5xx errors: %d (%.2f%%)
  4xx errors: %d (%.2f%%)
  avg p95   : %d ms

Canary (last 5 min):
  requests : %d
  5xx errors: %d (%.2f%%)
  4xx errors: %d (%.2f%%)
  avg p95   : %d ms

Reply with a single JSON object and nothing else:
{"verdict": "GOOD TO GO" | "NOT GOOD TO GO", "reason": "<one-sentence explanation>"}`,
		base.Requests,
		base.Errors5xx, rate(base.Errors5xx, base.Requests)*100,
		base.Errors4xx, rate(base.Errors4xx, base.Requests)*100,
		base.AvgP95Ms,
		canary.Requests,
		canary.Errors5xx, rate(canary.Errors5xx, canary.Requests)*100,
		canary.Errors4xx, rate(canary.Errors4xx, canary.Requests)*100,
		canary.AvgP95Ms,
	)

	resp, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_6,
		MaxTokens: 256,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "error", fmt.Sprintf("API error: %v", err)
	}

	if len(resp.Content) == 0 {
		return "error", "empty response from LLM"
	}

	text := resp.Content[0].Text

	var out struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return "error", fmt.Sprintf("could not parse LLM response: %v — raw: %s", err, text)
	}

	return out.Verdict, out.Reason
}
