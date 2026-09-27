package ui_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"komrad/internal/errui"
	"komrad/internal/ui"
	"komrad/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEngines is a machine with this engine and two others, recording where the terminal was sent.
type fakeEngines struct {
	mu       sync.Mutex
	statuses map[string]ui.EngineStatus
	build    ui.EngineBuild
	switched []string
}

func newFakeEngines() *fakeEngines {
	return &fakeEngines{
		statuses: map[string]ui.EngineStatus{
			"":        {Build: "v1 (d70bf5a)"},
			"remote":  {Build: "v1 (d70bf5a)"},
			"staging": {Build: "not answering", Label: "down", Refused: "the engine's server is not answering on 127.0.0.1:9"},
		},
		build: ui.EngineBuild{Commit: "d70bf5a"},
	}
}

func (f *fakeEngines) on(name string) ui.Engines {
	return ui.Engines{
		Name:  name,
		Build: f.build,
		ReadBuild: func(context.Context) (ui.EngineBuild, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()

			return f.build, true
		},
		List: func() ([]ui.EngineRow, error) {
			return []ui.EngineRow{{}, {Name: "remote"}, {Name: "staging"}}, nil
		},
		Probe: func(_ context.Context, row ui.EngineRow) ui.EngineStatus {
			return f.statuses[row.Name]
		},
		Switch: func(name string) error {
			f.mu.Lock()
			defer f.mu.Unlock()

			f.switched = append(f.switched, name)

			return nil
		},
	}
}

func (f *fakeEngines) readSwitched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string{}, f.switched...)
}

// TestEngineLine covers the engine drawn beside the logo.
func TestEngineLine(t *testing.T) {
	tests := []struct {
		name string
		// width is the sidebar's.
		width  int
		engine string
		lost   bool
		want   string
	}{
		{
			name:  "a connected engine says nothing about being connected",
			width: 60,
			want:  "engine  local  d70bf5a",
		},
		{
			name:  "a server that stopped answering is down",
			width: 60,
			lost:  true,
			want:  "engine  local  down  d70bf5a",
		},
		{
			name:  "a narrow pane gives up the commit first",
			width: 38,
			lost:  true,
			want:  "engine  local  down",
		},
		{
			name:   "then the status word, keeping the label and the name",
			width:  36,
			engine: "staging-east",
			lost:   true,
			want:   "engine  staging-east",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{sidebarWidth: tt.width, engines: newFakeEngines().on(tt.engine)})
			if tt.lost {
				harness.send(ui.NewWatchLost(errors.New("connection refused")))
			}

			logo := readLogoLine(harness.readFrame())
			assert.True(t, strings.HasSuffix(logo, tt.want), "%q ends %q", logo, tt.want)
			assert.True(t, strings.HasPrefix(logo, " komrad"))
		})
	}

	t.Run("the label's e is underlined like a view's key", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{engines: newFakeEngines().on("")})

		muted := paint.Panel.Style(paint.ToneMuted)
		assert.Contains(t, harness.readRawFrame(), muted.Underline(true).Render("e")+muted.Render("ngine"))
	})

	t.Run("down clears when the stream comes back", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{sidebarWidth: 60, engines: newFakeEngines().on("")})

		harness.send(ui.NewWatchLost(errors.New("connection refused")))
		harness.watch(buildCrowdedSnapshot(""))

		assert.True(t, strings.HasSuffix(readLogoLine(harness.readFrame()), "engine  local  d70bf5a"))
	})

	t.Run("a server back on other code is a mismatch, warned of once", func(t *testing.T) {
		engines := newFakeEngines()
		harness := openCrowded(t, panelHarnessParams{sidebarWidth: 60, engines: engines.on("remote")})

		engines.build = ui.EngineBuild{Commit: "a41c9e0", Stale: "rebuild komrad at a41c9e0"}
		harness.send(ui.NewWatchLost(errors.New("connection refused")))
		harness.watch(buildCrowdedSnapshot(""))

		frame := harness.readFrame()
		assert.True(t, strings.HasSuffix(readLogoLine(frame), "engine  remote  mismatch  a41c9e0"))
		assert.Contains(t, frame, "The engine is running different code")

		harness.send(ui.NewWatchLost(errors.New("connection refused")))
		harness.watch(buildCrowdedSnapshot(""))

		assert.NotContains(t, harness.readFrame(), "+1", "a mismatch that stayed is not warned of again")
	})
}

// TestEngineChooser covers o then e opening the chooser and what it does with a choice.
func TestEngineChooser(t *testing.T) {
	t.Run("a bare e does not open it", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{engines: newFakeEngines().on("")})

		harness.press("e")

		assert.NotContains(t, harness.readFrame(), "Open which komrad?")
	})

	t.Run("o then e lists every engine with the word each probe gave", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{engines: newFakeEngines().on("")})

		harness.press("o", "e")

		frame := harness.readFrame()
		assert.Contains(t, frame, "Open which komrad?")
		assert.Contains(t, frame, "local    (open)")
		assert.Contains(t, frame, "staging  down")
		assert.Contains(t, frame, "local  runs v1 (d70bf5a)")
	})

	t.Run("enter on an openable engine hands the terminal to it", func(t *testing.T) {
		engines := newFakeEngines()
		harness := openCrowded(t, panelHarnessParams{engines: engines.on("")})

		harness.press("o", "e", "down", "enter")

		assert.Equal(t, []string{"remote"}, engines.readSwitched())
		assert.NotContains(t, harness.readFrame(), "Open which komrad?")
	})

	t.Run("enter on a refused engine keeps the reason on screen", func(t *testing.T) {
		engines := newFakeEngines()
		harness := openCrowded(t, panelHarnessParams{engines: engines.on("")})

		harness.press("o", "e", "down", "down", "enter")

		assert.Empty(t, engines.readSwitched())
		assert.Contains(t, joinProse(splitLines(harness.readFrame())), "not answering on 127.0.0.1:9")
	})

	t.Run("going back to local names it", func(t *testing.T) {
		engines := newFakeEngines()
		harness := openCrowded(t, panelHarnessParams{engines: engines.on("remote")})

		harness.press("o", "e", "up", "enter")

		assert.Equal(t, []string{"local"}, engines.readSwitched())
	})

	t.Run("esc closes it", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{engines: newFakeEngines().on("")})

		harness.press("o", "e", "esc")

		assert.NotContains(t, harness.readFrame(), "Open which komrad?")
	})
}

// TestEngineChooserOnTheErrorScreen covers choosing another engine from a panel that could not start.
func TestEngineChooserOnTheErrorScreen(t *testing.T) {
	startupErr := errors.New("failed to carry 127.0.0.1:8787 through the tunnel: Operation timed out")

	t.Run("the engine is drawn over the error, and e is named", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{startupErr: startupErr, engines: newFakeEngines().on("remote")})

		frame := harness.readFrame()
		assert.Equal(t, "engine  remote  down  d70bf5a", strings.TrimSpace(splitLines(frame)[0]))
		assert.Contains(t, joinProse(splitLines(frame)), "Press e to choose another engine, or any other key to close.")
	})

	t.Run("e opens the chooser under the error and another key goes to it", func(t *testing.T) {
		engines := newFakeEngines()
		harness := newPanelHarness(t, panelHarnessParams{startupErr: startupErr, engines: engines.on("remote")})

		harness.press("e")
		require.Contains(t, harness.readFrame(), "Open which komrad?")
		assert.False(t, doesQuit(harness.panel.Update(ui.NewKeyPress("x"))), "the chooser takes the keys")

		harness.press("up", "enter")

		assert.Equal(t, []string{"local"}, engines.readSwitched())
	})

	t.Run("esc puts back closing on any key", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{startupErr: startupErr, engines: newFakeEngines().on("remote")})

		harness.press("e", "esc")

		assert.NotContains(t, harness.readFrame(), "Open which komrad?")
		assert.True(t, doesQuit(harness.panel.Update(ui.NewKeyPress("x"))))
	})

	t.Run("a mismatched engine says so", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			startupErr: errors.Join(errui.ErrEngineBuild, errors.New("engine v2")),
			engines:    newFakeEngines().on("remote"),
		})

		assert.Equal(t, "engine  remote  mismatch  d70bf5a", strings.TrimSpace(splitLines(harness.readFrame())[0]))
	})

	t.Run("the config error screen offers it too", func(t *testing.T) {
		engines := newFakeEngines()
		harness := newPanelHarness(t, panelHarnessParams{configErr: errors.New("bad toml"), engines: engines.on("remote")})

		harness.press("e", "up", "enter")

		assert.Equal(t, []string{"local"}, engines.readSwitched())
	})
}

func readLogoLine(frame string) string {
	logo, _, _ := strings.Cut(splitLines(frame)[0], "│")

	return strings.TrimSpace(logo)
}
