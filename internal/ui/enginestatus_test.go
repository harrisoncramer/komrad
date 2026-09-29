package ui_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"komrad/internal/ui"
	"komrad/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// fakeEngine is an engine whose server answers with whatever build it is set to.
type fakeEngine struct {
	mu    sync.Mutex
	build ui.EngineBuild
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{build: ui.EngineBuild{Commit: "d70bf5a"}}
}

func (f *fakeEngine) on(name string) ui.EngineStatus {
	return ui.EngineStatus{
		Name:  name,
		Build: f.build,
		ReadBuild: func(context.Context) (ui.EngineBuild, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()

			return f.build, true
		},
	}
}

// TestEngineLine covers the engine drawn beside the logo.
func TestEngineLine(t *testing.T) {
	tests := []struct {
		name string
		// width is the sidebar's.
		width  int
		engine string
		want   string
	}{
		{
			name:  "the engine is named with the commit its server runs",
			width: 60,
			want:  "\uf444 local d70bf5a",
		},
		{
			name:   "a narrow pane gives up the commit",
			width:  34,
			engine: "staging-east",
			want:   "\uf444 staging-east",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{sidebarWidth: tt.width, engineStatus: newFakeEngine().on(tt.engine)})

			logo := readLogoLine(harness.readFrame())
			assert.True(t, strings.HasSuffix(logo, tt.want), "%q ends %q", logo, tt.want)
			assert.True(t, strings.HasPrefix(logo, "\U000F050C komrad"))
		})
	}
}

// TestEngineDot covers the dot beside the engine following its heartbeat and build.
func TestEngineDot(t *testing.T) {
	tests := []struct {
		name string
		lost bool
		want paint.Tone
	}{
		{name: "the dot is green while the heartbeat arrives", want: paint.ToneSuccess},
		{name: "the dot is red once the heartbeat stops", lost: true, want: paint.ToneDanger},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{sidebarWidth: 60, engineStatus: newFakeEngine().on("")})
			if tt.lost {
				harness.send(ui.NewWatchLost(errors.New("connection refused")))
			}

			assert.Contains(t, harness.readRawFrame(), paint.Panel.Style(tt.want).Render("\uf444")+" ")
		})
	}

	t.Run("the dot turns green again when the stream comes back", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{sidebarWidth: 60, engineStatus: newFakeEngine().on("")})

		harness.send(ui.NewWatchLost(errors.New("connection refused")))
		harness.watch(buildCrowdedSnapshot(""))

		assert.Contains(t, harness.readRawFrame(), paint.Panel.Style(paint.ToneSuccess).Render("\uf444")+" ")
	})

	t.Run("a server back on other code is a mismatch, warned of once", func(t *testing.T) {
		engine := newFakeEngine()
		harness := openCrowded(t, panelHarnessParams{sidebarWidth: 60, engineStatus: engine.on("remote")})

		engine.build = ui.EngineBuild{Commit: "a41c9e0", Stale: "rebuild komrad at a41c9e0"}
		harness.send(ui.NewWatchLost(errors.New("connection refused")))
		harness.watch(buildCrowdedSnapshot(""))

		frame := harness.readFrame()
		assert.True(t, strings.HasSuffix(readLogoLine(frame), "\uf444 remote a41c9e0"))
		assert.Contains(t, harness.readRawFrame(), paint.Panel.Style(paint.ToneWarning).Render("\uf444")+" ")
		assert.Contains(t, frame, "The engine is running different code")

		harness.send(ui.NewWatchLost(errors.New("connection refused")))
		harness.watch(buildCrowdedSnapshot(""))

		frame = harness.readFrame()
		assert.Contains(t, frame, "+2", "the stream lost twice and the mismatch")
		assert.NotContains(t, frame, "+3", "a mismatch that stayed is not warned of again")
	})
}

func readLogoLine(frame string) string {
	logo, _, _ := strings.Cut(splitLines(frame)[0], "│")

	return strings.TrimSpace(logo)
}
