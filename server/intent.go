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
	},
	DeviceReadingLight: {
		"reading", "read light", "book light", "halo", "helo",
	},
	DeviceAccentLight: {
		"accent", "ascent light", "ambient light", "ember", "amber", "amber light",
	},
	DeviceNightLight: {
		"night", "nite light", "bed light", "nova", "noah", "nover", "novo",
	},
}

// Tokens permitted in global room-wide phrases without treating them as unknown device qualifiers.
var globalAllowedTokens = map[string]bool{
	"turn": true, "switch": true, "power": true, "on": true, "off": true, "out": true,
	"the": true, "a": true, "an": true, "please": true, "my": true,
	"can": true, "could": true, "you": true, "would": true, "it": true,
	"all": true, "every": true, "everything": true, "entire": true, "whole": true,
	"room": true, "living": true, "house": true, "home": true,
	"lights": true, "light": true,
}

// Global scope keywords triggering multi-device operations.
var globalAllKeywords = []string{
	"all", "all lights", "all the lights", "everything", "every light", "entire room", "whole room", "lights", "light",
}

// ParsedIntent represents the structured command extracted from raw transcribed speech.
type ParsedIntent struct {
	Intent               IntentType `json:"intent"`
	Action               string     `json:"action,omitempty"` // "ON" | "OFF" | "QUERY"
	DeviceIDs            []int      `json:"deviceIds"`
	TargetName           string     `json:"targetName,omitempty"`
	Confidence           float64    `json:"confidence"`
	RawText              string     `json:"rawText"`
	MatchedOn            string     `json:"matchedOn,omitempty"`
	RequiresConfirmation bool       `json:"requiresConfirmation"`
}

// IntentRequest defines the payload received at POST /api/intent.
type IntentRequest struct {
	Text      string `json:"text"`
	Execute   *bool  `json:"execute,omitempty"`   // Optional: defaults to true if omitted
	Confirmed *bool  `json:"confirmed,omitempty"` // Optional: set to true to execute commands that require confirmation
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
	Success              bool                    `json:"success"`
	Message              string                  `json:"message"`
	Intent               ParsedIntent            `json:"intent"`
	Executed             bool                    `json:"executed"`
	RequiresConfirmation bool                    `json:"requiresConfirmation"`
	Results              []DeviceExecutionResult `json:"results,omitempty"`
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

var determiners = map[string]bool{
	"the": true, "a": true, "an": true, "my": true, "this": true, "that": true,
	"these": true, "those": true, "their": true, "our": true, "your": true, "some": true, "his": true, "her": true,
}

func hasToken(tokens []string, words ...string) bool {
	for _, t := range tokens {
		for _, w := range words {
			if t == w {
				return true
			}
		}
	}
	return false
}

// DetectAction identifies whether the utterance intends an ON, OFF, TOGGLE, or QUERY action along with action confidence.
func DetectAction(clean string) (action string, actionConf float64, isQuery bool, isToggle bool) {
	// 1. Status query phrases (whole phrase match)
	queryPhrases := []string{
		"what is the status", "what are the lights", "is the light on", "is the light off",
		"are the lights on", "are the lights off", "status", "state", "check",
	}
	for _, qp := range queryPhrases {
		if hasWholeWordOrPhrase(clean, qp) {
			return "QUERY", 1.00, true, false
		}
	}

	// 2. Off phrases (checked before "on" to prevent "turn off" matching "turn on")
	offPhrases := []string{
		"turn off", "switch off", "power off", "shut down", "shut off",
		"turn of", "switch of", "deactivate", "kill", "disable", "stop",
		"darken", "extinguish", "lights out",
	}
	for _, op := range offPhrases {
		if hasWholeWordOrPhrase(clean, op) {
			return "OFF", 1.00, false, false
		}
	}

	// 3. On phrases
	onPhrases := []string{
		"turn on", "switch on", "power on", "activate", "enable", "start",
		"illuminate", "illumination", "light up", "brighten",
	}
	for _, op := range onPhrases {
		if hasWholeWordOrPhrase(clean, op) {
			return "ON", 1.00, false, false
		}
	}

	// 4. Standalone token checks (e.g. "halo on", "desk light off")
	// Accept bare "on"/"off" ONLY when it is the last token, or when the next token is NOT an article or determiner
	// (Prevents "on the desk" or "off a cliff" from triggering ON/OFF).
	tokens := Tokenize(clean)
	for i, token := range tokens {
		if token == "off" || token == "out" {
			if i == len(tokens)-1 || !determiners[tokens[i+1]] {
				return "OFF", 0.85, false, false
			}
		}
		if token == "on" {
			if i == len(tokens)-1 || !determiners[tokens[i+1]] {
				return "ON", 0.85, false, false
			}
		}
	}

	// 5. Toggle phrases
	togglePhrases := []string{"toggle", "flip", "invert"}
	for _, tp := range togglePhrases {
		if hasWholeWordOrPhrase(clean, tp) {
			return "TOGGLE", 1.00, false, true
		}
	}

	return "", 0.0, false, false
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

	// 1. Smart home bedtime routines (e.g. "time for sleep", "good night", "bedtime")
	sleepPhrases := []string{"time for sleep", "go to sleep", "good night", "goodnight", "bedtime", "going to bed"}
	for _, sp := range sleepPhrases {
		if hasWholeWordOrPhrase(clean, sp) {
			var allDeviceIDs []int
			for _, dev := range currentDevices {
				allDeviceIDs = append(allDeviceIDs, dev.Device)
			}
			return ParsedIntent{
				Intent:               IntentAllOff,
				Action:               "OFF",
				DeviceIDs:            allDeviceIDs,
				TargetName:           "All Devices",
				Confidence:           0.95,
				RawText:              rawTranscript,
				MatchedOn:            "routine:sleep",
				RequiresConfirmation: true, // Bedtime proposal: confirm before turning off entire home
			}
		}
	}

	action, actionConf, isQuery, isToggle := DetectAction(clean)

	// 2. Score candidate devices FIRST!
	// (Fixes priority bug where "all night lights" triggered all_on by matching "all" before checking device)
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

	// 3. If a specific device matched, resolve it with combined confidence!
	if bestMatch != nil {
		if action == "" {
			return ParsedIntent{
				Intent:     IntentUnknown,
				Action:     "",
				DeviceIDs:  []int{bestMatch.device.Device},
				TargetName: bestMatch.device.Name,
				Confidence: 0.20,
				RawText:    rawTranscript,
				MatchedOn:  bestMatch.matchedOn,
			}
		}

		var resolvedIntent IntentType
		switch {
		case action == "ON":
			resolvedIntent = IntentTurnOn
		case action == "OFF":
			resolvedIntent = IntentTurnOff
		case isToggle:
			resolvedIntent = IntentToggle
		case isQuery || action == "QUERY":
			resolvedIntent = IntentQueryStatus
		default:
			resolvedIntent = IntentUnknown
		}

		// Combined confidence: min(actionConf, deviceConf)
		combinedConf := bestMatch.score
		if actionConf < combinedConf {
			combinedConf = actionConf
		}

		return ParsedIntent{
			Intent:     resolvedIntent,
			Action:     action,
			DeviceIDs:  []int{bestMatch.device.Device},
			TargetName: bestMatch.device.Name,
			Confidence: combinedConf,
			RawText:    rawTranscript,
			MatchedOn:  bestMatch.matchedOn,
		}
	}

	// 4. Global fallback: ONLY if no specific device was matched
	isGlobal := false
	tokens := Tokenize(clean)

	for _, kw := range globalAllKeywords {
		if hasWholeWordOrPhrase(clean, kw) {
			if kw == "lights" || kw == "all lights" || kw == "light" || kw == "all" || kw == "every" || kw == "everything" {
				// Guard: Only treat as global if all tokens in the phrase are generic/scope words.
				// If there is an unrecognized noun (e.g. "laptop" in "laptop light"), do NOT trigger all lights.
				hasUnrecognizedQualifier := false
				for _, tok := range tokens {
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

		globalConf := 0.95
		if actionConf < globalConf {
			globalConf = actionConf
		}

		if action == "ON" {
			return ParsedIntent{
				Intent:     IntentAllOn,
				Action:     "ON",
				DeviceIDs:  allDeviceIDs,
				TargetName: "All Devices",
				Confidence: globalConf,
				RawText:    rawTranscript,
				MatchedOn:  "global:all_on",
			}
		}
		return ParsedIntent{
			Intent:     IntentAllOff,
			Action:     "OFF",
			DeviceIDs:  allDeviceIDs,
			TargetName: "All Devices",
			Confidence: globalConf,
			RawText:    rawTranscript,
			MatchedOn:  "global:all_off",
		}
	}

	// 5. Unrecognized / Ambiguous
	return ParsedIntent{
		Intent:     IntentUnknown,
		Action:     action,
		Confidence: 0.20,
		RawText:    rawTranscript,
	}
}
