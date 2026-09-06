package xslice

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeyBy pins the map a slice becomes when each element is keyed, a later duplicate
// replacing the one before it.
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

// TestGroupBy pins the map a slice becomes when elements sharing a key are gathered
// together.
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
			assert.Equal(t, tt.want, groupBy(tt.in, tt.key))
		})
	}
}

// TestUniqueBy pins that only the first element under each key survives, in the order they
// were given.
func TestUniqueBy(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		key  func(string) rune
		want []string
	}{
		{
			name: "empty slice yields an empty slice",
			in:   []string{},
			key:  func(s string) rune { return rune(s[0]) },
			want: []string{},
		},
		{
			name: "the first element with a key wins, in original order",
			in:   []string{"apple", "avocado", "banana", "blueberry"},
			key:  func(s string) rune { return rune(s[0]) },
			want: []string{"apple", "banana"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UniqueBy(tt.in, tt.key))
		})
	}
}
