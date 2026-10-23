package review

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestNewInputField covers which lines a field opened on a body shows.
func TestNewInputField(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}

	tests := []struct {
		name      string
		body      string
		width     int
		wantShown string
		wantGone  string
	}{
		{
			name:      "a body taller than the field opens scrolled to its end in a narrow field",
			body:      strings.Join(lines, "\n"),
			width:     30,
			wantShown: "line 19",
			wantGone:  "line 00",
		},
		{
			name:      "a body taller than the field opens scrolled to its end in a wide field",
			body:      strings.Join(lines, "\n"),
			width:     80,
			wantShown: "line 19",
			wantGone:  "line 00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := newInputField(newInputFieldParams{
				Title: "Reply",
				Body:  tt.body,
				Width: tt.width,
			})

			view := ansi.Strip(input.form.View())
			assert.Contains(t, view, tt.wantShown)
			assert.NotContains(t, view, tt.wantGone)
		})
	}
}
