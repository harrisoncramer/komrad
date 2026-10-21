package ui

import (
	"context"
	"errors"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
)

// EngineStatus is what the panel knows of the engine it is on.
type EngineStatus struct {
	// Name is the engine this panel is on, empty for this machine.
	Name string
	// Build is what the engine's server said it runs when the panel started.
	Build EngineBuild
	// ReadBuild asks the engine's server what it runs, reporting whether it answered.
	ReadBuild func(ctx context.Context) (EngineBuild, bool)
}

// EngineBuild is what an engine's server said it runs.
type EngineBuild struct {
	// Commit is the short commit the server was built from.
	Commit string
}

func engineTitle(name string) string {
	if name == "" {
		return config.LocalEngine
	}

	return name
}

// engineBuildMsg is the engine's server saying again what it runs.
type engineBuildMsg struct {
	build EngineBuild
}

// engineTone is the colour of the dot beside the engine, green while its heartbeat arrives.
func (m *model) engineTone() paint.Tone {
	switch {
	case errors.Is(m.startupErr, errui.ErrRebuilt):
	case m.startupErr != nil, m.unreachable != nil:
		return paint.ToneDanger
	}

	return paint.ToneSuccess
}

// engineBuildWait bounds one read of what the engine runs.
const engineBuildWait = 30 * time.Second

func (m *model) readEngineBuildCmd() tea.Cmd {
	read := m.engineStatus.ReadBuild
	if read == nil {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), engineBuildWait)
		defer cancel()

		build, answered := read(ctx)
		if !answered {
			return nil
		}

		return engineBuildMsg{build: build}
	}
}

// landEngineBuild takes what the server says it runs.
func (m *model) landEngineBuild(msg engineBuildMsg) {
	m.engineBuild = msg.build
}
