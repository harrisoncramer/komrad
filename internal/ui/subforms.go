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

// subformsKey is the form key of the list of subforms on a form of subforms' top form.
const subformsKey = "subforms"

// formOfSubforms is a form whose top fields are always rendered and whose other fields are kept in
// subforms, each opened from the top form on its own.
type formOfSubforms struct {
	kind  formKind
	title string
	// top is the fields rendered above the list of subforms.
	top         func(m *model) []huh.Field
	topEditable []editableText
	// reshaped reports whether an answer on the top form has changed which subforms are off.
	reshaped func() bool
	subforms []subform
	// choice is the subform the list's cursor is on.
	choice *string
	submit func(*model) tea.Cmd
}

// subform is a set of fields a form of subforms opens from its top form.
type subform struct {
	key   string
	title string
	// isOff says the subform has nothing to ask, nil for never.
	isOff    func() bool
	fields   func(m *model) []huh.Field
	editable []editableText
}

func (s formOfSubforms) topState() formState {
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

func (s formOfSubforms) buildTop(m *model) (*huh.Form, []string) {
	return formOf(fixedKeys(), append(s.top(m), s.subformsField())...)
}

func (s formOfSubforms) subformsField() huh.Field {
	muted := paint.Panel.Style(paint.ToneMuted)

	options := xslice.Map(s.subforms, func(sub subform) huh.Option[string] {
		label := sub.title + nestedMark
		if sub.off() {
			label = muted.Render(label)
		}

		return huh.NewOption(label, sub.key)
	})

	return huh.NewSelect[string]().Key(subformsKey).Title(s.title).Value(s.choice).Options(options...)
}

// updateTop opens the subform under the cursor on enter, and hands every other message to the form.
func (s formOfSubforms) updateTop(m *model, msg tea.Msg) tea.Cmd {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || press.String() != "enter" {
		return m.updateForm(msg)
	}

	focused := m.form.form.GetFocusedField()
	if focused == nil || focused.GetKey() != subformsKey {
		return m.updateForm(msg)
	}

	index := slices.IndexFunc(s.subforms, func(sub subform) bool { return sub.key == *s.choice })
	if index < 0 || s.subforms[index].off() {
		return nil
	}

	return m.openForm(s.subformState(s.subforms[index], renderedGray(m.form.form)))
}

func (s formOfSubforms) subformState(sub subform, top []string) formState {
	return formState{
		kind:           s.kind,
		keepsOnRefusal: true,
		update:         (*model).updateForm,
		submit:         s.submit,
		rebuild: func(m *model) (*huh.Form, []string) {
			return formOf(fixedKeys(), sub.fields(m)...)
		},
		editable: sub.editable,
		leading: func(_, height int) []string {
			return []string{strings.Join(top[:min(len(top), max(height, 0))], "\n")}
		},
		back: s.backToTop,
	}
}

// backToTop closes the open subform and puts the cursor back on the list of subforms.
func (s formOfSubforms) backToTop(m *model) tea.Cmd {
	cmd := m.openForm(s.topState())

	return tea.Batch(append([]tea.Cmd{cmd}, m.focusFieldCmds(subformsKey)...)...)
}

func (f subform) off() bool {
	return f.isOff != nil && f.isOff()
}

// renderedGray is a form's lines as it stands, without its help, rendered gray.
func renderedGray(form *huh.Form) []string {
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
