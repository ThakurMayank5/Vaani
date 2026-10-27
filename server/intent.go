package main

import (
	"fmt"
	"strings"
)

type IntentType string

const (
	IntentTurnOn      IntentType = "TURN_ON"
	IntentTurnOff     IntentType = "TURN_OFF"
	IntentToggle      IntentType = "TOGGLE"
	IntentQueryStatus IntentType = "QUERY_STATUS"
	IntentAllOn       IntentType = "ALL_ON"
	IntentAllOff      IntentType = "ALL_OFF"
	IntentUnknown     IntentType = "UNKNOWN"
)

// defaultDeviceAliases maps device IDs to common phonetic or colloquial variations.
// High false-positive words like "hello", "know", "yellow", and "members" are strictly excluded
// to prevent ambient background chatter in noisy rooms from accidentally triggering lights.
var defaultDeviceAliases = map[int][]string{
	DeviceDeskLight: {
		"desk", "desk lamp", "table light", "prism", "prison", "prizm", "prisms", "prismatic",
		"film", "plism", "study lamp", "study light", "light 1", "light one", "device 1", "device one",
		"one", "first", "first light", "1st light",
	},
	DeviceReadingLight: {
		"reading", "read light", "book light", "halo", "helo", "hello", "hailo", "bedside light", "bedside lamp",
		"light 2", "light two", "device 2", "device two", "two", "second", "second light", "2nd light",
	},
	DeviceAccentLight: {
		"accent", "ascent light", "ambient light", "ember", "amber", "amber light", "mood light",
		"light 3", "light three", "device 3", "device three", "three", "third", "third light", "3rd light",
	},
	DeviceNightLight: {
		"night", "nite light", "bed light", "nova", "noah", "nover", "novo", "sleep light",
		"light 4", "light four", "device 4", "device four", "four", "fourth", "fourth light", "4th light",
	},
}

// Tokens permitted in global room-wide phrases without treating them as unknown device qualifiers.
var globalAllowedTokens = map[string]bool{
	"turn": true, "switch": true, "power": true, "on": true, "off": true, "out": true,
	"the": true, "a": true, "an": true, "please": true, "my": true,
	"can": true, "could": true, "you": true, "would": true, "it": true,
	"all": true, "every": true, "everything": true, "entire": true, "whole": true,
	"room": true, "living": true, "house": true, "home": true,
	"lights": true, "light": true, "devices": true, "device": true,
	"hey": true, "vaani": true, "vani": true, "ok": true, "okay": true, "now": true, "just": true,
	"sab": true, "saari": true, "sabhi": true, "kuch": true, "karo": true, "kar": true, "do": true, "band": true, "chalu": true,
}

// Global scope keywords triggering multi-device operations.
var globalAllKeywords = []string{
	"all", "all lights", "all the lights", "everything", "every light", "entire room", "whole room", "lights", "light",
	"sab", "sab lights", "sab kuch", "saari lights", "sabhi lights", "all devices",
}

// ParsedIntent represents the structured command extracted from raw transcribed speech.
type ParsedIntent struct {
	Intent     IntentType `json:"intent"`
	Action     string     `json:"action,omitempty"` // "ON" | "OFF" | "QUERY"
	DeviceIDs  []int      `json:"deviceIds"`
	TargetName string     `json:"targetName,omitempty"`
	Confidence float64    `json:"confidence"`
	RawText    string     `json:"rawText"`
	MatchedOn  string     `json:"matchedOn,omitempty"`
}

// IntentRequest defines the payload received at POST /api/intent.
type IntentRequest struct {
	Text    string `json:"text"`
	Execute *bool  `json:"execute,omitempty"` // Optional: defaults to true if omitted
}

// DeviceExecutionResult captures the outcome of sending a command to an individual device.
type DeviceExecutionResult struct {
	Device  int    `json:"device"`
	Name    string `json:"name"`
	GPIO    int    `json:"gpio"`
	State   string `json:"state"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// IntentResponse is the HTTP response returned by the intent handler.
type IntentResponse struct {
	Success  bool                    `json:"success"`
	Message  string                  `json:"message"`
	Intent   ParsedIntent            `json:"intent"`
	Executed bool                    `json:"executed"`
	Results  []DeviceExecutionResult `json:"results,omitempty"`
}

// hasWholeWordOrPhrase checks whether a target word or phrase exists as complete tokens.
// Prevents "all" matching "hall"/"call" or "state" matching "estate".
func hasWholeWordOrPhrase(clean string, target string) bool {
	target = NormalizeText(target)
	if target == "" {
		return false
	}

	targetTokens := Tokenize(target)
	inputTokens := Tokenize(clean)

	if len(targetTokens) == 0 || len(inputTokens) < len(targetTokens) {
		return false
	}

	if len(targetTokens) == 1 {
		word := targetTokens[0]
		for _, tok := range inputTokens {
			if tok == word {
				return true
			}
		}
		return false
	}

	// For multi-word phrases, check sequential token match
	for i := 0; i <= len(inputTokens)-len(targetTokens); i++ {
		match := true
		for j := 0; j < len(targetTokens); j++ {
			if inputTokens[i+j] != targetTokens[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}

	return false
}

// DetectAction identifies whether the utterance intends an ON, OFF, TOGGLE, or QUERY action.
func DetectAction(clean string) (action string, isQuery bool, isToggle bool) {
	// 1. Status query phrases (whole phrase match)
	queryPhrases := []string{
		"what is the status", "what are the lights", "is the light on", "is the light off",
		"are the lights on", "are the lights off", "status", "state", "check", "kya status hai",
	}
	for _, qp := range queryPhrases {
		if hasWholeWordOrPhrase(clean, qp) {
			return "QUERY", true, false
		}
	}

	// 2. Off phrases (checked before "on" to prevent "turn off" matching "turn on")
	offPhrases := []string{
		"turn off", "switch off", "power off", "shut down", "shut off",
		"turn of", "switch of", "deactivate", "kill", "disable", "stop",
		"band karo", "band kar do", "band kar", "bujha do",
	}
	for _, op := range offPhrases {
		if hasWholeWordOrPhrase(clean, op) {
			return "OFF", false, false
		}
	}

	// 3. On phrases
	onPhrases := []string{
		"turn on", "switch on", "power on", "activate", "enable", "start",
		"chalu karo", "chalu kar do", "chalu kar", "jala do", "on karo",
	}
	for _, op := range onPhrases {
		if hasWholeWordOrPhrase(clean, op) {
			return "ON", false, false
		}
	}

	// 4. Standalone token checks (e.g. "halo on", "desk light off")
	tokens := Tokenize(clean)
	for _, token := range tokens {
		if token == "off" || token == "out" || token == "band" {
			return "OFF", false, false
		}
		if token == "on" || token == "chalu" {
			return "ON", false, false
		}
	}

	// 5. Toggle phrases
	togglePhrases := []string{"toggle", "flip", "invert", "badal do"}
	for _, tp := range togglePhrases {
		if hasWholeWordOrPhrase(clean, tp) {
			return "TOGGLE", false, true
		}
	}

	return "", false, false
}

// ParseIntent extracts structured intent from raw speech transcript using fuzzy logic.
func ParseIntent(rawTranscript string, currentDevices []DeviceInfo) ParsedIntent {
	clean := NormalizeText(rawTranscript)
	if clean == "" {
		return ParsedIntent{
			Intent:     IntentUnknown,
			Confidence: 0.0,
			RawText:    rawTranscript,
		}
	}

	action, _, isToggle := DetectAction(clean)

	// Check if this is a global command ("all lights", "everything", "lights off", "light on")
	isGlobal := false
	for _, kw := range globalAllKeywords {
		if hasWholeWordOrPhrase(clean, kw) {
			if kw == "lights" || kw == "all lights" || kw == "light" {
				// Guard: Only treat as global if all tokens in the phrase are generic/scope words.
				// If there is an unrecognized noun (e.g. "laptop" in "laptop light"), do NOT trigger all lights.
				hasUnrecognizedQualifier := false
				for _, tok := range Tokenize(clean) {
					if !globalAllowedTokens[tok] {
						hasUnrecognizedQualifier = true
						break
					}
				}
				if !hasUnrecognizedQualifier {
					isGlobal = true
					break
				}
			} else {
				isGlobal = true
				break
			}
		}
	}

	if isGlobal && (action == "ON" || action == "OFF") {
		var allDeviceIDs []int
		for _, dev := range currentDevices {
			allDeviceIDs = append(allDeviceIDs, dev.Device)
		}

		if action == "ON" {
			return ParsedIntent{
				Intent:     IntentAllOn,
				Action:     "ON",
				DeviceIDs:  allDeviceIDs,
				TargetName: "All Devices",
				Confidence: 0.95,
				RawText:    rawTranscript,
				MatchedOn:  "global:all_on",
			}
		}
		return ParsedIntent{
			Intent:     IntentAllOff,
			Action:     "OFF",
			DeviceIDs:  allDeviceIDs,
			TargetName: "All Devices",
			Confidence: 0.95,
			RawText:    rawTranscript,
			MatchedOn:  "global:all_off",
		}
	}

	// Score each device candidate against the transcript
	type candidateMatch struct {
		device    DeviceInfo
		score     float64
		matchedOn string
	}

	var bestMatch *candidateMatch

	for _, dev := range currentDevices {
		candidates := []struct {
			text  string
			label string
		}{
			{dev.Name, "name"},
			{dev.CustomWord, "custom_word"},
		}

		// Add leading prefix of device name (e.g. "desk" for "Desk Light")
		tokens := Tokenize(dev.Name)
		if len(tokens) > 1 {
			candidates = append(candidates, struct {
				text  string
				label string
			}{tokens[0], "name_prefix"})
		}

		// Add phonetic and common ASR mishearings
		if aliases, ok := defaultDeviceAliases[dev.Device]; ok {
			for _, alias := range aliases {
				candidates = append(candidates, struct {
					text  string
					label string
				}{alias, "alias"})
			}
		}

		for _, cand := range candidates {
			score, span := BestFuzzyMatch(clean, cand.text)
			// Guard: "night" vs "light" differ by only 1 letter ('n' vs 'l', sim=0.80).
			// The generic domain word "light" must never fuzzily match the specific qualifier "night".
			candLower := strings.ToLower(cand.text)
			if (candLower == "night" || candLower == "nite") && (span == "light" || span == "lights") {
				continue
			}

			// Threshold: 0.80 prevents 4-letter collisions like "hall" matching "halo" (which is 0.75)
			if score >= 0.80 {
				if bestMatch == nil || score > bestMatch.score {
					bestMatch = &candidateMatch{
						device:    dev,
						score:     score,
						matchedOn: fmt.Sprintf("%s:%s (matched '%s' with sim=%.2f)", cand.label, cand.text, span, score),
					}
				}
			}
		}
	}

	if bestMatch == nil {
		return ParsedIntent{
			Intent:     IntentUnknown,
			Action:     action,
			Confidence: 0.2,
			RawText:    rawTranscript,
		}
	}

	// Resolve IntentType
	var resolvedIntent IntentType
	switch {
	case action == "ON":
		resolvedIntent = IntentTurnOn
	case action == "OFF":
		resolvedIntent = IntentTurnOff
	case isToggle:
		resolvedIntent = IntentToggle
	case action == "QUERY":
		resolvedIntent = IntentQueryStatus
	default:
		resolvedIntent = IntentUnknown
	}

	return ParsedIntent{
		Intent:     resolvedIntent,
		Action:     action,
		DeviceIDs:  []int{bestMatch.device.Device},
		TargetName: bestMatch.device.Name,
		Confidence: bestMatch.score,
		RawText:    rawTranscript,
		MatchedOn:  bestMatch.matchedOn,
	}
}
