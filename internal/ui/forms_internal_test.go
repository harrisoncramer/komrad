package ui

import (
	"testing"

	"weave/config"
	"weave/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireModel(t *testing.T, updated tea.Model) model {
	t.Helper()

	m, ok := updated.(model)
	require.True(t, ok)

	return m
}

func (m model) openRename(agent panel.Agent) model {
	openRename(&m, agent)

	return m
}

// TestFormOrderLeavesOutANote checks the cursor steps the same way huh does, huh passing a note
// by rather than focusing it.
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
