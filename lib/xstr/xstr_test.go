package xstr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
