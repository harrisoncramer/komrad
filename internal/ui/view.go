package ui

import (
	"strings"

	"weave/internal/errui"
)

// View renders the active screen, taking the failures in the order they happen so that what is
// shown is the first thing that broke rather than the last thing it caused.
func (m model) View() string {
	if m.configErr != nil {
		return errui.Config(m.width, m.configErr)
	}

	if m.startupErr != nil {
		return errui.Startup(m.width, m.startupErr, m.startupFindings)
	}

	if m.unreachable != nil {
		return errui.Unreachable(m.width, m.addr, m.unreachable)
	}

	var chrome string
	switch {
	case m.pendingForm:
		chrome = dimStyle.Render("  submitting…") + "\n\n"
	case m.banner != nil:
		chrome = m.banner.Render(m.width)
	}

	return strings.TrimRight(chrome+m.screenView(), "\n")
}

func (m model) screenView() string {
	if m.form.active() {
		return m.form.form.View()
	}

	return m.screen.spec().view(m)
}
