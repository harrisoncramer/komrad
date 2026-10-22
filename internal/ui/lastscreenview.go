package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/api"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/savedscreen"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// minScreenRows is the fewest rows a saved screen is fitted to under its document before it is
// given the whole column to scroll to instead.
const minScreenRows = 8

type lastScreenRowsParams struct {
	screen  *lastScreen
	agentID string
	width   int
	height  int
}

// lastScreenRows is the saved screen under a line saying when it was taken, fitted to the
// column, or one line saying why there is none.
func lastScreenRows(params lastScreenRowsParams) []string {
	muted := paint.Panel.Style(paint.ToneMuted)
	shown := params.screen

	switch {
	case shown.agentID != params.agentID || !shown.answered:
		return []string{muted.Render("reading the saved screen…")}
	case errors.Is(shown.err, api.ErrNoSavedScreen):
		return []string{muted.Render("no saved screen")}
	case shown.err != nil:
		return []string{muted.Render("could not read the saved screen: " + errui.GetReason(shown.err))}
	}

	saved := shown.screen
	banner := muted.Render(strings.Join([]string{
		"saved screen",
		label.Friendly(saved.GetCapturedAt().AsTime()),
		fmt.Sprintf("%dx%d", saved.GetWidth(), saved.GetHeight()),
	}, separated))
	size := screenSize{width: max(params.width, 1), height: min(max(params.height-1, 1), int(saved.GetHeight()))}
	fitted := shown.fitted.Read(size, func() string {
		return savedscreen.Fit(saved.GetFrame(), size.width, size.height)
	})

	return append([]string{banner}, strings.Split(fitted, "\n")...)
}

func noticeRows(notice string, width int) []string {
	if notice == "" {
		return nil
	}

	const padding = 2

	text := ansi.Truncate(notice, max(width-2*padding, 1), "…")
	palette := paint.Panel
	fill := lipgloss.NewStyle().
		Background(palette.Color(paint.ToneModeVisual)).
		Foreground(palette.Color(paint.ToneInverse)).
		Width(min(ansi.StringWidth(text)+2*padding, max(width, 1)))
	bar := fill.Render("")

	return []string{bar, fill.Bold(true).PaddingLeft(padding).Render(text), bar}
}

// withLastScreen is a column's rows under a notice saying the agent is not running, with the
// saved screen of the agent it screens rendered under them.
func (m *model) withLastScreen(rows []string, width int) []string {
	agentID, ok := m.screenedAgentID()
	if !ok {
		return rows
	}

	top := noticeRows(m.screenNotice(), width)
	if slices.ContainsFunc(rows, func(row string) bool { return strings.TrimSpace(ansi.Strip(row)) != "" }) {
		top = slices.Concat(top, []string{""}, rows)
	}

	height := m.columnHeight() - len(top) - 1
	if height-1 < minScreenRows {
		height = m.columnHeight()
	}

	return slices.Concat(top, []string{""}, lastScreenRows(lastScreenRowsParams{
		screen:  &m.lastScreen,
		agentID: agentID,
		width:   width,
		height:  height,
	}))
}
