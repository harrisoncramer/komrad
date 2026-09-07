package ui

import (
	"strings"

	"weave/internal/errui"
)

// View renders the active screen, taking the failures in the order they happen so that what is
// shown is the first thing that broke rather than the last thing it caused.
func (m model) View() string {
	if m.configErr != nil {
		return errui.RenderConfigError(m.width, m.configErr)
	}

	if m.startupErr != nil {
		return errui.RenderStartupError(m.width, m.startupErr, m.startupFindings)
	}

	if m.unreachable != nil {
		return errui.RenderUnreachableError(m.width, m.addr, m.unreachable)
	}

	var chrome string
	switch {
	case m.pendingForm:
		chrome = dimStyle.Render("  submitting…") + "\n\n"
	case m.banner != nil:
		chrome = m.banner.Render(m.width)
	}

	return m.fitFrame(strings.TrimRight(chrome+m.screenView(), "\n"))
}

// fitFrame pads the panel out to the height of its pane, so a shorter frame covers the rows the
// one before it left behind.
func (m model) fitFrame(view string) string {
	if m.height <= 0 || m.width <= 0 {
		return view
	}

	rows := strings.Count(view, "\n") + 1
	if rows >= m.height {
		return view
	}

	// Spaces rather than empty rows, since only a written cell covers what was there.
	blank := strings.Repeat(" ", m.width)

	return view + strings.Repeat("\n"+blank, m.height-rows)
}

func (m model) screenView() string {
	if !m.form.isActive() {
		return m.screen.spec().view(m)
	}

	split, ok := m.columnSplit()
	if !ok {
		return m.form.form.View()
	}

	// Asked for before either half is drawn, so the halves are laid out already knowing their
	// column rather than sending every line of chrome back through paneWidth into the geometry.
	held := m.at(split.sidebar)

	// The list is drawn beside the form as the context the form was opened from, and takes none of
	// the keys while it is there, since the form has them all.
	return m.joinColumns(split, held.screen.spec().view(held), m.form.form.View())
}
