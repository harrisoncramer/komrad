package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Engines is what the panel knows of the engine it is on.
type Engines struct {
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
	// Stale says how the server's code differs from the panel's, empty when they agree.
	Stale string
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
func (m model) engineTone() paint.Tone {
	switch {
	case errors.Is(m.startupErr, errui.ErrEngineBuild), m.engineBuild.Stale != "":
		return paint.ToneWarning
	case errors.Is(m.startupErr, errui.ErrRebuilt):
	case m.startupErr != nil, m.unreachable != nil:
		return paint.ToneDanger
	}

	return paint.ToneSuccess
}

// engineLine is the engine drawn in at most width cells, dropping the commit before the name.
func (m model) engineLine(width int) string {
	muted := paint.Panel.Style(paint.ToneMuted)
	line := paint.Panel.Style(m.engineTone()).Render(m.glyphs.Spell(glyph.Engine)) + " " + muted.Render(engineTitle(m.engines.Name))

	if m.engineBuild.Commit == "" {
		return line
	}

	withCommit := line + " " + muted.Render(m.engineBuild.Commit)
	if width > 0 && ansi.StringWidth(withCommit) > width {
		return line
	}

	return withCommit
}

// logoLine is the logo with the engine right aligned beside it.
func (m model) logoLine(logo string, width int) string {
	if width <= 0 {
		return logo + "  " + m.engineLine(0)
	}

	engine := m.engineLine(width - ansi.StringWidth(logo) - 2)
	gap := width - ansi.StringWidth(logo) - ansi.StringWidth(engine)

	return logo + strings.Repeat(" ", max(gap, 2)) + engine
}

// engineBuildWait bounds one read of what the engine runs.
const engineBuildWait = 30 * time.Second

func (m model) readEngineBuildCmd() tea.Cmd {
	read := m.engines.ReadBuild
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

// landEngineBuild takes what the server says it runs, warning once each time it stops agreeing
// with the panel.
func (m model) landEngineBuild(msg engineBuildMsg) model {
	was := m.engineBuild.Stale
	m.engineBuild = msg.build

	if msg.build.Stale != "" && was == "" {
		m.pushMessage(message{
			kind:    messageWarning,
			summary: "The engine is running different code",
			detail:  msg.build.Stale,
		})
	}

	return m
}
