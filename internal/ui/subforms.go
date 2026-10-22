package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/form"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// updateSubforms opens the subform under the cursor on enter, and hands every other message to
// the form.
func (m *model) updateSubforms(msg tea.Msg) tea.Cmd {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || press.String() != "enter" {
		return m.updateForm(msg)
	}

	focused := m.form.rendered.GetFocusedField()
	if focused == nil || focused.GetKey() != form.SubformsKey {
		return m.updateForm(msg)
	}

	key, ok := focused.GetValue().(string)
	index := slices.IndexFunc(m.form.Subforms, func(sub form.Subform) bool { return sub.Key == key })
	if !ok || index < 0 || m.form.Subforms[index].IsOff() {
		return nil
	}

	return m.openForm(subformState(m.form.Form, m.form.Subforms[index], renderedGray(m.form.rendered)))
}

// subformState is a subform on screen, the top form it was opened from rendered gray to its left.
func subformState(top form.Form, sub form.Subform, rendered []string) formState {
	state := openedFormState(form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			return form.Of(fixedKeys(), sub.Fields(in)...)
		},
		Submit:          top.Submit,
		Editable:        sub.Editable,
		Narrow:          top.Narrow,
		ClosesOnRefusal: top.ClosesOnRefusal,
	})
	state.leading = func(_, height int) []string {
		return []string{strings.Join(rendered[:min(len(rendered), max(height, 0))], "\n")}
	}
	state.back = func(m *model) tea.Cmd {
		cmd := m.openForm(openedFormState(top))

		return tea.Batch(append([]tea.Cmd{cmd}, m.focusFieldCmds(form.SubformsKey)...)...)
	}

	return state
}
