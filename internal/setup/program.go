package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

type state int

const (
	statePending state = iota
	stateChecking
	stateAsking
	stateRepairing
	stateDone
	stateSkipped
	stateFailed
)

type row struct {
	step     Step
	state    state
	detail   string
	fix      string
	repaired bool
}

type observedMsg struct {
	index       int
	observation observation
}

type repairedMsg struct {
	index int
	err   error
}

type model struct {
	look    func(Step) observation
	fix     func(Step) error
	rows    []row
	current int
	form    *huh.Form
	spinner spinner.Model
	width   int
	aborted bool
}

// errFailed is what a run returns when a step could not be made true.
var errFailed = fail.Precondition("setup did not finish")

// ErrAborted is what a run returns when the person quit before the last step.
var ErrAborted = fail.Precondition("setup was quit")

// Run walks the steps in a terminal, asking the person whatever a step needs.
func Run(ctx context.Context, check checker, steps []Step) error {
	rows := make([]row, len(steps))
	for i, step := range steps {
		rows[i] = row{step: step}
	}

	final, err := tea.NewProgram(newModel(ctx, check, rows), tea.WithContext(ctx)).Run()
	if err != nil {
		return err
	}

	finished, ok := final.(model)
	if !ok {
		return ErrAborted
	}

	return outcome(finished)
}

func newModel(ctx context.Context, check checker, rows []row) model {
	return model{
		look:    func(step Step) observation { return step.observe(ctx, check) },
		fix:     func(step Step) error { return step.repair(ctx) },
		rows:    rows,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func outcome(m model) error {
	if m.aborted {
		return ErrAborted
	}
	for _, r := range m.rows {
		if r.state == stateFailed {
			return errFailed
		}
	}

	return nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.observe())
}

func (m model) observe() tea.Cmd {
	if m.current >= len(m.rows) {
		return tea.Quit
	}

	index, step, look := m.current, m.rows[m.current].step, m.look
	m.rows[index].state = stateChecking

	return func() tea.Msg {
		return observedMsg{index: index, observation: look(step)}
	}
}

func (m model) repair() tea.Cmd {
	index, step, fix := m.current, m.rows[m.current].step, m.fix
	m.rows[index].state = stateRepairing
	m.rows[index].repaired = true

	return func() tea.Msg {
		return repairedMsg{index: index, err: fix(step)}
	}
}

func (m model) advance() (tea.Model, tea.Cmd) {
	m.current++
	m.form = nil

	return m, m.observe()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.aborted = true

			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd

	case observedMsg:
		return m.observed(msg)

	case repairedMsg:
		return m.repaired(msg)
	}

	if m.form != nil {
		return m.updateForm(msg)
	}

	return m, nil
}

func (m model) observed(msg observedMsg) (tea.Model, tea.Cmd) {
	r := &m.rows[msg.index]
	r.detail = msg.observation.Detail
	if msg.observation.Finding != nil {
		r.fix = msg.observation.Finding.Fix
	}

	switch msg.observation.Verdict {
	case verdictDone:
		r.state = stateDone

		return m.advance()

	case verdictStop:
		r.state = stateFailed

		return m, tea.Quit

	case verdictSkip:
		r.state = stateSkipped

		return m.advance()

	case verdictAsk, verdictRepair:
		if r.repaired {
			r.state = stateFailed

			return m, tea.Quit
		}
	}

	if msg.observation.Verdict == verdictRepair {
		return m, m.repair()
	}

	r.state = stateAsking
	m.form = r.step.question()
	if m.form == nil {
		return m, m.repair()
	}
	m.form = m.form.WithShowHelp(true)

	return m, m.form.Init()
}

func (m model) repaired(msg repairedMsg) (tea.Model, tea.Cmd) {
	r := &m.rows[msg.index]
	m.form = nil

	switch {
	case errors.Is(msg.err, errSkipped):
		r.state = stateSkipped

		return m.advance()

	case msg.err != nil:
		r.state = stateFailed
		r.detail = msg.err.Error()

		return m, tea.Quit
	}

	return m, m.observe()
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.form.Update(msg)
	if form, ok := updated.(*huh.Form); ok {
		m.form = form
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m, m.repair()
	case huh.StateAborted:
		m.aborted = true

		return m, tea.Quit
	case huh.StateNormal:
	}

	return m, cmd
}

var (
	markDone    = glyph.Defaults().Spell(glyph.MessageSuccess)
	markFailed  = glyph.Defaults().Spell(glyph.MessageError)
	markPending = glyph.Defaults().Spell(glyph.MessageInfo)
)

func (m model) View() tea.View {
	var b strings.Builder
	for i, r := range m.rows {
		fmt.Fprintf(&b, "%s %-9s %s\n", m.mark(r), r.step.name(), paint.Panel.Style(paint.ToneMuted).Render(r.detail))
		if (r.state == stateFailed || r.state == stateSkipped) && r.fix != "" {
			fmt.Fprintf(&b, "            fix: %s\n", r.fix)
		}
		if i == m.current && m.form != nil && r.state == stateAsking {
			b.WriteString("\n" + m.form.View() + "\n\n")
		}
	}

	return tea.NewView(b.String())
}

func (m model) mark(r row) string {
	switch r.state {
	case stateChecking, stateRepairing:
		return m.spinner.View()
	case stateDone:
		return paint.Panel.Style(paint.ToneSuccess).Render(markDone)
	case stateSkipped:
		return paint.Panel.Style(paint.ToneMuted).Render("-")
	case stateFailed:
		return paint.Panel.Style(paint.ToneDanger).Render(markFailed)
	case stateAsking:
		return "?"
	case statePending:
	}

	return paint.Panel.Style(paint.ToneMuted).Render(markPending)
}

// RunUnattended walks the steps with nobody to ask, printing a line per step and taking every
// answer from a flag.
func RunUnattended(ctx context.Context, out io.Writer, check checker, steps []Step) error {
	for _, step := range steps {
		line, err := runUnattended(ctx, check, step)
		if _, writeErr := fmt.Fprintln(out, line); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func runUnattended(ctx context.Context, check checker, step Step) (string, error) {
	name := fmt.Sprintf("%-9s", step.name())

	observation := step.observe(ctx, check)
	switch observation.Verdict {
	case verdictDone:
		return markDone + " " + name + " " + observation.Detail, nil
	case verdictStop:
		return describe(markFailed, name, observation), errFailed
	case verdictSkip:
		return describe("-", name, observation), nil
	case verdictAsk:
		if err := step.answerUnattended(); err != nil {
			return markFailed + " " + name + " " + err.Error(), errFailed
		}
	case verdictRepair:
	}

	if err := step.repair(ctx); err != nil {
		if errors.Is(err, errSkipped) {
			return "- " + name + " skipped", nil
		}

		return markFailed + " " + name + " " + err.Error(), errFailed
	}

	observation = step.observe(ctx, check)
	switch observation.Verdict {
	case verdictDone:
		return markDone + " " + name + " " + observation.Detail, nil
	case verdictSkip:
		return describe("-", name, observation), nil
	case verdictAsk, verdictRepair, verdictStop:
	}

	return describe(markFailed, name, observation), errFailed
}

func describe(mark, name string, observation observation) string {
	line := mark + " " + name + " " + observation.Detail
	if observation.Finding != nil && observation.Finding.Fix != "" {
		line += "\n            fix: " + observation.Finding.Fix
	}

	return line
}
