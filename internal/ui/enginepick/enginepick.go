// Package enginepick is the small picker the panel key opens when more than one komrad can be
// attached to: this machine, or an engine somewhere else.
package enginepick

import (
	"context"
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const listWidth = 30

const gutter = 2

const localName = "this machine"

// Row is one komrad the picker offers.
type Row struct {
	// Name is the engine's name, empty for this machine.
	Name string
	// Facts are the settings shown beside the row while the cursor is on it.
	Facts []Fact
}

// Fact is one labelled setting an engine is described by.
type Fact struct {
	Label string
	Value string
}

// Status is what a probe found out about one row.
type Status struct {
	// Build names the build answering there, or says why nothing is.
	Build string
	// Label is the one word drawn beside a refused row.
	Label string
	// Refused says why the row cannot be attached to, empty when it can.
	Refused string
}

// Title is how a row is named on screen.
func (r Row) Title() string {
	if r.Name == "" {
		return localName
	}

	return r.Name
}

// Probe finds out what a row is running, called once per row off the update loop.
type Probe func(row Row) Status

type RunParams struct {
	Rows  []Row
	Probe Probe
	// Self names this komrad's build, drawn in the header.
	Self string
}

// Run draws the picker until a row is chosen or the picker is closed, answering with the row
// and whether one was chosen.
func Run(ctx context.Context, params RunParams) (Row, bool, error) {
	program := tea.NewProgram(newModel(params), tea.WithContext(ctx))

	final, err := program.Run()
	if err != nil {
		return Row{}, false, fmt.Errorf("failed to run the engine picker: %w", err)
	}

	m, ok := final.(model)
	if !ok || m.chosen < 0 {
		return Row{}, false, nil
	}

	return m.rows[m.chosen], true, nil
}

type probedMsg struct {
	index  int
	status Status
}

type model struct {
	rows     []Row
	statuses []Status
	probed   []bool
	probe    Probe
	self     string
	cursor   int
	chosen   int
	notice   string
	width    int
}

func newModel(params RunParams) model {
	return model{
		rows:     params.Rows,
		statuses: make([]Status, len(params.Rows)),
		probed:   make([]bool, len(params.Rows)),
		probe:    params.Probe,
		self:     params.Self,
		chosen:   -1,
	}
}

// Init starts one probe per row.
func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.rows))
	for index := range m.rows {
		cmds = append(cmds, m.probeCmd(index))
	}

	return tea.Batch(cmds...)
}

func (m model) probeCmd(index int) tea.Cmd {
	if m.probe == nil {
		return nil
	}

	row, probe := m.rows[index], m.probe

	return func() tea.Msg {
		return probedMsg{index: index, status: probe(row)}
	}
}

// Update moves the cursor, takes a choice, and records what the probes found.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

		return m, nil
	case probedMsg:
		if msg.index >= 0 && msg.index < len(m.rows) {
			m.statuses[msg.index] = msg.status
			m.probed[msg.index] = true
		}

		return m, nil
	case tea.KeyPressMsg:
		return m.press(msg)
	}

	return m, nil
}

func (m model) press(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
		m.notice = ""
	case "down", "j":
		m.cursor = min(m.cursor+1, len(m.rows)-1)
		m.notice = ""
	case "enter":
		return m.choose()
	}

	return m, nil
}

func (m model) choose() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}

	if !m.probed[m.cursor] {
		m.notice = "still checking what " + m.rows[m.cursor].Title() + " is running"

		return m, nil
	}

	if refused := m.statuses[m.cursor].Refused; refused != "" {
		m.notice = refused

		return m, nil
	}

	m.chosen = m.cursor

	return m, tea.Quit
}

// View is the picker on the alternate screen.
func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true

	return view
}

// render draws the rows in a column with the cursor's row described beside them.
func (m model) render() string {
	list := make([]string, 0, len(m.rows)+2)
	list = append(list, paint.Panel.Header().Render("Open which komrad?"), "")

	for index, row := range m.rows {
		list = append(list, m.rowLine(index, row))
	}

	header := paint.Panel.Style(paint.ToneMuted).Render("this komrad is " + m.self)
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listWidth).Render(strings.Join(list, "\n")),
		strings.Repeat(" ", gutter),
		m.preview())

	footer := paint.Panel.Style(paint.ToneMuted).Render("f move  enter open  esc close")
	if m.notice != "" {
		footer = paint.Panel.Style(paint.ToneDanger).Render(m.notice)
	}

	return header + "\n\n" + body + "\n\n" + footer
}

func (m model) rowLine(index int, row Row) string {
	marker := "  "
	if index == m.cursor {
		marker = paint.Panel.Cursor().Render("▸ ")
	}

	title := row.Title()

	var tail string

	switch {
	case !m.probed[index]:
		tail = paint.Panel.Style(paint.ToneMuted).Render("checking")
	case m.statuses[index].Refused != "":
		title = paint.Panel.Style(paint.ToneMuted).Render(title)
		tail = paint.Panel.Style(paint.ToneDanger).Render(m.statuses[index].Label)
	default:
		tail = paint.Panel.Style(paint.ToneSuccess).Render("ready")
	}

	return ansi.Truncate(marker+title+"  "+tail, listWidth, "…")
}

func (m model) preview() string {
	if len(m.rows) == 0 {
		return ""
	}

	row, status := m.rows[m.cursor], m.statuses[m.cursor]
	width := max(m.width-listWidth-gutter, 20)

	lines := append([]string{paint.Panel.Header().Render(row.Title()), ""}, xslice.Map(row.Facts, func(fact Fact) string {
		return fmt.Sprintf("%-7s  ", fact.Label) + paint.Panel.Style(paint.ToneBright).Render(fact.Value)
	})...)

	lines = append(lines, "")

	switch {
	case !m.probed[m.cursor]:
		lines = append(lines, paint.Panel.Style(paint.ToneMuted).Render("checking what it is running"))
	case status.Refused != "":
		lines = append(lines, "running  "+status.Build)
		lines = append(lines, xslice.Map(paint.WrapRows(status.Refused, width), func(line string) string {
			return paint.Panel.Style(paint.ToneDanger).Render(line)
		})...)
	default:
		lines = append(lines, "running  "+status.Build)
	}

	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}
