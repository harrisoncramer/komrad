package xuuid

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestParseOrNil covers text that is not a uuid reading as uuid.Nil.
func TestParseOrNil(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name string
		text string
		want uuid.UUID
	}{
		{
			name: "nothing at all",
			want: uuid.Nil,
		},
		{
			name: "text that is not a uuid",
			text: "%3",
			want: uuid.Nil,
		},
		{
			name: "a uuid",
			text: id.String(),
			want: id,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseOrNil(tt.text))
		})
	}
}
