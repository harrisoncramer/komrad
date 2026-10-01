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

// topSummaryCols is how wide the top form is drawn beside an open section.
const topSummaryCols = 26

// sectionedForm is a form whose top fields are always drawn and whose other fields are kept in
// sections, each opened from the top form on its own.
type sectionedForm struct {
	kind  formKind
	title string
	// top is the fields drawn above the list of sections.
	top func(m model) []huh.Field
	// topSummary is the top fields as they are drawn gray beside an open section.
	topSummary  func() []summaryField
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

// summaryField is one top field as the gray top form draws it.
type summaryField struct {
	title string
	value string
}

// openSectionedForm puts up a sectioned form's top form, its cursor on the first field.
func openSectionedForm(h host, form sectionedForm) tea.Cmd {
	return h.openForm(form.topState())
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

	cmd := m.openForm(s.sectionState(s.sections[index]))

	return m, cmd
}

func (s sectionedForm) sectionState(section formSection) formState {
	return formState{
		kind:           s.kind,
		keepsOnRefusal: true,
		update:         model.updateForm,
		submit:         s.submit,
		rebuild: func(m model) (*huh.Form, []string) {
			return formOf(formKeys(), section.fields(m)...)
		},
		editable: section.editable,
		leading:  s.drawTopSummary,
		back:     s.backToTop,
	}
}

// backToTop closes the open section and puts the cursor back on the list of sections.
func (s sectionedForm) backToTop(m model) (tea.Model, tea.Cmd) {
	cmd := m.openForm(s.topState())

	cmds := []tea.Cmd{cmd}
	for range max(slices.Index(m.form.order, sectionsKey), 0) {
		cmds = append(cmds, m.form.form.NextField())
	}

	return m, tea.Batch(cmds...)
}

// drawTopSummary is the top form drawn gray to the left of an open section, or nothing when the
// section would be left too narrow.
func (s sectionedForm) drawTopSummary(width, height int) []string {
	if width-topSummaryCols-dividerCols < formColumnFloor {
		return nil
	}

	var lines []string
	for _, field := range s.topSummary() {
		value, _, _ := strings.Cut(field.value, "\n")
		lines = append(lines, field.title, "  "+value, "")
	}

	lines = append(lines, s.title)
	for _, section := range s.sections {
		marker := "  "
		if section.key == *s.choice {
			marker = "> "
		}
		lines = append(lines, marker+section.title+nestedMark)
	}

	muted := paint.Panel.Style(paint.ToneMuted)

	return []string{strings.Join(xslice.Map(lines[:min(len(lines), max(height, 0))], func(line string) string {
		return muted.Render(ansi.Truncate(line, topSummaryCols, "…"))
	}), "\n")}
}

func (f formSection) off() bool {
	return f.isOff != nil && f.isOff()
}
