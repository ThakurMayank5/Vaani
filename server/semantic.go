package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	defaultSemanticURL = "http://127.0.0.1:42070/api/semantic_intent"
	semanticHTTPClient = &http.Client{
		Timeout: 2 * time.Second,
	}
)

func getSemanticURL() string {
	if val := os.Getenv("SEMANTIC_PARSER_URL"); val != "" {
		return val
	}
	return defaultSemanticURL
}

func isSemanticFallbackEnabled() bool {
	val := strings.ToLower(os.Getenv("SEMANTIC_PARSER_URL"))
	return val != "disabled" && val != "false" && val != "off"
}

// SemanticIntentPayload is sent to the fallback semantic engine.
type SemanticIntentPayload struct {
	Text    string       `json:"text"`
	Devices []DeviceInfo `json:"devices"`
}

// SemanticIntentResult is returned by the fallback semantic engine.
type SemanticIntentResult struct {
	Success              bool    `json:"success"`
	Intent               string  `json:"intent"` // "TURN_ON", "TURN_OFF", etc.
	Action               string  `json:"action,omitempty"`
	DeviceIDs            []int   `json:"deviceIds"`
	TargetName           string  `json:"targetName,omitempty"`
	Confidence           float64 `json:"confidence"`
	MatchedOn            string  `json:"matchedOn,omitempty"`
	Model                string  `json:"model,omitempty"`
	Message              string  `json:"message,omitempty"`
	RequiresConfirmation bool    `json:"requiresConfirmation"`
	LatencyMs            float64 `json:"latencyMs,omitempty"`
}

// QuerySemanticFallback reaches out to the semantic decision engine (e.g. Laya / SLM)
// when the deterministic fuzzy parser cannot determine a confident match.
func QuerySemanticFallback(ctx context.Context, rawText string, devices []DeviceInfo) (ParsedIntent, error) {
	if !isSemanticFallbackEnabled() {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("semantic fallback disabled")
	}

	url := getSemanticURL()
	payload := SemanticIntentPayload{
		Text:    rawText,
		Devices: devices,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("failed to marshal semantic payload: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("failed to create semantic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := semanticHTTPClient.Do(req)
	if err != nil {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("semantic parser unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("semantic parser returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result SemanticIntentResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("failed to decode semantic response: %w", err)
	}

	if !result.Success {
		return ParsedIntent{
			Intent:     IntentUnknown,
			Confidence: result.Confidence,
			RawText:    rawText,
		}, nil
	}

	// Semantic Polarity Cross-Check Guard:
	// Positive illumination phrases (e.g. "dark", "bright", "illuminate") must NEVER trigger TURN_OFF
	lower := strings.ToLower(rawText)
	isBrightenContext := strings.Contains(lower, "dark") || strings.Contains(lower, "bright") || strings.Contains(lower, "illuminate") || strings.Contains(lower, "light up")
	isSleepContext := strings.Contains(lower, "sleep") || strings.Contains(lower, "bedtime") || strings.Contains(lower, "goodnight")

	if isBrightenContext && (result.Intent == "TURN_OFF" || result.Action == "OFF") {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("polarity guard: brightening phrase cannot trigger OFF")
	}
	if isSleepContext && (result.Intent == "TURN_ON" || result.Action == "ON") {
		return ParsedIntent{Intent: IntentUnknown, RawText: rawText}, fmt.Errorf("polarity guard: sleep phrase cannot trigger ON")
	}

	matchedLabel := fmt.Sprintf("semantic:%s", result.Model)
	if result.MatchedOn != "" {
		matchedLabel += fmt.Sprintf(" (%s)", result.MatchedOn)
	}

	return ParsedIntent{
		Intent:               IntentType(result.Intent),
		Action:               result.Action,
		DeviceIDs:            result.DeviceIDs,
		TargetName:           result.TargetName,
		Confidence:           result.Confidence,
		RawText:              rawText,
		MatchedOn:            matchedLabel,
		RequiresConfirmation: result.RequiresConfirmation,
	}, nil
}

