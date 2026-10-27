package main

import (
	"strings"
	"unicode"
)

// NormalizeText strips punctuation, converts characters to lower case,
// and normalizes multiple whitespace characters into single spaces.
func NormalizeText(s string) string {
	var b strings.Builder
	lastWasSpace := false

	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastWasSpace = false
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if !lastWasSpace && b.Len() > 0 {
				b.WriteRune(' ')
				lastWasSpace = true
			}
		}
	}

	return strings.TrimSpace(b.String())
}

// LevenshteinDistance calculates the minimum edit distance (insertions, deletions, substitutions)
// between two strings using runes for multi-byte Unicode safety.
func LevenshteinDistance(s1, s2 string) int {
	r1 := []rune(s1)
	r2 := []rune(s2)
	n := len(r1)
	m := len(r2)

	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	prev := make([]int, m+1)
	curr := make([]int, m+1)

	for j := 0; j <= m; j++ {
		prev[j] = j
	}

	for i := 1; i <= n; i++ {
		curr[0] = i
		for j := 1; j <= m; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}

			deletion := prev[j] + 1
			insertion := curr[j-1] + 1
			substitution := prev[j-1] + cost

			minVal := deletion
			if insertion < minVal {
				minVal = insertion
			}
			if substitution < minVal {
				minVal = substitution
			}

			curr[j] = minVal
		}
		copy(prev, curr)
	}

	return prev[m]
}

// Similarity returns a normalized float64 score in [0.0, 1.0].
// 1.0 represents an exact match, while 0.0 represents complete dissimilarity.
func Similarity(s1, s2 string) float64 {
	r1 := []rune(s1)
	r2 := []rune(s2)
	maxLen := len(r1)
	if len(r2) > maxLen {
		maxLen = len(r2)
	}

	if maxLen == 0 {
		return 1.0
	}

	dist := LevenshteinDistance(s1, s2)
	sim := 1.0 - float64(dist)/float64(maxLen)
	if sim < 0.0 {
		return 0.0
	}
	return sim
}

// Tokenize splits normalized text by whitespace.
func Tokenize(text string) []string {
	return strings.Fields(text)
}

// ExtractNGrams returns all consecutive sliding n-grams of size n from tokens.
func ExtractNGrams(tokens []string, n int) []string {
	if len(tokens) < n || n <= 0 {
		return nil
	}
	result := make([]string, 0, len(tokens)-n+1)
	for i := 0; i <= len(tokens)-n; i++ {
		result = append(result, strings.Join(tokens[i:i+n], " "))
	}
	return result
}

// BestFuzzyMatchSpan scans 1-gram, 2-gram, and 3-gram substrings of tokens
// against a target string and returns the highest similarity score, matched span,
// and token boundaries [startTok, endTok).
func BestFuzzyMatchSpan(tokens []string, target string) (float64, string, int, int) {
	normTarget := NormalizeText(target)
	if normTarget == "" || len(tokens) == 0 {
		return 0.0, "", -1, -1
	}

	targetWords := Tokenize(normTarget)
	targetWordCount := len(targetWords)
	if targetWordCount == 0 {
		return 0.0, "", -1, -1
	}

	bestScore := 0.0
	bestSpan := ""
	bestStart := -1
	bestEnd := -1

	minN := targetWordCount - 1
	if minN < 1 {
		minN = 1
	}
	maxN := targetWordCount + 1
	if maxN > 3 {
		maxN = 3
	}

	for n := minN; n <= maxN; n++ {
		if n > len(tokens) {
			continue
		}
		for i := 0; i <= len(tokens)-n; i++ {
			gram := strings.Join(tokens[i:i+n], " ")
			score := Similarity(gram, normTarget)
			if score > bestScore || (score == bestScore && score > 0 && n > (bestEnd-bestStart)) {
				bestScore = score
				bestSpan = gram
				bestStart = i
				bestEnd = i + n
			}
		}
	}

	return bestScore, bestSpan, bestStart, bestEnd
}

// BestFuzzyMatch scans 1-gram, 2-gram, and 3-gram substrings of text
// against a target string and returns the highest similarity score and matched span.
func BestFuzzyMatch(text, target string) (float64, string) {
	tokens := Tokenize(NormalizeText(text))
	score, span, _, _ := BestFuzzyMatchSpan(tokens, target)
	return score, span
}
