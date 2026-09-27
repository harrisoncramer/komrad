package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// Engines is what the panel knows of the engine it is on and of the others this machine can open.
type Engines struct {
	// Name is the engine this panel is on, empty for this machine.
	Name string
	// Build is what the engine's server said it runs when the panel started.
	Build EngineBuild
	// ReadBuild asks the engine's server what it runs, reporting whether it answered.
	ReadBuild func(ctx context.Context) (EngineBuild, bool)
	// List reads the engines this machine can open, itself first.
	List func() ([]EngineRow, error)
	// Probe finds out whether one engine can be opened.
	Probe func(ctx context.Context, row EngineRow) EngineStatus
	// Switch moves this terminal to the named engine.
	Switch func(name string) error
}

// EngineBuild is what an engine's server said it runs.
type EngineBuild struct {
	// Commit is the short commit the server was built from.
	Commit string
	// Stale says how the server's code differs from the panel's, empty when they agree.
	Stale string
}

// EngineRow is one engine the chooser offers.
type EngineRow struct {
	// Name is the engine's name, empty for this machine.
	Name string
	// Facts are the settings the engine is described by.
	Facts []EngineFact
}

// EngineFact is one labelled setting an engine is described by.
type EngineFact struct {
	Label string
	Value string
}

// EngineStatus is what a probe found out about one engine.
type EngineStatus struct {
	// Build names the build answering there, or says why nothing is.
	Build string
	// Label is the one word drawn beside a refused row.
	Label string
	// Refused says why the engine cannot be opened, empty when it can.
	Refused string
}

// Title is how an engine is named on screen.
func (r EngineRow) Title() string {
	return engineTitle(r.Name)
}

func engineTitle(name string) string {
	if name == "" {
		return config.LocalEngine
	}

	return name
}

// chooserState is the chooser's rows and what the probes have found of them.
type chooserState struct {
	build EngineBuild
	rows  []EngineRow
	found map[string]EngineStatus
	// round tells this opening's answers from an earlier one's.
	round  int
	choice *string
}

// engineProbedMsg is one probe's answer.
type engineProbedMsg struct {
	round  int
	name   string
	status EngineStatus
}

// engineBuildMsg is the engine's server saying again what it runs.
type engineBuildMsg struct {
	build EngineBuild
}

// engineSwitchedMsg is how a switch to another engine ended.
type engineSwitchedMsg struct {
	name string
	err  error
}

// engineProbeWait bounds one probe.
const engineProbeWait = 30 * time.Second

var errEngineUnchecked = errors.New("still checking what it is running")

// engineWord is the status word the logo line carries, empty when nothing is wrong.
func (m model) engineWord() (string, paint.Tone) {
	switch {
	case errors.Is(m.startupErr, errui.ErrEngineBuild), m.chooser.build.Stale != "":
		return "mismatch", paint.ToneWarning
	case errors.Is(m.startupErr, errui.ErrRebuilt):
	case m.startupErr != nil, m.unreachable != nil:
		return "down", paint.ToneDanger
	}

	return "", paint.ToneMuted
}

// engineKeys is the key that opens the chooser, the default when the config could not be read.
func (m model) engineKeys() []string {
	if len(m.keys.Views.Engine) == 0 {
		return []string{"e"}
	}

	return m.keys.Views.Engine
}

// engineParts is the engine's label, name, status word and commit, in the order a narrow pane
// gives them up from the end.
func (m model) engineParts() (label, name, word, commit string) {
	marked := stops{labels: []string{"engine"}, marks: []string{headLabel(m.engineKeys())}, focus: noStop, lit: noStop}
	label = marked.render(paint.Panel.Style(paint.ToneMuted), 0)
	name = paint.Panel.Style(paint.ToneBright).Render(engineTitle(m.engines.Name))

	if text, tone := m.engineWord(); text != "" {
		word = paint.Panel.Style(tone).Render(text)
	}

	if m.chooser.build.Commit != "" {
		commit = paint.Panel.Style(paint.ToneMuted).Render(m.chooser.build.Commit)
	}

	return label, name, word, commit
}

// engineLine is the engine drawn in at most width cells, dropping the commit and then the status
// word before the label and the name.
func (m model) engineLine(width int) string {
	label, name, word, commit := m.engineParts()
	parts := []string{label, name}

	for _, tail := range [][]string{{word, commit}, {word}, {}} {
		fitted := append(append([]string{}, parts...), xslice.Filter(tail, func(part string) bool { return part != "" })...)
		line := strings.Join(fitted, "  ")

		if ansi.StringWidth(line) <= width || len(tail) == 0 {
			return line
		}
	}

	return strings.Join(parts, "  ")
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

// canChooseEngine reports whether the panel was given a way to read and reach other engines.
func (m model) canChooseEngine() bool {
	return m.engines.List != nil && m.engines.Probe != nil && m.engines.Switch != nil
}

// openEngines reads the engines this machine can open and starts one probe for each.
func (m model) openEngines() (model, tea.Cmd) {
	rows, err := m.engines.List()
	if err != nil {
		m.report("Could not read the engines", err)

		return m, nil
	}

	choice := m.engines.Name
	m.chooser.rows = rows
	m.chooser.found = map[string]EngineStatus{}
	m.chooser.round++
	m.chooser.choice = &choice

	return m, tea.Batch(xslice.Map(rows, m.probeEngineCmd)...)
}

func (m model) probeEngineCmd(row EngineRow) tea.Cmd {
	probe, round := m.engines.Probe, m.chooser.round

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), engineProbeWait)
		defer cancel()

		return engineProbedMsg{round: round, name: row.Name, status: probe(ctx, row)}
	}
}

func (m model) readEngineBuildCmd() tea.Cmd {
	read := m.engines.ReadBuild
	if read == nil {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), engineProbeWait)
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
	was := m.chooser.build.Stale
	m.chooser.build = msg.build

	if msg.build.Stale != "" && was == "" {
		m.pushMessage(message{
			kind:    messageWarning,
			summary: "The engine is running different code",
			detail:  msg.build.Stale,
		})
	}

	return m
}

// landEngineProbe records a probe's answer and draws the chooser again around it.
func (m model) landEngineProbe(msg engineProbedMsg) (model, tea.Cmd) {
	if msg.round != m.chooser.round || m.chooser.found == nil {
		return m, nil
	}

	m.chooser.found[msg.name] = msg.status

	if m.form.kind != formEngine {
		return m, nil
	}

	cmd := m.rebuildForm()

	return m, cmd
}

// openEngineChooser draws the chooser beside the list.
func (m model) openEngineChooser() (tea.Model, tea.Cmd) {
	m.viewsOpen = false

	next, probes := m.openEngines()
	if next.chooser.choice == nil {
		return next, probes
	}

	opened := next.openForm(next.engineForm())

	return next, tea.Batch(probes, opened)
}

// engineForm is the chooser as a form the panel draws again as each probe answers.
func (m model) engineForm() formState {
	return formState{
		kind:   formEngine,
		update: model.updateForm,
		submit: model.chooseEngine,
		rebuild: func(m model) (*huh.Form, []string) {
			return m.engineChooser()
		},
	}
}

// engineChooser is the chooser's fields, the reason under the list following the cursor.
func (m model) engineChooser() (*huh.Form, []string) {
	width := 0
	for _, row := range m.chooser.rows {
		width = max(width, ansi.StringWidth(row.Title()))
	}

	options := xslice.Map(m.chooser.rows, func(row EngineRow) huh.Option[string] {
		return huh.NewOption(m.engineOptionLabel(row, width), row.Name)
	})

	choice := m.chooser.choice

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Key("engine").
			Title("Open which komrad?").
			Options(options...).
			Value(choice),
		huh.NewNote().
			DescriptionFunc(func() string { return m.engineReason(*choice) }, choice),
	)).WithShowHelp(true).WithKeyMap(engineChooserKeyMap()).WithTheme(paint.Panel.FormTheme())

	return form, []string{"engine"}
}

// engineChooserKeyMap opens the engine on enter and has no filter.
func engineChooserKeyMap() *huh.KeyMap {
	keys := formKeys()
	km := gitMenuKeyMap(keys)
	km.Select.Submit = key.NewBinding(key.WithKeys(append([]string{"enter"}, keys.Submit...)...), key.WithHelp("enter", "open"))
	km.Select.Next.SetEnabled(false)

	return km
}

// engineOptionLabel is a row's name padded to the widest, the word its probe gave, and whether
// this panel is on it.
func (m model) engineOptionLabel(row EngineRow, width int) string {
	label := row.Title() + strings.Repeat(" ", width-ansi.StringWidth(row.Title()))

	status, probed := m.chooser.found[row.Name]

	switch {
	case !probed:
		label += "  checking"
	case status.Refused != "":
		label += "  " + status.Label
	}

	if row.Name == m.engines.Name {
		label += "  (open)"
	}

	return label
}

// engineReason is what the chooser says under the list about the engine the cursor is on.
func (m model) engineReason(name string) string {
	status, probed := m.chooser.found[name]

	switch {
	case !probed:
		return engineTitle(name) + " " + errEngineUnchecked.Error()
	case status.Refused != "":
		return status.Refused
	default:
		return engineTitle(name) + "  runs " + status.Build
	}
}

// chooseEngine moves the terminal to the engine the cursor is on, or keeps the chooser up while
// that engine cannot be opened.
func (m model) chooseEngine() (tea.Model, tea.Cmd) {
	name := *m.chooser.choice
	status, probed := m.chooser.found[name]

	if name == m.engines.Name {
		return m.closeEngineChooser()
	}

	if !probed || status.Refused != "" {
		cmd := m.rebuildForm()

		return m, cmd
	}

	next, closed := m.closeEngineChooser()

	return next, tea.Batch(closed, m.switchEngineCmd(name))
}

// closeEngineChooser takes the chooser down, on the error screen as well as over a list.
func (m model) closeEngineChooser() (tea.Model, tea.Cmd) {
	if m.fatal() != nil {
		m.form = formState{}

		return m, nil
	}

	return m.closeForm()
}

func (m model) switchEngineCmd(name string) tea.Cmd {
	switchTo := m.engines.Switch

	return func() tea.Msg {
		return engineSwitchedMsg{name: name, err: switchTo(engineTitle(name))}
	}
}

// landEngineSwitch says why the terminal is still here.
func (m model) landEngineSwitch(msg engineSwitchedMsg) model {
	if msg.err != nil {
		m.report(fmt.Sprintf("Could not open %s", engineTitle(msg.name)), msg.err)
	}

	return m
}

// fatalCloseHint is the last line of the error screen, naming the key that opens the chooser
// when there is one.
func (m model) fatalCloseHint() string {
	if !m.canChooseEngine() {
		return errui.CloseHint
	}

	return "Press " + headLabel(m.engineKeys()) + " to choose another engine, or any other key to close."
}

// routeFatal is the error screen's keys: the chooser key opens the chooser, and every other key
// closes the panel unless the chooser is up.
func (m model) routeFatal(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case engineProbedMsg:
		return m.landEngineProbe(msg)
	case engineSwitchedMsg:
		m.form = formState{}
		m.switchErr = msg.err

		return m, nil
	}

	if m.form.kind == formEngine {
		return m.updateFatalChooser(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.canChooseEngine() && isBound(msg.String(), m.engineKeys()) {
			return m.openFatalChooser()
		}

		return m, tea.Quit
	case tea.BlurMsg:
		return m, tea.Quit
	}

	return m, nil
}

// openFatalChooser draws the chooser under the error.
func (m model) openFatalChooser() (tea.Model, tea.Cmd) {
	next, probes := m.openEngines()
	if next.chooser.choice == nil {
		return next, probes
	}

	next.form = next.engineForm()
	next.form.form, next.form.order = next.engineChooser()

	return next, tea.Batch(probes, next.form.form.Init(), formResized())
}

// updateFatalChooser hands the chooser on the error screen its messages, sized to the pane.
func (m model) updateFatalChooser(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case formResizedMsg, tea.WindowSizeMsg:
		msg = tea.WindowSizeMsg{Width: m.width, Height: max(m.height/2, 1)}
	case tea.BlurMsg, tea.FocusMsg:
		return m, nil
	}

	if press, ok := msg.(tea.KeyPressMsg); ok && isBound(press.String(), formKeys().Submit) {
		return m.chooseEngine()
	}

	form, cmd := m.form.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form.form = updated
	}

	switch m.form.form.State {
	case huh.StateCompleted:
		return m.chooseEngine()
	case huh.StateAborted:
		m.form = formState{}

		return m, nil
	case huh.StateNormal:
	}

	return m, cmd
}

// fatalView is the error screen, under the engine it is about and over the chooser when that is up.
func (m model) fatalView(screen string) string {
	if !m.canChooseEngine() {
		return screen
	}

	lines := []string{m.engineLine(m.width), strings.Repeat("─", max(m.width, 0)), screen}

	if m.switchErr != nil {
		lines = append(lines, paint.Panel.Style(paint.ToneDanger).Render(ansi.Wrap("Could not switch: "+m.switchErr.Error(), max(m.width, 1), "")))
	}

	if m.form.kind == formEngine && m.form.form != nil {
		lines = append(lines, m.form.form.View())
	}

	return strings.Join(lines, "\n")
}
