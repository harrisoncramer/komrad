package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// sectionsKey is the form key of the list of sections on a sectioned form's top form.
const sectionsKey = "sections"

// sectionedForm is a form whose top fields are always drawn and whose other fields are kept in
// sections, each opened from the top form on its own.
type sectionedForm struct {
	kind  formKind
	title string
	// top is the fields drawn above the list of sections.
	top         func(m model) []huh.Field
	topEditable []editableText
	// reshaped reports whether an answer on the top form has changed which sections are off.
	reshaped func() bool
	sections []formSection
	// choice is the section the list's cursor is on.
	choice *string
	submit func(model) (tea.Model, tea.Cmd)
}

// formSection is one section of a sectioned form.
type formSection struct {
	key   string
	title string
	// isOff says the section has nothing to ask, nil for never.
	isOff    func() bool
	fields   func(m model) []huh.Field
	editable []editableText
}

func (s sectionedForm) topState() formState {
	return formState{
		kind:           s.kind,
		keepsOnRefusal: true,
		update:         s.updateTop,
		submit:         s.submit,
		rebuild:        s.buildTop,
		reshaped:       s.reshaped,
		editable:       s.topEditable,
	}
}

func (s sectionedForm) buildTop(m model) (*huh.Form, []string) {
	return formOf(formKeys(), append(s.top(m), s.sectionsField())...)
}

func (s sectionedForm) sectionsField() huh.Field {
	muted := paint.Panel.Style(paint.ToneMuted)

	options := xslice.Map(s.sections, func(section formSection) huh.Option[string] {
		label := section.title + nestedMark
		if section.off() {
			label = muted.Render(label)
		}

		return huh.NewOption(label, section.key)
	})

	return huh.NewSelect[string]().Key(sectionsKey).Title(s.title).Value(s.choice).Options(options...)
}

// updateTop opens the section under the cursor on enter, and hands every other message to the form.
func (s sectionedForm) updateTop(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || press.String() != "enter" {
		return m.updateForm(msg)
	}

	focused := m.form.form.GetFocusedField()
	if focused == nil || focused.GetKey() != sectionsKey {
		return m.updateForm(msg)
	}

	index := slices.IndexFunc(s.sections, func(section formSection) bool { return section.key == *s.choice })
	if index < 0 || s.sections[index].off() {
		return m, nil
	}

	cmd := m.openForm(s.sectionState(s.sections[index], drawnGray(m.form.form)))

	return m, cmd
}

func (s sectionedForm) sectionState(section formSection, top []string) formState {
	return formState{
		kind:           s.kind,
		keepsOnRefusal: true,
		update:         model.updateForm,
		submit:         s.submit,
		rebuild: func(m model) (*huh.Form, []string) {
			return formOf(formKeys(), section.fields(m)...)
		},
		editable: section.editable,
		leading: func(_, height int) []string {
			return []string{strings.Join(top[:min(len(top), max(height, 0))], "\n")}
		},
		back: s.backToTop,
	}
}

// backToTop closes the open section and puts the cursor back on the list of sections.
func (s sectionedForm) backToTop(m model) (tea.Model, tea.Cmd) {
	cmd := m.openForm(s.topState())

	return m, tea.Batch(append([]tea.Cmd{cmd}, m.focusFieldCmds(sectionsKey)...)...)
}

func (f formSection) off() bool {
	return f.isOff != nil && f.isOff()
}

// drawnGray is a form's lines as it stands, without its help, drawn gray.
func drawnGray(form *huh.Form) []string {
	view := form.WithShowHelp(false).View()

	lines := xslice.Map(strings.Split(ansi.Strip(view), "\n"), func(line string) string {
		return strings.TrimRight(line, " ")
	})
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	muted := paint.Panel.Style(paint.ToneMuted)

	return xslice.Map(lines, func(line string) string { return muted.Render(line) })
}
