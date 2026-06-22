package metrics

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
)

const rpcIdGetMetrics = "get_metrics"

func InitMetricsSystem(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if err := (*initializer).RegisterRpc(rpcIdGetMetrics, getMetricsRpc); err != nil {
		return err
	}

	return nil
}

// RPC handler
func getMetricsRpc(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	url := "http://3.110.162.73:9200/"

	resp, err := http.Get(url)
	if err != nil {
		logger.Error("Failed to fetch metrics: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("Failed to read response body: %v", err)
		return "", err
	}

	metrics := parsePrometheusText(string(body))

	jsonBytes, err := safeMarshalJSON(metrics)
	if err != nil {
		logger.Error("Failed to marshal metrics JSON: %v", err)
		return "", err
	}

	return string(jsonBytes), nil
}

func parsePrometheusText(text string) map[string]interface{} {
	result := make(map[string]interface{})
	scanner := bufio.NewScanner(strings.NewReader(text))
	reCurly := regexp.MustCompile(`\{.*\}`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		rawKey := parts[0]
		valueStr := parts[1]
		cleanKey := reCurly.ReplaceAllString(rawKey, "")

		// Handle NaN, +Inf, -Inf explicitly
		switch strings.ToLower(valueStr) {
		case "nan", "+nan", "-nan":
			result[cleanKey] = nil
		case "inf", "+inf":
			result[cleanKey] = math.Inf(1)
		case "-inf":
			result[cleanKey] = math.Inf(-1)
		default:
			if f, err := strconv.ParseFloat(valueStr, 64); err == nil {
				// replace infinities with nil to avoid JSON error
				if math.IsNaN(f) || math.IsInf(f, 0) {
					result[cleanKey] = nil
				} else {
					result[cleanKey] = f
				}
			} else {
				result[cleanKey] = valueStr
			}
		}
	}

	return result
}

func safeMarshalJSON(v map[string]interface{}) ([]byte, error) {
	safe := make(map[string]interface{}, len(v))
	for k, val := range v {
		switch x := val.(type) {
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				safe[k] = nil
			} else {
				safe[k] = x
			}
		default:
			safe[k] = x
		}
	}
	return json.MarshalIndent(safe, "", "  ")
}
