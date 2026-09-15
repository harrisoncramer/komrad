package engine_test

import (
	"strings"
	"testing"

	"weave/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsRemoteIsWhetherADestinationWasGiven covers the zero value meaning a local engine.
func TestIsRemoteIsWhetherADestinationWasGiven(t *testing.T) {
	t.Parallel()

	assert.False(t, engine.Remote{}.IsRemote())
	assert.False(t, engine.Remote{Destination: "  "}.IsRemote())
	assert.True(t, engine.Remote{Destination: "ubuntu@host"}.IsRemote())
}

// TestAttachArgvIsEmptyForALocalEngine covers the caller being able to ask without checking first.
func TestAttachArgvIsEmptyForALocalEngine(t *testing.T) {
	t.Parallel()

	assert.Nil(t, engine.Remote{Socket: "weave", Session: "weave"}.AttachArgv("%9"))
}

// TestAttachArgvIsEmptyWithoutAPane covers a stage asked to show nothing.
func TestAttachArgvIsEmptyWithoutAPane(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	assert.Nil(t, remote.AttachArgv(""))
	assert.Nil(t, remote.AttachArgv("   "))
}

// TestAttachArgvRunsSshWithOneScript covers the shape the stage pane is respawned with.
func TestAttachArgvRunsSshWithOneScript(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	argv := remote.AttachArgv("%9")
	require.Len(t, argv, 4)
	assert.Equal(t, []string{"ssh", "-t", "ubuntu@host"}, argv[:3])
}

// TestAttachArgvPointsAtAViewSessionOfItsOwn covers one session per staged agent, which is what
// keeps two stage panes from resizing one another.
func TestAttachArgvPointsAtAViewSessionOfItsOwn(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	script := remote.AttachArgv("%9")[3]
	assert.Contains(t, script, "new-session -d -s 'view-9' -t 'weave'")
	assert.Contains(t, script, "exec tmux -L 'weave' attach -t 'view-9'")
	assert.NotEqual(t, engine.ViewSession("%9"), engine.ViewSession("%10"))
}

// TestAttachScriptSilencesTheRemoteStatusBar covers the nested tmux drawing its own chrome
// inside the stage pane when it is left on.
func TestAttachScriptSilencesTheRemoteStatusBar(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	assert.Contains(t, remote.AttachArgv("%9")[3], "set-option -t 'view-9' status off")
}

// TestAttachScriptResolvesTheWindowOnTheEngine covers the panel knowing an agent by its pane
// while only the engine can say which window holds it.
func TestAttachScriptResolvesTheWindowOnTheEngine(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	script := remote.AttachArgv("%9")[3]
	assert.Contains(t, script, "display-message -p -t '%9' '#{window_id}'")
	assert.Contains(t, script, "select-window -t 'view-9':$w")
}

// TestAttachScriptToleratesAViewSessionThatAlreadyExists covers the stage re-running this every
// time the cursor comes back to the same agent.
func TestAttachScriptToleratesAViewSessionThatAlreadyExists(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "ubuntu@host", Socket: "weave", Session: "weave"}

	assert.Contains(t, remote.AttachArgv("%9")[3], "|| true")
}

// TestAttachScriptQuotesWhatItInterpolates covers a socket or session name the far side's shell
// would otherwise split or expand.
func TestAttachScriptQuotesWhatItInterpolates(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{
		Destination: "ubuntu@host",
		Socket:      "we ave",
		Session:     "it's",
	}

	script := remote.AttachArgv("%9")[3]
	assert.Contains(t, script, `tmux -L 'we ave'`)
	assert.Contains(t, script, `-t 'it'\''s'`)
	assert.NotContains(t, strings.ReplaceAll(script, `'\''`, ""), "it's")
}

// TestStringNamesTheEngine covers what a log line or an error says about where agents are.
func TestStringNamesTheEngine(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "local", engine.Remote{}.String())
	assert.Equal(t,
		"ubuntu@host (tmux -L weave)",
		engine.Remote{Destination: "ubuntu@host", Socket: "weave"}.String())
}
