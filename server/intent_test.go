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
		// "prison" or "film" misheard for "prism"
		{"turn on prison", IntentTurnOn, DeviceDeskLight},
		{"Turn on the film.", IntentTurnOn, DeviceDeskLight},
		{"turn on first light", IntentTurnOn, DeviceDeskLight},
		// "helo" or "hello" misheard for "halo"
		{"switch off helo", IntentTurnOff, DeviceReadingLight},
		{"switch on hello", IntentTurnOn, DeviceReadingLight},
		{"turn off second light", IntentTurnOff, DeviceReadingLight},
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
		{"time for sleep", IntentAllOff},
		{"good night", IntentAllOff},
		{"bedtime", IntentAllOff},
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

func TestParseIntent_DevicePriorityOverGlobal(t *testing.T) {
	devices := mockDevices()

	// 1. "Turn on all nigght lights" contains "all", but specifically targets Night Light (Device 4)
	res := ParseIntent("turn on all nigght lights", devices)
	if res.Intent != IntentTurnOn {
		t.Errorf("expected IntentTurnOn, got %s", res.Intent)
	}
	if len(res.DeviceIDs) != 1 || res.DeviceIDs[0] != 4 {
		t.Errorf("expected Night Light (Device 4), got %v (Target: %s)", res.DeviceIDs, res.TargetName)
	}

	// 2. "some illumination on the desk" has illumination as action and desk as device
	res2 := ParseIntent("some illumination on the desk", devices)
	if res2.Intent != IntentTurnOn {
		t.Errorf("expected IntentTurnOn for illumination, got %s", res2.Intent)
	}
	if len(res2.DeviceIDs) != 1 || res2.DeviceIDs[0] != 1 {
		t.Errorf("expected Desk Light (Device 1), got %v (Target: %s)", res2.DeviceIDs, res2.TargetName)
	}

	// 3. Prepositional bare "on" with determiner ("on the desk") must NOT trigger an action
	res3 := ParseIntent("put the book on the desk", devices)
	if res3.Intent != IntentUnknown {
		t.Errorf("expected IntentUnknown for prepositional 'on the desk', got %s", res3.Intent)
	}
}

func TestParseIntent_MultipleDevices(t *testing.T) {
	devices := mockDevices()

	// 1. "Turn on ember and halo" -> matches Device 3 (Ember) then Device 2 (Halo) in utterance order
	res := ParseIntent("Turn on ember and halo", devices)
	if res.Intent != IntentTurnOn {
		t.Errorf("expected IntentTurnOn, got %s", res.Intent)
	}
	if len(res.DeviceIDs) != 2 {
		t.Fatalf("expected 2 devices, got %d (%v)", len(res.DeviceIDs), res.DeviceIDs)
	}
	if res.DeviceIDs[0] != DeviceAccentLight || res.DeviceIDs[1] != DeviceReadingLight {
		t.Errorf("expected devices [3, 2], got %v", res.DeviceIDs)
	}
	if res.DeviceActions[DeviceAccentLight] != "ON" || res.DeviceActions[DeviceReadingLight] != "ON" {
		t.Errorf("expected DeviceActions {3: ON, 2: ON}, got %v", res.DeviceActions)
	}

	// 2. "turn off prism, halo, and ember" -> matches Devices [1, 2, 3] with uniform OFF
	res2 := ParseIntent("turn off prism, halo, and ember", devices)
	if res2.Intent != IntentTurnOff {
		t.Errorf("expected IntentTurnOff, got %s", res2.Intent)
	}
	if len(res2.DeviceIDs) != 3 {
		t.Fatalf("expected 3 devices, got %d (%v)", len(res2.DeviceIDs), res2.DeviceIDs)
	}
	if res2.DeviceActions[DeviceDeskLight] != "OFF" || res2.DeviceActions[DeviceReadingLight] != "OFF" || res2.DeviceActions[DeviceAccentLight] != "OFF" {
		t.Errorf("expected all devices OFF, got %v", res2.DeviceActions)
	}

	// 3. User's exact compound opposing command: "Turn off emberr turn on halo."
	res3 := ParseIntent("Turn off emberr turn on halo.", devices)
	if res3.Intent != IntentMulti {
		t.Errorf("expected IntentMulti, got %s", res3.Intent)
	}
	if res3.Action != "MULTI" {
		t.Errorf("expected Action MULTI, got %s", res3.Action)
	}
	if len(res3.DeviceIDs) != 2 {
		t.Fatalf("expected 2 devices, got %d (%v)", len(res3.DeviceIDs), res3.DeviceIDs)
	}
	if res3.DeviceActions[DeviceAccentLight] != "OFF" {
		t.Errorf("expected Ember (Dev 3) to be OFF, got %s", res3.DeviceActions[DeviceAccentLight])
	}
	if res3.DeviceActions[DeviceReadingLight] != "ON" {
		t.Errorf("expected Halo (Dev 2) to be ON, got %s", res3.DeviceActions[DeviceReadingLight])
	}
	if res3.TargetName != "Accent Light (OFF), Reading Light (ON)" {
		t.Errorf("expected formatted per-device targets, got %s", res3.TargetName)
	}

	// 4. Postfix compound commands: "ember off halo on"
	res4 := ParseIntent("ember off halo on", devices)
	if res4.Intent != IntentMulti {
		t.Errorf("expected IntentMulti for postfix command, got %s", res4.Intent)
	}
	if res4.DeviceActions[DeviceAccentLight] != "OFF" || res4.DeviceActions[DeviceReadingLight] != "ON" {
		t.Errorf("expected Ember OFF, Halo ON, got %v", res4.DeviceActions)
	}

	// 5. Mixed postfix + conjunction prefix: "ember off and turn on halo"
	res5 := ParseIntent("ember off and turn on halo", devices)
	if res5.Intent != IntentMulti {
		t.Errorf("expected IntentMulti, got %s", res5.Intent)
	}
	if res5.DeviceActions[DeviceAccentLight] != "OFF" || res5.DeviceActions[DeviceReadingLight] != "ON" {
		t.Errorf("expected Ember OFF, Halo ON, got %v", res5.DeviceActions)
	}

	// 6. Trailing shared action: "ember and halo off" -> backward propagation
	res6 := ParseIntent("ember and halo off", devices)
	if res6.Intent != IntentTurnOff {
		t.Errorf("expected IntentTurnOff, got %s", res6.Intent)
	}
	if res6.DeviceActions[DeviceAccentLight] != "OFF" || res6.DeviceActions[DeviceReadingLight] != "OFF" {
		t.Errorf("expected both OFF, got %v", res6.DeviceActions)
	}

	// 7. Three devices with opposing actions: "turn on prism turn off halo and turn on ember"
	res7 := ParseIntent("turn on prism turn off halo and turn on ember", devices)
	if res7.Intent != IntentMulti {
		t.Errorf("expected IntentMulti, got %s", res7.Intent)
	}
	if res7.DeviceActions[DeviceDeskLight] != "ON" || res7.DeviceActions[DeviceReadingLight] != "OFF" || res7.DeviceActions[DeviceAccentLight] != "ON" {
		t.Errorf("expected Desk ON, Halo OFF, Ember ON, got %v", res7.DeviceActions)
	}

	// 8. User's exact phonetic ASR slip: "Turn on halo term of nova.."
	res8 := ParseIntent("Turn on halo term of nova..", devices)
	if res8.Intent != IntentMulti {
		t.Errorf("expected IntentMulti, got %s", res8.Intent)
	}
	if res8.Action != "MULTI" {
		t.Errorf("expected Action MULTI, got %s", res8.Action)
	}
	if len(res8.DeviceIDs) != 2 {
		t.Fatalf("expected 2 devices, got %d (%v)", len(res8.DeviceIDs), res8.DeviceIDs)
	}
	if res8.DeviceActions[DeviceReadingLight] != "ON" {
		t.Errorf("expected Halo (Dev 2) to be ON, got %s", res8.DeviceActions[DeviceReadingLight])
	}
	if res8.DeviceActions[DeviceNightLight] != "OFF" {
		t.Errorf("expected Nova (Dev 4) to be OFF, got %s", res8.DeviceActions[DeviceNightLight])
	}
	if res8.TargetName != "Reading Light (ON), Night Light (OFF)" {
		t.Errorf("expected target 'Reading Light (ON), Night Light (OFF)', got %s", res8.TargetName)
	}
}
