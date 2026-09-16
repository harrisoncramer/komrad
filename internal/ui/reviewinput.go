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

// reviewInputBar is the bar huh draws a field with and the space after it.
const reviewInputBar = 2

// reviewInput is the panel's ordinary text field, drawn in the review's own sidebar rather than
// in the column every other form is given.
type reviewInput struct {
	form *huh.Form
	body *string
	// title and description are kept so the rows they wrap into can be measured again at every
	// width the field is given.
	title       string
	description string
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

	input := reviewInput{
		form:        form,
		body:        body,
		title:       params.Title,
		description: params.Description,
	}

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

	width = max(width, 1)

	_, cmd := i.form.Update(tea.WindowSizeMsg{
		Width:  width,
		Height: reviewEditorHeight + i.chromeRows(width),
	})

	return cmd
}

// Explainer: huh answers a height too small for what it holds by cutting the bottom off the
// words rather than by growing, and the title and the hint both wrap, so how many rows they take
// is measured at the width about to be sent rather than counted once.

// chromeRows is how many rows the title and the hint under it take at one width.
func (i reviewInput) chromeRows(width int) int {
	text := max(width-reviewInputBar, 1)

	return len(paint.WrapRows(i.title, text)) + len(paint.WrapRows(i.description, text))
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
