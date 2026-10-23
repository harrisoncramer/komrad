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

	body := strings.Join(lines, "\n")
	tests := []struct {
		name  string
		width int
	}{
		{
			name:  "a body taller than the field opens scrolled to its end in a narrow field",
			width: 30,
		},
		{
			name:  "a body taller than the field opens scrolled to its end in a wide field",
			width: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := newInputField(newInputFieldParams{
				Title: "Reply",
				Body:  body,
				Width: tt.width,
			})

			view := ansi.Strip(input.form.View())
			assert.Contains(t, view, "line 19")
			assert.NotContains(t, view, "line 00")
		})
	}
}
