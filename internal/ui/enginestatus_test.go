package ui_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"komrad/internal/ui"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"

	"github.com/stretchr/testify/assert"
)

// TestEngineLine covers the engine rendered beside the logo.
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
			engine: "staging-east-central-one",
			want:   "\uf444 staging-east-central-one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{SidebarWidth: tt.width, EngineStatus: newFakeEngine().on(tt.engine)})

			assert.Equal(t, tt.want, paneltest.EngineLine(harness.Frame()))
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
		{
			name: "the dot is red once the heartbeat stops",
			lost: true,
			want: paint.ToneDanger,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{SidebarWidth: 60, EngineStatus: newFakeEngine().on("")})
			if tt.lost {
				harness.LoseWatch(errors.New("connection refused"))
			}

			assert.Contains(t, harness.RawFrame(), paint.Panel.Style(tt.want).Render("\uf444")+" ")
		})
	}

	t.Run("the dot turns green again when the stream comes back", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{SidebarWidth: 60, EngineStatus: newFakeEngine().on("")})

		harness.LoseWatch(errors.New("connection refused"))
		harness.Watch(buildCrowdedSnapshot(""))

		assert.Contains(t, harness.RawFrame(), paint.Panel.Style(paint.ToneSuccess).Render("\uf444")+" ")
	})
}

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
