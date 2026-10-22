package form_test

import (
	"testing"

	"komrad/config"
	"komrad/internal/ui/form"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
)

// TestOfLeavesOutANote covers the cursor stepping past a note the way huh does.
func TestOfLeavesOutANote(t *testing.T) {
	t.Parallel()

	prompt, title := "", ""

	built, order := form.Of(config.Fixed(),
		huh.NewNote().Description("nothing to answer here"),
		form.Text("prompt").Value(&prompt),
		huh.NewInput().Key("title").Value(&title),
	)
	built.Init()

	assert.Equal(t, []string{"prompt", "title"}, order, "a note takes no cursor stop, so it takes no place in the order")
	assert.Equal(t, "prompt", built.GetFocusedField().GetKey(), "the form opens on the first field the cursor can reach")

	built.NextField()
	assert.Equal(t, "title", built.GetFocusedField().GetKey(), "one step is one field the cursor can reach")
}
