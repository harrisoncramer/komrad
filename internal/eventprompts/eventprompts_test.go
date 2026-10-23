package eventprompts

import (
	"strings"
	"testing"
	"unicode/utf8"

	"komrad/lib/protoenum"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/stretchr/testify/assert"
)

// TestEveryEventHasADefaultPromptThatFits pins that no event's default is missing, blank or longer than a workflow may hold.
func TestEveryEventHasADefaultPromptThatFits(t *testing.T) {
	for _, event := range protoenum.Values[eventv1.Event]() {
		t.Run(protoenum.Name(event), func(t *testing.T) {
			prompt := DefaultPrompt(event)
			assert.NotEmpty(t, strings.TrimSpace(prompt))
			assert.LessOrEqual(t, utf8.RuneCountInString(prompt), MaxPrompt)
		})
	}
}
