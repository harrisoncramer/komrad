// Package palette is the command palette, a popup that fires one of the user's actions on
// one agent and closes. It draws its own list rather than tmux's menu, so the list can be
// narrowed by typing as the number of actions a user has written grows.
package palette

import (
	"fmt"
	"strings"

	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The colours are terminal indexes rather than hex values, for the reason the panel's are:
// the popup is drawn in whatever scheme the terminal is already running.
const (
	colorAccent = lipgloss.Color("4")
	colorMuted  = lipgloss.Color("8")
)

var (
	headerStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(colorMuted)
)

// chromeLines is how many lines the header, the query, the hovered action's description, and
// the help take, which is what the list has to fit inside the popup around it.
const chromeLines = 7

// fallbackHeight is how many rows are drawn before tmux has said how tall the popup is,
// which is only the first frame.
const fallbackHeight = 10

// Item is one action the palette offers, named as the service lists it. A file waiting on a
// chmod is offered and drawn dim, so it reads as a script to fix rather than one that is not
// there.
type Item struct {
	Name        string
	Description string
	Executable  bool
}

// RunParams is what showing the palette takes.
type RunParams struct {
	// Agent is the name of the agent every action here would run on, drawn in the header so
	// the popup says what it is about to act on.
	Agent string
	Items []Item
	// Fire starts the chosen action and returns once the service has it, so an error is
	// always from before the process began.
	Fire func(name string) error
}

// Run draws the palette and returns once an action has been fired or the popup dismissed.
// An action that would not start comes back as an error, which the caller's non-zero exit
// leaves on screen rather than closing the popup over.
func Run(params RunParams) error {
	final, err := tea.NewProgram(newModel(params)).Run()
	if err != nil {
		return fmt.Errorf("failed to draw the palette: %w", err)
	}

	shown, ok := final.(model)
	if !ok {
		return nil
	}

	return shown.failure
}

type model struct {
	agent  string
	items  []Item
	fire   func(name string) error
	query  string
	cursor int
	// height is the popup's, which is how many rows the list draws at once.
	height int
	// failure is what firing an action refused with, carried out of the program since the
	// popup is gone by the time anything can be printed over it.
	failure error
}

func newModel(params RunParams) model {
	return model{agent: params.Agent, items: params.Items, fire: params.Fire, height: fallbackHeight}
}

func (m model) Init() tea.Cmd { return nil }

// Update drives the palette. Every printable key types into the query rather than acting,
// since a letter that both narrowed the list and fired something could be used for neither.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height

		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch k := key.String(); {
	case k == "ctrl+c" || k == "esc":
		return m, tea.Quit
	case k == "enter":
		return m.fireHovered()
	case k == "up" || k == "ctrl+p":
		m.cursor = max(m.cursor-1, 0)
	case k == "down" || k == "ctrl+n":
		m.cursor = min(m.cursor+1, len(m.rows())-1)
	case k == "backspace":
		return m.typeQuery(trimLastRune(m.query)), nil
	case key.Type == tea.KeySpace:
		return m.typeQuery(m.query + " "), nil
	case key.Type == tea.KeyRunes:
		return m.typeQuery(m.query + string(key.Runes)), nil
	}

	return m, nil
}

// typeQuery narrows the list and puts the cursor back on the first row, since the row it was
// on is rarely still under it once the list has moved.
func (m model) typeQuery(query string) model {
	m.query = query
	m.cursor = 0

	return m
}

func (m model) fireHovered() (tea.Model, tea.Cmd) {
	rows := m.rows()
	if m.cursor >= len(rows) {
		return m, nil
	}

	m.failure = m.fire(rows[m.cursor].Name)

	return m, tea.Quit
}

// rows are the actions the list draws, the ones the query still matches.
func (m model) rows() []Item {
	if m.query == "" {
		return m.items
	}

	return xslice.Filter(m.items, func(item Item) bool { return doesMatch(item.Name, m.query) })
}

// doesMatch reports whether a name is kept under the current query, which matches as a
// subsequence so a few letters of a long name reach it. Case is ignored, since a name in the
// actions directory is one file and there is nothing to tell apart by it.
func doesMatch(name, query string) bool {
	name, query = strings.ToLower(name), strings.ToLower(query)

	at := 0
	for _, want := range query {
		found := strings.IndexRune(name[at:], want)
		if found < 0 {
			return false
		}

		at += found + len(string(want))
	}

	return true
}

func (m model) View() string {
	rows := m.rows()
	out := headerStyle.Render("Run Action") + "  " + dimStyle.Render(m.agent) + "\n\n" +
		cursorStyle.Render("/") + " " + m.query + cursorStyle.Render("▏") + "\n\n"

	if len(rows) == 0 {
		return out + dimStyle.Render("no actions") + "\n"
	}

	room := max(m.height-chromeLines, 1)
	first := m.firstDrawn(len(rows), room)

	for i := first; i < min(first+room, len(rows)); i++ {
		prefix := "  "
		if i == m.cursor {
			prefix = cursorStyle.Render("› ")
		}

		name := rows[i].Name
		if !rows[i].Executable {
			name = dimStyle.Render(name)
		}

		out += prefix + name + "\n"
	}

	return out + "\n" + dimStyle.Render(m.describeHovered(rows)) + "\n" +
		dimStyle.Render("enter run · ctrl+n/p move · esc close")
}

// firstDrawn is the row the list starts at, chosen so the cursor is inside the rows the
// popup has room for.
func (m model) firstDrawn(count, room int) int {
	if count <= room || m.cursor < room {
		return 0
	}

	return min(m.cursor-room+1, count-room)
}

// describeHovered is what the action under the cursor says about itself, and its own name
// when its file declared nothing, so the line under the list is never blank.
func (m model) describeHovered(rows []Item) string {
	if m.cursor >= len(rows) {
		return ""
	}

	if description := rows[m.cursor].Description; description != "" {
		return description
	}

	return rows[m.cursor].Name
}

func trimLastRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}

	return string(runes[:len(runes)-1])
}
