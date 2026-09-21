package xstr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFirstLine pins what the first line of a string is for each shape of input, an empty
// string included.
func TestFirstLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "no newline returns the whole string",
			in:   "one line",
			want: "one line",
		},
		{
			name: "trailing newline is dropped",
			in:   "first\n",
			want: "first",
		},
		{
			name: "only the first of several lines",
			in:   "first\nsecond\nthird",
			want: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FirstLine(tt.in))
		})
	}
}

// TestSlug pins how a string is reduced to a slug, including one that holds nothing a slug
// could keep.
func TestSlug(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{
			name:   "only punctuation yields nothing",
			in:     "!!!___???",
			maxLen: 24,
			want:   "",
		},
		{
			name:   "empty yields nothing",
			in:     "",
			maxLen: 24,
			want:   "",
		},
		{
			name:   "spaces and case collapse to dashed lowercase",
			in:     "Fix the Login Bug",
			maxLen: 24,
			want:   "fix-the-login-bug",
		},
		{
			name:   "runs of punctuation collapse to one dash",
			in:     "a -- b__c",
			maxLen: 24,
			want:   "a-b-c",
		},
		{
			name:   "leading and trailing separators are trimmed",
			in:     "  hello  ",
			maxLen: 24,
			want:   "hello",
		},
		{
			name:   "truncates at the length cap",
			in:     "hello world",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "digits are kept",
			in:     "v2 release",
			maxLen: 24,
			want:   "v2-release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Slug(tt.in, tt.maxLen))
		})
	}
}

// TestHead covers the cut itself, which has to leave text a script can decode rather than half of
// whatever character the limit landed inside.
func TestHead(t *testing.T) {
	tests := []struct {
		name          string
		s             string
		limit         int
		want          string
		wantTruncated bool
	}{
		{
			name:  "a string inside the limit is untouched",
			s:     "fix auth",
			limit: 8,
			want:  "fix auth",
		},
		{
			name:          "a longer string is cut to the limit",
			s:             "fix auth now",
			limit:         8,
			want:          "fix auth",
			wantTruncated: true,
		},
		{
			name:          "a cut inside a character pulls back to the one before it",
			s:             "日本語",
			limit:         7,
			want:          "日本",
			wantTruncated: true,
		},
		{
			name:          "a limit inside the first character keeps nothing",
			s:             "日本語",
			limit:         2,
			want:          "",
			wantTruncated: true,
		},
		{
			name:  "an empty string is not truncated",
			s:     "",
			limit: 8,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := Head(tt.s, tt.limit)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}

// TestTail covers the cut from the other end, which has to keep the end of the text whole and
// drop whatever character the limit landed inside.
func TestTail(t *testing.T) {
	tests := []struct {
		name          string
		s             string
		limit         int
		want          string
		wantTruncated bool
	}{
		{
			name:  "a string inside the limit is untouched",
			s:     "fix auth",
			limit: 8,
			want:  "fix auth",
		},
		{
			name:          "a longer string keeps its end",
			s:             "fix auth now",
			limit:         8,
			want:          "auth now",
			wantTruncated: true,
		},
		{
			name:          "a cut inside a character moves on to the one after it",
			s:             "日本語",
			limit:         7,
			want:          "本語",
			wantTruncated: true,
		},
		{
			name:          "a limit inside the last character keeps nothing",
			s:             "日本語",
			limit:         2,
			want:          "",
			wantTruncated: true,
		},
		{
			name:  "an empty string is not truncated",
			s:     "",
			limit: 8,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := Tail(tt.s, tt.limit)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}

// TestJoinSome pins that a list past the limit names the first few and counts the rest.
func TestJoinSome(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		limit int
		want  string
	}{
		{
			name:  "nothing",
			limit: 2,
			want:  "",
		},
		{
			name:  "within the limit",
			items: []string{"a", "b"},
			limit: 2,
			want:  "a, b",
		},
		{
			name:  "past the limit",
			items: []string{"a", "b", "c", "d"},
			limit: 2,
			want:  "a, b, and 2 more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, JoinSome(tt.items, tt.limit))
		})
	}
}
