package ui

import (
	"testing"

	"komrad/config"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
)

// TestFormOrderLeavesOutANote covers the cursor stepping past a note the way huh does.
func TestFormOrderLeavesOutANote(t *testing.T) {
	prompt, title := "", ""

	form, order := formOf(config.FixedFormKeys(),
		huh.NewNote().Description("nothing to answer here"),
		textField("prompt").Value(&prompt),
		huh.NewInput().Key("title").Value(&title),
	)
	form.Init()

	assert.Equal(t, []string{"prompt", "title"}, order, "a note takes no cursor stop, so it takes no place in the order")
	assert.Equal(t, "prompt", form.GetFocusedField().GetKey(), "the form opens on the first field the cursor can reach")

	form.NextField()
	assert.Equal(t, "title", form.GetFocusedField().GetKey(), "one step is one field the cursor can reach")
}
