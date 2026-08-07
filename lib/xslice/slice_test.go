package xslice

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyBy(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		key  func(string) rune
		want map[rune]string
	}{
		{
			name: "empty slice yields an empty map",
			in:   []string{},
			key:  func(s string) rune { return rune(s[0]) },
			want: map[rune]string{},
		},
		{
			name: "duplicate keys let the later element win",
			in:   []string{"apple", "avocado", "banana"},
			key:  func(s string) rune { return rune(s[0]) },
			want: map[rune]string{'a': "avocado", 'b': "banana"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, KeyBy(tt.in, tt.key))
		})
	}
}

func TestGroupBy(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		key  func(string) rune
		want map[rune][]string
	}{
		{
			name: "empty slice yields an empty map",
			in:   []string{},
			key:  func(s string) rune { return rune(s[0]) },
			want: map[rune][]string{},
		},
		{
			name: "multiple items with the same key land in one bucket, in order",
			in:   []string{"apple", "avocado", "banana"},
			key:  func(s string) rune { return rune(s[0]) },
			want: map[rune][]string{'a': {"apple", "avocado"}, 'b': {"banana"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GroupBy(tt.in, tt.key))
		})
	}
}
