package ui

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestCompactTokens pins the figure a row carries, which has four cells to say what could be
// anything from a handful of tokens to millions.
func TestCompactTokens(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{in: 0, want: "0"},
		{in: 279, want: "279"},
		{in: 999, want: "999"},
		{in: 1_000, want: "1k"},
		{in: 38_267, want: "38k"},
		{in: 999_999, want: "999k"},
		{in: 1_000_000, want: "1.0M"},
		{in: 1_412_338, want: "1.4M"},
		{in: 12_349_000, want: "12.3M"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, compactTokens(tt.in))
		})
	}
}

// TestExactTokens is the whole number the record under the list shows, where the compact
// figure on the row would round away the difference between two busy agents.
func TestExactTokens(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{in: 0, want: "0"},
		{in: 7, want: "7"},
		{in: 999, want: "999"},
		{in: 1_000, want: "1,000"},
		{in: 38_267, want: "38,267"},
		{in: 1_412_338, want: "1,412,338"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, exactTokens(tt.in))
		})
	}
}

// TestRecentTokensMark covers which rows carry a figure at all, since an agent whose log said
// nothing in the window has to read as nothing rather than as a zero.
func TestRecentTokensMark(t *testing.T) {
	assert.Empty(t, recentTokensMark(nil), "a harness that reported nothing says nothing")
	assert.Empty(t, recentTokensMark(&tokenUsage{}), "and neither does a window that came to zero")
	assert.Contains(t, recentTokensMark(&tokenUsage{input: 2, cacheRead: 38_265}), "38k")
}

// TestTokenFieldsAddUpToTheFigureOnTheRow is what keeps the row's single number honest: the
// counts under it are the ones it is made of, and they are the only ones.
func TestTokenFieldsAddUpToTheFigureOnTheRow(t *testing.T) {
	usage := &tokenUsage{input: 2, cacheRead: 38_267, cacheWrite: 25_559, output: 279}

	assert.Nil(t, tokenFields(nil))
	assert.Equal(t, []recordField{
		{"tokens 4h", "64,107"},
		{"input", "2"},
		{"cache read", "38,267"},
		{"cache write", "25,559"},
		{"output", "279"},
	}, tokenFields(usage))
	assert.Equal(t, "64k", compactTokens(usage.total()))
}

// TestTokenUsageFrom keeps the absent case apart from the empty one, since one means the CLI
// said nothing and the other means it said zero.
func TestTokenUsageFrom(t *testing.T) {
	assert.Nil(t, tokenUsageFrom(nil))
	assert.Equal(t, &tokenUsage{input: 2, cacheRead: 40, cacheWrite: 300, output: 7}, tokenUsageFrom(
		&agentv1.TokenUsage{Input: 2, CacheRead: 40, CacheWrite: 300, Output: 7},
	))
}
