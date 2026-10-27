package main

import (
	"fmt"
	"sort"
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
	IntentMulti       IntentType = "MULTI_ACTION"
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
	Intent               IntentType     `json:"intent"`
	Action               string         `json:"action,omitempty"` // "ON" | "OFF" | "MULTI" | "QUERY"
	DeviceIDs            []int          `json:"deviceIds"`
	DeviceActions        map[int]string `json:"deviceActions,omitempty"`
	TargetName           string         `json:"targetName,omitempty"`
	Confidence           float64        `json:"confidence"`
	RawText              string         `json:"rawText"`
	MatchedOn            string         `json:"matchedOn,omitempty"`
	RequiresConfirmation bool           `json:"requiresConfirmation"`
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
		"are the lights on", "are the lights off", "status", "state", "check", "kya status hai",
	}
	for _, qp := range queryPhrases {
		if hasWholeWordOrPhrase(clean, qp) {
			return "QUERY", 1.00, true, false
		}
	}

	// 2. Off phrases (checked before "on" to prevent "turn off" matching "turn on")
	// Includes common ASR phonetic mishearings (e.g. Whisper transcribing "turn off" as "term of" or "tern off")
	offPhrases := []string{
		"turn off", "switch off", "power off", "shut down", "shut off",
		"turn of", "switch of", "power of",
		"term off", "term of", "tern off", "tern of", "terms of",
		"tune off", "tone off", "turned off", "turning off",
		"deactivate", "kill", "disable", "stop",
		"darken", "extinguish", "lights out",
		"band karo", "band kar do", "band kar", "bujha do",
	}
	for _, op := range offPhrases {
		if hasWholeWordOrPhrase(clean, op) {
			return "OFF", 1.00, false, false
		}
	}

	// 3. On phrases
	// Includes common ASR phonetic mishearings (e.g. "term on", "tune on", "turned on")
	onPhrases := []string{
		"turn on", "switch on", "power on",
		"term on", "tern on", "tune on", "tone on", "turned on", "turning on",
		"activate", "enable", "start",
		"illuminate", "illumination", "light up", "brighten",
		"chalu karo", "chalu kar do", "chalu kar", "jala do", "on karo",
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
		if token == "off" || token == "out" || token == "band" {
			if i == len(tokens)-1 || !determiners[tokens[i+1]] {
				return "OFF", 0.85, false, false
			}
		}
		if token == "on" || token == "chalu" {
			if i == len(tokens)-1 || !determiners[tokens[i+1]] {
				return "ON", 0.85, false, false
			}
		}
	}

	// 5. Toggle phrases
	togglePhrases := []string{"toggle", "flip", "invert", "badal do"}
	for _, tp := range togglePhrases {
		if hasWholeWordOrPhrase(clean, tp) {
			return "TOGGLE", 1.00, false, true
		}
	}

	// 6. Fuzzy verb fallback for noisy or accented ASR transcripts
	if len(tokens) >= 2 {
		for i := 0; i < len(tokens)-1; i++ {
			gram := tokens[i] + " " + tokens[i+1]
			if Similarity(gram, "turn off") >= 0.75 || Similarity(gram, "switch off") >= 0.75 {
				return "OFF", 0.85, false, false
			}
			if Similarity(gram, "turn on") >= 0.75 || Similarity(gram, "switch on") >= 0.75 {
				return "ON", 0.85, false, false
			}
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

	tokens := Tokenize(clean)
	action, actionConf, _, _ := DetectAction(clean)

	// 2. Score candidate devices FIRST! (Supports multiple devices, e.g. "turn on ember and halo", "turn off ember turn on halo")
	// (Fixes priority bug where "all night lights" triggered all_on by matching "all" before checking device)
	type candidateMatch struct {
		device    DeviceInfo
		score     float64
		matchedOn string
		startTok  int
		endTok    int
	}

	matchedDevices := make(map[int]candidateMatch)

	for _, dev := range currentDevices {
		candidates := []struct {
			text  string
			label string
		}{
			{dev.Name, "name"},
			{dev.CustomWord, "custom_word"},
		}

		// Add leading prefix of device name (e.g. "desk" for "Desk Light")
		devTokens := Tokenize(dev.Name)
		if len(devTokens) > 1 {
			candidates = append(candidates, struct {
				text  string
				label string
			}{devTokens[0], "name_prefix"})
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
			score, span, startTok, endTok := BestFuzzyMatchSpan(tokens, cand.text)
			// Guard: "night" vs "light" differ by only 1 letter ('n' vs 'l', sim=0.80).
			// The generic domain word "light" must never fuzzily match the specific qualifier "night".
			candLower := strings.ToLower(cand.text)
			if (candLower == "night" || candLower == "nite") && (span == "light" || span == "lights") {
				continue
			}
			// Guard: "light on" vs "light one" differ by only 1 letter ('on' vs 'one', sim=0.89).
			// The global command "light on" must never fuzzily match the specific alias "light one".
			if candLower == "light one" && (span == "light on" || span == "lights on" || span == "light off" || span == "lights off") {
				continue
			}

			// Threshold: 0.80 prevents 4-letter collisions like "hall" matching "halo" (which is 0.75)
			if score >= 0.80 {
				existing, exists := matchedDevices[dev.Device]
				spanLen := endTok - startTok
				existingSpanLen := existing.endTok - existing.startTok
				if !exists || score > existing.score || (score == existing.score && spanLen > existingSpanLen) {
					matchedDevices[dev.Device] = candidateMatch{
						device:    dev,
						score:     score,
						matchedOn: fmt.Sprintf("%s:%s (matched '%s' with sim=%.2f)", cand.label, cand.text, span, score),
						startTok:  startTok,
						endTok:    endTok,
					}
				}
			}
		}
	}

	// 3. If one or more specific devices matched, resolve them with per-device clause scoping!
	if len(matchedDevices) > 0 {
		type matchedItem struct {
			device    DeviceInfo
			score     float64
			matchedOn string
			startTok  int
			endTok    int
		}

		var matchedList []matchedItem
		for _, m := range matchedDevices {
			matchedList = append(matchedList, matchedItem{
				device:    m.device,
				score:     m.score,
				matchedOn: m.matchedOn,
				startTok:  m.startTok,
				endTok:    m.endTok,
			})
		}
		sort.Slice(matchedList, func(i, j int) bool {
			return matchedList[i].startTok < matchedList[j].startTok
		})

		deviceActions := make(map[int]string)
		deviceConfs := make(map[int]float64)

		// 1. Prefix window before the first device (e.g. "turn off ember")
		if matchedList[0].startTok > 0 {
			prefixSlice := tokens[0:matchedList[0].startTok]
			act, conf, _, _ := DetectAction(strings.Join(prefixSlice, " "))
			if act != "" {
				deviceActions[matchedList[0].device.Device] = act
				deviceConfs[matchedList[0].device.Device] = conf
			}
		}

		// 2. Windows between consecutive devices (e.g. "turn off ember turn on halo", "ember off halo on")
		for i := 0; i < len(matchedList)-1; i++ {
			currEnd := matchedList[i].endTok
			nextStart := matchedList[i+1].startTok
			if currEnd <= nextStart {
				midSlice := tokens[currEnd:nextStart]
				if len(midSlice) > 0 {
					// Check if curr device needs a postfix action (e.g. "ember off halo on")
					if _, hasAct := deviceActions[matchedList[i].device.Device]; !hasAct {
						firstTok := midSlice[0]
						if firstTok == "off" || firstTok == "on" || firstTok == "out" || firstTok == "toggle" {
							pAct, pConf, _, _ := DetectAction(firstTok)
							if pAct != "" {
								deviceActions[matchedList[i].device.Device] = pAct
								deviceConfs[matchedList[i].device.Device] = pConf
								midSlice = midSlice[1:]
							}
						}
					}

					// Remaining tokens in midSlice serve as prefix for the next device (e.g. "turn on halo")
					if len(midSlice) > 0 {
						prefAct, prefConf, _, _ := DetectAction(strings.Join(midSlice, " "))
						if prefAct != "" {
							deviceActions[matchedList[i+1].device.Device] = prefAct
							deviceConfs[matchedList[i+1].device.Device] = prefConf
						}
					}
				}
			}
		}

		// 3. Postfix window after the last device (e.g. "ember and halo off")
		lastItem := matchedList[len(matchedList)-1]
		if lastItem.endTok < len(tokens) {
			tailSlice := tokens[lastItem.endTok:]
			tailAct, tailConf, _, _ := DetectAction(strings.Join(tailSlice, " "))
			if tailAct != "" {
				if _, hasAct := deviceActions[lastItem.device.Device]; !hasAct {
					deviceActions[lastItem.device.Device] = tailAct
					deviceConfs[lastItem.device.Device] = tailConf
				}
			}
		}

		// 4. Forward propagation (e.g. "turn on ember and halo" -> halo inherits ON from ember)
		var lastAct string
		var lastConf float64
		for _, item := range matchedList {
			if act, ok := deviceActions[item.device.Device]; ok && act != "" {
				lastAct = act
				lastConf = deviceConfs[item.device.Device]
			} else if lastAct != "" {
				deviceActions[item.device.Device] = lastAct
				deviceConfs[item.device.Device] = lastConf
			}
		}

		// 5. Backward propagation (e.g. "ember and halo off" -> ember inherits OFF backwards from halo)
		var nextAct string
		var nextConf float64
		for i := len(matchedList) - 1; i >= 0; i-- {
			item := matchedList[i]
			if act, ok := deviceActions[item.device.Device]; ok && act != "" {
				nextAct = act
				nextConf = deviceConfs[item.device.Device]
			} else if nextAct != "" {
				deviceActions[item.device.Device] = nextAct
				deviceConfs[item.device.Device] = nextConf
			}
		}

		// 6. Sentence action fallback if any device remains unassigned
		if action != "" {
			for _, item := range matchedList {
				if act, ok := deviceActions[item.device.Device]; !ok || act == "" {
					deviceActions[item.device.Device] = action
					deviceConfs[item.device.Device] = actionConf
				}
			}
		}

		// If no action was resolved anywhere in the command
		if len(deviceActions) == 0 {
			var matchedIDs []int
			var targetNames []string
			var matchedOnList []string
			for _, item := range matchedList {
				matchedIDs = append(matchedIDs, item.device.Device)
				targetNames = append(targetNames, item.device.Name)
				matchedOnList = append(matchedOnList, item.matchedOn)
			}
			return ParsedIntent{
				Intent:     IntentUnknown,
				Action:     "",
				DeviceIDs:  matchedIDs,
				TargetName: strings.Join(targetNames, ", "),
				Confidence: 0.20,
				RawText:    rawTranscript,
				MatchedOn:  strings.Join(matchedOnList, "; "),
			}
		}

		distinctActions := make(map[string]bool)
		for _, item := range matchedList {
			if act, ok := deviceActions[item.device.Device]; ok && act != "" {
				distinctActions[act] = true
			}
		}

		var matchedIDs []int
		var targetNames []string
		var matchedOnList []string
		lowestScore := 1.0

		for _, item := range matchedList {
			devID := item.device.Device
			matchedIDs = append(matchedIDs, devID)
			act := deviceActions[devID]

			if len(distinctActions) > 1 {
				targetNames = append(targetNames, fmt.Sprintf("%s (%s)", item.device.Name, act))
				matchedOnList = append(matchedOnList, fmt.Sprintf("%s -> %s", item.matchedOn, act))
			} else {
				targetNames = append(targetNames, item.device.Name)
				matchedOnList = append(matchedOnList, item.matchedOn)
			}

			if item.score < lowestScore {
				lowestScore = item.score
			}
			if conf, ok := deviceConfs[devID]; ok && conf < lowestScore {
				lowestScore = conf
			}
		}

		joinedTargets := strings.Join(targetNames, ", ")
		joinedMatchedOn := strings.Join(matchedOnList, "; ")

		// If opposing or distinct actions exist across devices (e.g. Ember OFF, Halo ON)
		if len(distinctActions) > 1 {
			return ParsedIntent{
				Intent:        IntentMulti,
				Action:        "MULTI",
				DeviceIDs:     matchedIDs,
				DeviceActions: deviceActions,
				TargetName:    joinedTargets,
				Confidence:    lowestScore,
				RawText:       rawTranscript,
				MatchedOn:     joinedMatchedOn,
			}
		}

		// Uniform action across all matched devices
		singleAction := ""
		for act := range distinctActions {
			singleAction = act
			break
		}

		var resolvedIntent IntentType
		switch singleAction {
		case "ON":
			resolvedIntent = IntentTurnOn
		case "OFF":
			resolvedIntent = IntentTurnOff
		case "TOGGLE":
			resolvedIntent = IntentToggle
		case "QUERY":
			resolvedIntent = IntentQueryStatus
		default:
			resolvedIntent = IntentUnknown
		}

		return ParsedIntent{
			Intent:        resolvedIntent,
			Action:        singleAction,
			DeviceIDs:     matchedIDs,
			DeviceActions: deviceActions,
			TargetName:    joinedTargets,
			Confidence:    lowestScore,
			RawText:       rawTranscript,
			MatchedOn:     joinedMatchedOn,
		}
	}

	// 4. Global fallback: ONLY if no specific device was matched
	isGlobal := false

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
