package form

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
)

// SubformsKey is the key of the field listing a form's subforms.
const SubformsKey = "subforms"

// Inputs is what the panel hands a form each time it builds or answers it.
type Inputs struct {
	Listing panel.Listing
	// WorktreeInspections is what git last answered for each worktree directory.
	WorktreeInspections map[string]panel.WorktreeInspection
}

// Form is a question the panel puts over the screen, and what answering it does.
type Form struct {
	// Prebuilt is the form as first rendered, nil to render it with Build.
	Prebuilt *huh.Form
	// Build renders the form around the values its fields write through, and lists the keys of
	// the fields the cursor can reach in the order they are rendered.
	Build func(Inputs) (*huh.Form, []string)
	// Submit is what the answered form does, nil when it does nothing and closes.
	Submit func(Inputs) screen.Request
	// DoesAnswerReshape reports whether an answer has changed which fields the form renders.
	DoesAnswerReshape func() bool
	// Editable are the text fields the panel's editor key can open.
	Editable []Editable
	// Subforms are the sets of fields the list under the top fields opens, each on its own.
	Subforms []Subform
	// CanUseNarrowestWidth says the form may shrink to the narrowest column width.
	CanUseNarrowestWidth bool
	// ClosesOnRefusal says a refused write takes the form down rather than leaving it to be fixed.
	ClosesOnRefusal bool
	// DescribedWorktrees are the directories the form describes, and it renders again as each is read.
	DescribedWorktrees []string
}

// Editable is a form's text field and the string the panel renders it from.
type Editable struct {
	Key   string
	Value *string
}

// Subform is a set of fields a form opens from the list under its top fields.
type Subform struct {
	Key   string
	Title string
	// IsRefused says enter refuses the subform, which has nothing to ask, nil for never.
	IsRefused func() bool
	Fields    func(Inputs) []huh.Field
	Editable  []Editable
}

// IsOff reports whether the subform has nothing to ask.
func (s Subform) IsOff() bool {
	return s.IsRefused != nil && s.IsRefused()
}

// SubformList is the field listing the subforms, which a form with subforms puts under its top fields.
func SubformList(title string, subforms []Subform, choice *string) huh.Field {
	muted := paint.Panel.Style(paint.ToneMuted)
	options := xslice.Map(subforms, func(sub Subform) huh.Option[string] {
		label := sub.Title + " " + glyph.Crumb
		if sub.IsOff() {
			label = muted.Render(label)
		}

		return huh.NewOption(label, sub.Key)
	})

	return huh.NewSelect[string]().Key(SubformsKey).Title(title).Value(choice).Options(options...)
}

// Open puts a form up in place of whatever form or menu is up.
type Open struct {
	Form Form
}

// Request marks Open as a screen request.
func (Open) Request() {}

// Refuse leaves the form up with why its answer was refused.
type Refuse struct {
	Err error
	// ReturnToField is the key of the field the cursor goes back to, empty to leave it where it is.
	ReturnToField string
}

// Request marks Refuse as a screen request.
func (Refuse) Request() {}

// Close takes the form or menu up down and comes back to the screen it was rendered over.
type Close struct{}

// Request marks Close as a screen request.
func (Close) Request() {}
