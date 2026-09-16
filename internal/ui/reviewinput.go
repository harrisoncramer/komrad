package ui

import (
	"weave/config"
	"weave/internal/ui/paint"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// reviewInputKey is the field's key inside its own one-field form.
const reviewInputKey = "body"

// reviewInputChrome is the rows a field takes around the words themselves: its title, the hint
// under it, and the blank line the two are separated by.
const reviewInputChrome = 4

// reviewInput is the panel's ordinary text field, drawn in the review's own sidebar rather than
// in the column every other form is given.
type reviewInput struct {
	form *huh.Form
	body *string
}

// newReviewInputParams is what a field is opened with.
type newReviewInputParams struct {
	Title       string
	Description string
	Placeholder string
	// Body is what the field opens holding, which is what came back from an editor.
	Body  string
	Width int
	Keys  config.FormKeys
}

// newReviewInput builds the field around the words it opens on.
func newReviewInput(params newReviewInputParams) (reviewInput, tea.Cmd) {
	body := new(string)
	*body = params.Body

	field := textField(reviewInputKey).
		Title(params.Title).
		Description(params.Description).
		Placeholder(params.Placeholder).
		Lines(reviewEditorHeight).
		Value(body)

	form := huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(false).
		WithKeyMap(reviewInputKeyMap(params.Keys)).
		WithTheme(paint.FormTheme())

	input := reviewInput{form: form, body: body}

	return input, tea.Batch(form.Init(), input.resize(params.Width))
}

// Explainer: huh lays a field out from a window size and only while nothing was set at build
// time, so the width the sidebar has is sent as a resize rather than through WithWidth, which
// would make the field deaf to every later one.

// resize tells the field how much of the sidebar it has.
func (i reviewInput) resize(width int) tea.Cmd {
	if i.form == nil {
		return nil
	}

	_, cmd := i.form.Update(tea.WindowSizeMsg{
		Width:  max(width, 20),
		Height: reviewEditorHeight + reviewInputChrome,
	})

	return cmd
}

// update hands a message to the field.
func (i reviewInput) update(msg tea.Msg) (reviewInput, tea.Cmd) {
	if i.form == nil {
		return i, nil
	}

	_, cmd := i.form.Update(msg)

	return i, cmd
}

// view draws the field, and nothing at all where there is no field open.
func (i reviewInput) view() string {
	if i.form == nil {
		return ""
	}

	return i.form.View()
}

// value is what has been typed into the field.
func (i reviewInput) value() string {
	if i.body == nil {
		return ""
	}

	return *i.body
}

// reviewInputKeyMap keeps the return key inside the field, since the panel's own keys are what
// say the words are finished.
func reviewInputKeyMap(keys config.FormKeys) *huh.KeyMap {
	km := formKeyMap(keys)
	km.Text.NewLine = key.NewBinding(key.WithKeys("enter", "ctrl+j", "alt+enter"))
	km.Text.Next = key.NewBinding()
	km.Text.Prev = key.NewBinding()
	km.Text.Submit = key.NewBinding()
	km.Text.Editor = key.NewBinding()

	return km
}
