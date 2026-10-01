package eventprompts

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// TestEveryEventHasADefaultPromptThatFits pins that no event's default is blank or longer than a workflow may hold.
func TestEveryEventHasADefaultPromptThatFits(t *testing.T) {
	for _, event := range All() {
		t.Run(event.Name, func(t *testing.T) {
			assert.NotEmpty(t, strings.TrimSpace(event.DefaultPrompt))
			assert.LessOrEqual(t, utf8.RuneCountInString(event.DefaultPrompt), MaxPrompt)
		})
	}
}
