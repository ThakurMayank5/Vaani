package main

import (
	"testing"
)

func mockDevices() []DeviceInfo {
	return []DeviceInfo{
		{Device: DeviceDeskLight, Name: "Desk Light", CustomWord: "Prism", GPIO: 18, State: "OFF"},
		{Device: DeviceReadingLight, Name: "Reading Light", CustomWord: "Halo", GPIO: 19, State: "OFF"},
		{Device: DeviceAccentLight, Name: "Accent Light", CustomWord: "Ember", GPIO: 21, State: "OFF"},
		{Device: DeviceNightLight, Name: "Night Light", CustomWord: "Nova", GPIO: 22, State: "OFF"},
	}
}

func TestParseIntent_ExactAndPhrases(t *testing.T) {
	devices := mockDevices()

	cases := []struct {
		input          string
		expectedIntent IntentType
		expectedDevice int
	}{
		{"turn on desk light", IntentTurnOn, DeviceDeskLight},
		{"switch off reading light", IntentTurnOff, DeviceReadingLight},
		{"halo on", IntentTurnOn, DeviceReadingLight},
		{"prism off", IntentTurnOff, DeviceDeskLight},
		{"turn on ember", IntentTurnOn, DeviceAccentLight},
		{"nova off", IntentTurnOff, DeviceNightLight},
	}

	for _, c := range cases {
		result := ParseIntent(c.input, devices)
		if result.Intent != c.expectedIntent {
			t.Errorf("ParseIntent(%q).Intent = %s; want %s", c.input, result.Intent, c.expectedIntent)
		}
		if len(result.DeviceIDs) != 1 || result.DeviceIDs[0] != c.expectedDevice {
			t.Errorf("ParseIntent(%q).DeviceIDs = %v; want [%d]", c.input, result.DeviceIDs, c.expectedDevice)
		}
	}
}

func TestParseIntent_FuzzyASRMishearings(t *testing.T) {
	devices := mockDevices()

	cases := []struct {
		input          string
		expectedIntent IntentType
		expectedDevice int
	}{
		// "prison" misheard for "prism"
		{"turn on prison", IntentTurnOn, DeviceDeskLight},
		// "helo" misheard for "halo"
		{"switch off helo", IntentTurnOff, DeviceReadingLight},
		// "amber" misheard for "ember"
		{"amber on", IntentTurnOn, DeviceAccentLight},
		// "noah" misheard for "nova"
		{"turn off noah", IntentTurnOff, DeviceNightLight},
		// punctuation and noise in speech
		{"Turn off, desk light please!", IntentTurnOff, DeviceDeskLight},
	}

	for _, c := range cases {
		result := ParseIntent(c.input, devices)
		if result.Intent != c.expectedIntent {
			t.Errorf("ParseIntent(%q).Intent = %s; want %s (matched: %s)", c.input, result.Intent, c.expectedIntent, result.MatchedOn)
		}
		if len(result.DeviceIDs) != 1 || result.DeviceIDs[0] != c.expectedDevice {
			t.Errorf("ParseIntent(%q).DeviceIDs = %v; want [%d]", c.input, result.DeviceIDs, c.expectedDevice)
		}
	}
}

func TestParseIntent_GlobalCommands(t *testing.T) {
	devices := mockDevices()

	cases := []struct {
		input          string
		expectedIntent IntentType
	}{
		{"turn off all lights", IntentAllOff},
		{"everything off", IntentAllOff},
		{"lights off", IntentAllOff},
		{"light off", IntentAllOff},
		{"turn on all lights", IntentAllOn},
		{"everything on", IntentAllOn},
		{"light on", IntentAllOn},
	}

	for _, c := range cases {
		result := ParseIntent(c.input, devices)
		if result.Intent != c.expectedIntent {
			t.Errorf("ParseIntent(%q).Intent = %s; want %s", c.input, result.Intent, c.expectedIntent)
		}
		if len(result.DeviceIDs) != len(devices) {
			t.Errorf("ParseIntent(%q).DeviceIDs length = %d; want %d", c.input, len(result.DeviceIDs), len(devices))
		}
	}
}

func TestParseIntent_FalsePositiveRejection(t *testing.T) {
	devices := mockDevices()

	// Phrases with substrings like "hall" (contains "all") or "hello" (similar to "halo")
	// that shouldn't trigger accidental room-wide or light switches in a noisy hackathon
	cases := []string{
		"standing in the hall on Sunday",
		"call the team",
		"hello everyone how are you",
		"turn on the laptop light",
		"turn on the kitchen light",
		"what is the weather in Delhi",
		"play some music",
		"",
	}

	for _, input := range cases {
		result := ParseIntent(input, devices)
		if result.Intent != IntentUnknown {
			t.Errorf("ParseIntent(%q).Intent = %s, Target: %s, MatchedOn: %s; want UNKNOWN (avoid false positive)", input, result.Intent, result.TargetName, result.MatchedOn)
		}
	}
}
