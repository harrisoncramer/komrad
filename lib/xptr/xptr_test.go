package xptr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNonEmpty covers the one rule: an empty string is no pointer at all.
func TestNonEmpty(t *testing.T) {
	t.Parallel()

	assert.Nil(t, NonEmpty(""))
	assert.Equal(t, "main", *NonEmpty("main"))
}

// TestEqual covers the four ways two pointers can compare.
func TestEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    *string
		b    *string
		want bool
	}{
		{
			name: "both nil are equal",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "nil and a value are not equal",
			a:    nil,
			b:    Ptr("main"),
			want: false,
		},
		{
			name: "two pointers at the same value are equal",
			a:    Ptr("main"),
			b:    Ptr("main"),
			want: true,
		},
		{
			name: "two pointers at different values are not equal",
			a:    Ptr("main"),
			b:    Ptr("topic"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Equal(tt.a, tt.b))
		})
	}
}
