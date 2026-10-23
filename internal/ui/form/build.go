package form

import (
	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Of renders a group of fields as one form, and lists the keys of the fields the cursor can
// reach, in the order they are rendered.
func Of(fixed config.FixedKeys, fields ...huh.Field) (*huh.Form, []string) {
	return titledOf(fixed, "", fields...)
}

// titledOf is Of under a title that heads every field.
func titledOf(fixed config.FixedKeys, title string, fields ...huh.Field) (*huh.Form, []string) {
	form := huh.NewForm(huh.NewGroup(fields...).Title(title)).
		WithShowHelp(true).
		WithKeyMap(KeyMap(fixed)).
		WithTheme(paint.Panel.FormTheme())

	reached := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Skip() {
			continue
		}

		reached = append(reached, field.GetKey())
	}

	return form, reached
}

// Text is a text field whose editor the panel opens rather than huh.
func Text(key string) *huh.Text {
	return huh.NewText().Key(key).ExternalEditor(false)
}

// QuitKeyMap is huh's own keys with the panel's back and quit keys leaving the form.
func QuitKeyMap(fixed config.FixedKeys) *huh.KeyMap {
	quit := append(append([]string{}, fixed.Back...), fixed.Quit...)

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys(quit...), key.WithHelp(keys.Label(fixed.Back), "back"))

	return km
}

// KeyMap is the keys of a form of several fields, tab stepping between them and the submit key
// answering the form from any of them.
func KeyMap(fixed config.FixedKeys) *huh.KeyMap {
	submit := key.NewBinding(key.WithKeys(fixed.Submit...), key.WithHelp(keys.Label(fixed.Submit), "submit"))

	next := key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next"))

	km := QuitKeyMap(fixed)
	km.Input.Next = next
	km.Input.Submit = submit
	km.Text.Next = next
	km.Text.NewLine = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"), key.WithHelp("ctrl+j", "new line"))
	km.Text.Submit = submit
	km.Select.Next = next
	km.Select.Submit = submit
	km.Confirm.Next = next
	km.Confirm.Submit = submit

	return km
}

// Confirm asks to go ahead or cancel, starting on going ahead, and hands back where it writes the answer.
func Confirm(title, description, affirmative string, fixed config.FixedKeys) (*huh.Form, *bool) {
	confirmed := true
	form, _ := Of(fixed, confirmField(&confirmed, description, affirmative).Title(title))

	return form, &confirmed
}

// confirmField asks to go ahead or cancel, writing the answer to answer.
func confirmField(answer *bool, description, affirmative string) *huh.Confirm {
	return huh.NewConfirm().
		Key("confirm").
		Description(description).
		Affirmative(affirmative).
		Negative("Cancel").
		WithButtonAlignment(lipgloss.Left).
		Value(answer)
}
