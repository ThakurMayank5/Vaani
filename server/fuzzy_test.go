package main

import (
	"testing"
)

func TestNormalizeText(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Turn on, Desk Light!", "turn on desk light"},
		{"   SWITCH   OFF   ", "switch off"},
		{"Hello... world???", "hello world"},
		{"", ""},
	}

	for _, c := range cases {
		got := NormalizeText(c.input)
		if got != c.expected {
			t.Errorf("NormalizeText(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestLevenshteinDistance(t *testing.T) {
	cases := []struct {
		a, b     string
		expected int
	}{
		{"prism", "prism", 0},
		{"prizm", "prism", 1},
		{"prison", "prism", 2},
		{"helo", "halo", 1},
		{"hello", "halo", 2},
		{"", "test", 4},
		{"test", "", 4},
		{"sitting", "kitten", 3},
	}

	for _, c := range cases {
		got := LevenshteinDistance(c.a, c.b)
		if got != c.expected {
			t.Errorf("LevenshteinDistance(%q, %q) = %d; want %d", c.a, c.b, got, c.expected)
		}
	}
}

func TestBestFuzzyMatch(t *testing.T) {
	cases := []struct {
		text     string
		target   string
		minScore float64
	}{
		{"turn on prism", "prism", 1.0},
		{"switch on prizm", "prism", 0.80},
		{"switch on prison", "prism", 0.65},
		{"turn off reading light please", "reading light", 1.0},
		{"turn off read light", "reading light", 0.75},
		{"halo on", "halo", 1.0},
		{"helo on", "halo", 0.75},
	}

	for _, c := range cases {
		score, _ := BestFuzzyMatch(c.text, c.target)
		if score < c.minScore {
			t.Errorf("BestFuzzyMatch(%q, %q) score = %f; want >= %f", c.text, c.target, score, c.minScore)
		}
	}
}
