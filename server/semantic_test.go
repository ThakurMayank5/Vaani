package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestQuerySemanticFallback_Success(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload SemanticIntentPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}

		resp := SemanticIntentResult{
			Success:    true,
			Intent:     "TURN_ON",
			Action:     "ON",
			DeviceIDs:  []int{1},
			TargetName: "Desk Light",
			Confidence: 0.92,
			MatchedOn:  "choice:TURN_ON/Prism",
			Model:      "laya",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer fakeServer.Close()

	os.Setenv("SEMANTIC_PARSER_URL", fakeServer.URL)
	defer os.Unsetenv("SEMANTIC_PARSER_URL")

	devices := []DeviceInfo{
		{Device: 1, Name: "Desk Light", CustomWord: "Prism"},
	}

	intent, err := QuerySemanticFallback(context.Background(), "make it brighter", devices)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if intent.Intent != IntentTurnOn {
		t.Errorf("expected IntentTurnOn, got %s", intent.Intent)
	}
	if intent.Action != "ON" {
		t.Errorf("expected Action ON, got %s", intent.Action)
	}
	if len(intent.DeviceIDs) != 1 || intent.DeviceIDs[0] != 1 {
		t.Errorf("expected DeviceID 1, got %v", intent.DeviceIDs)
	}
	if intent.MatchedOn != "semantic:laya (choice:TURN_ON/Prism)" {
		t.Errorf("expected matchedOn label, got %s", intent.MatchedOn)
	}
}

func TestQuerySemanticFallback_Unreachable(t *testing.T) {
	os.Setenv("SEMANTIC_PARSER_URL", "http://127.0.0.1:54321/unreachable")
	defer os.Unsetenv("SEMANTIC_PARSER_URL")

	_, err := QuerySemanticFallback(context.Background(), "test text", nil)
	if err == nil {
		t.Errorf("expected error when semantic parser is unreachable, got nil")
	}
}

func TestQuerySemanticFallback_Disabled(t *testing.T) {
	os.Setenv("SEMANTIC_PARSER_URL", "disabled")
	defer os.Unsetenv("SEMANTIC_PARSER_URL")

	intent, err := QuerySemanticFallback(context.Background(), "test text", nil)
	if err == nil {
		t.Errorf("expected error when semantic fallback is disabled")
	}
	if intent.Intent != IntentUnknown {
		t.Errorf("expected IntentUnknown, got %s", intent.Intent)
	}
}

func TestQuerySemanticFallback_PolarityGuard(t *testing.T) {
	// Fake server erroneously proposing TURN_OFF when user said "it is getting dark"
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SemanticIntentResult{
			Success:    true,
			Intent:     "TURN_OFF",
			Action:     "OFF",
			DeviceIDs:  []int{1},
			TargetName: "Desk Light",
			Confidence: 0.85,
			Model:      "laya",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer fakeServer.Close()

	os.Setenv("SEMANTIC_PARSER_URL", fakeServer.URL)
	defer os.Unsetenv("SEMANTIC_PARSER_URL")

	// "it is getting dark" must trigger polarity guard and reject TURN_OFF
	intent, err := QuerySemanticFallback(context.Background(), "it is getting dark", nil)
	if err == nil {
		t.Errorf("expected polarity guard error when brightening phrase yields TURN_OFF")
	}
	if intent.Intent != IntentUnknown {
		t.Errorf("expected IntentUnknown on polarity violation, got %s", intent.Intent)
	}
}

