package review

import (
	"komrad/config"
	"komrad/internal/ui/field"
	"komrad/internal/ui/form"
	"komrad/internal/ui/paint"
	"komrad/internal/vim"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// inputKey is the field's key inside its own one-field form.
const inputKey = "body"

// inputBar is the bar huh renders a field with and the space after it.
const inputBar = 2

// inputField is the panel's ordinary text field, rendered in the review's own sidebar rather than
// in the column every other form is given.
type inputField struct {
	form *huh.Form
	body *string
	// title and description are kept so the rows they wrap into can be measured again at every
	// width the field is given.
	title       string
	description string
	// vim is vim's state for the field, nil when vim is off.
	vim *field.Vim
}

// newInputFieldParams is what a field is opened with.
type newInputFieldParams struct {
	Title       string
	Description string
	Placeholder string
	// Body is what the field opens holding.
	Body  string
	Width int
	Keys  config.FixedKeys
	// Register is the panel's vim register, nil when vim is off.
	Register *vim.Register
}

// newInputField builds the field around the words it opens on.
func newInputField(params newInputFieldParams) (inputField, tea.Cmd) {
	body := new(string)
	*body = params.Body

	text := form.Text(inputKey).
		Title(params.Title).
		Description(params.Description).
		Placeholder(params.Placeholder).
		Lines(editorHeight).
		Value(body)

	form := huh.NewForm(huh.NewGroup(text)).
		WithShowHelp(false).
		WithKeyMap(inputKeyMap(params.Keys)).
		WithTheme(paint.Panel.FormTheme())

	input := inputField{
		form:        form,
		body:        body,
		title:       params.Title,
		description: params.Description,
		vim:         field.NewVim(params.Register),
	}

	return input, tea.Batch(form.Init(), input.resize(params.Width))
}

// resize tells the field how much of the sidebar it has.
func (i inputField) resize(width int) tea.Cmd {
	if i.form == nil {
		return nil
	}

	width = max(width, 1)

	_, cmd := i.form.Update(tea.WindowSizeMsg{
		Width:  width,
		Height: editorHeight + i.chromeRows(width),
	})

	return cmd
}

// chromeRows is how many rows the title and the hint under it take at one width.
func (i inputField) chromeRows(width int) int {
	text := max(width-inputBar, 1)

	return len(paint.WrapRows(i.title, text)) + len(paint.WrapRows(i.description, text))
}

// update hands a message to the field.
func (i inputField) update(msg tea.Msg) (inputField, tea.Cmd) {
	if i.form == nil {
		return i, nil
	}

	_, cmd := i.form.Update(msg)

	return i, cmd
}

// value is what has been typed into the field.
func (i inputField) value() string {
	if i.body == nil {
		return ""
	}

	return *i.body
}

// inputKeyMap leaves submitting to the composer around the field.
func inputKeyMap(keys config.FixedKeys) *huh.KeyMap {
	km := form.QuitKeyMap(keys)
	km.Text.NewLine = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"))
	km.Text.Next = key.NewBinding()
	km.Text.Prev = key.NewBinding()
	km.Text.Submit = key.NewBinding()
	km.Text.Editor = key.NewBinding()

	return km
}

// updateVim hands a key to vim, and is what the box does with it.
func (i inputField) updateVim(key tea.KeyPressMsg) vim.Outcome {
	if i.form == nil || i.vim == nil {
		return vim.Pass
	}

	focused := i.form.GetFocusedField()
	if focused == nil {
		return vim.Pass
	}

	outcome, ok := i.vim.Press(i.form, focused, key.String())
	if !ok {
		return vim.Pass
	}

	return outcome
}

// isInserting reports whether the field takes typed text rather than vim commands.
func (i inputField) isInserting() bool {
	if i.form == nil {
		return true
	}

	focused := i.form.GetFocusedField()

	return focused == nil || i.vim.IsInserting(focused.GetKey())
}
