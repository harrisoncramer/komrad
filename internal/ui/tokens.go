package ui

import (
	"fmt"
	"strings"

	usagestore "weave/internal/store/usage"
	agentv1 "weave/proto/gen/agent/v1"
)

// tokenUsage is what one agent's CLI reported spending over the panel's recent window, the
// counts kept apart because how a cache read weighs against a fresh input token is not
// something weave knows.
type tokenUsage struct {
	input      int64
	cacheRead  int64
	cacheWrite int64
	output     int64
}

func tokenUsageFrom(reported *agentv1.TokenUsage) *tokenUsage {
	if reported == nil {
		return nil
	}

	return &tokenUsage{
		input:      reported.GetInput(),
		cacheRead:  reported.GetCacheRead(),
		cacheWrite: reported.GetCacheWrite(),
		output:     reported.GetOutput(),
	}
}

func (u tokenUsage) total() int64 {
	return u.input + u.cacheRead + u.cacheWrite + u.output
}

// recentTokensMark is the one figure a row carries, every count added together, drawn only for
// an agent whose log said something in the window so that the busy rows are the ones that
// stand out.
func recentTokensMark(usage *tokenUsage) string {
	if usage == nil || usage.total() == 0 {
		return ""
	}

	return " " + dimStyle.Render(compactTokens(usage.total()))
}

// tokenFields breaks the row's figure into the counts it is made of, since a reading dominated
// by cache reads means something different from the same figure written fresh.
func tokenFields(usage *tokenUsage) []recordField {
	if usage == nil {
		return nil
	}

	window := fmt.Sprintf("tokens %s", shortWindow())

	return []recordField{
		{window, exactTokens(usage.total())},
		{"input", exactTokens(usage.input)},
		{"cache read", exactTokens(usage.cacheRead)},
		{"cache write", exactTokens(usage.cacheWrite)},
		{"output", exactTokens(usage.output)},
	}
}

func shortWindow() string {
	return fmt.Sprintf("%dh", int(usagestore.RecentWindow.Hours()))
}

// compactTokens is the figure a row has width for, rounded down so that the number never
// claims more than was reported.
func compactTokens(n int64) string {
	switch {
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprintf("%.1fM", float64(n/100_000)/10)
	}
}

// exactTokens is the whole number, grouped in threes, which is what the record under the list
// has the width for.
func exactTokens(n int64) string {
	digits := fmt.Sprintf("%d", n)

	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}

	return grouped.String()
}
