package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Value [2]json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// queryInstant runs an instant PromQL query and returns the first scalar result.
// Returns (0, false, nil) when the result set is empty (e.g. job not up yet).
func queryInstant(promURL, promql string) (float64, bool, error) {
	u := promURL + "/api/v1/query?query=" + url.QueryEscape(promql)
	resp, err := httpClient.Get(u) //nolint:gosec
	if err != nil {
		return 0, false, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()

	var pr promResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return 0, false, fmt.Errorf("decode: %w", err)
	}
	if pr.Status != "success" {
		return 0, false, fmt.Errorf("prometheus status %q", pr.Status)
	}
	if len(pr.Data.Result) == 0 {
		return 0, false, nil
	}

	var valStr string
	if err := json.Unmarshal(pr.Data.Result[0].Value[1], &valStr); err != nil {
		return 0, false, fmt.Errorf("unmarshal value: %w", err)
	}
	if valStr == "NaN" || valStr == "+Inf" || valStr == "-Inf" {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse float %q: %w", valStr, err)
	}
	return v, true, nil
}

func queryJob(promURL, job string) (windowStats, bool, error) {
	type metric struct {
		promql string
		dest   *int64
	}
	var stats windowStats
	queries := []metric{
		{fmt.Sprintf(`increase(whatfpl_requests_total{job=%q}[%s])`, job, promWindow), &stats.requests},
		{fmt.Sprintf(`increase(whatfpl_errors_4xx_total{job=%q}[%s])`, job, promWindow), &stats.errors4xx},
		{fmt.Sprintf(`increase(whatfpl_errors_5xx_total{job=%q}[%s])`, job, promWindow), &stats.errors5xx},
		{fmt.Sprintf(`histogram_quantile(0.95, rate(whatfpl_request_duration_ms_bucket{job=%q}[%s]))`, job, promWindow), &stats.avgP95Ms},
	}

	for _, q := range queries {
		v, ok, err := queryInstant(promURL, q.promql)
		if err != nil {
			return windowStats{}, false, fmt.Errorf("query %q: %w", q.promql, err)
		}
		if !ok {
			return windowStats{}, false, nil
		}
		*q.dest = int64(math.Round(v))
	}
	return stats, true, nil
}
