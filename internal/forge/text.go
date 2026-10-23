package forge

import (
	"komrad/lib/protoenum"
	forgev1 "komrad/proto/gen/forge/v1"
)

// Text is a pull request's title and description.
type Text struct {
	Title string
	Body  string
}

// EditParams is the pull request to change, the text the change was made from, and what to change.
type EditParams struct {
	URL string
	// EditedFrom is the text the caller read, which the forge refuses to change once it has moved.
	EditedFrom Text
	// Title is the new title, nil to leave it.
	Title *string
	// Body is the new description, nil to leave it.
	Body *string
}

// TextTarget is what a forge read about a pull request whose text it is about to read or change.
type TextTarget struct {
	// Forge names the forge in a refusal.
	Forge forgev1.Kind
	// Ref names the pull request in a refusal, such as #12 or !12.
	Ref     string
	Text    Text
	CanEdit bool
}

// Refusal is why the forge will not let the credential change the text, nil when it will.
func (t TextTarget) Refusal() error {
	if t.CanEdit {
		return nil
	}

	return ErrNotEditable.Saying("%s will not let the credential change %s", protoenum.Name(t.Forge), t.Ref)
}

// Changes refuses an edit the credential may not make or one made from text that has since moved,
// and says whether the edit changes anything.
func (p EditParams) Changes(target TextTarget) (bool, error) {
	if err := target.Refusal(); err != nil {
		return false, err
	}

	retitled := p.Title != nil && *p.Title != target.Text.Title
	redescribed := p.Body != nil && *p.Body != target.Text.Body

	if retitled && target.Text.Title != p.EditedFrom.Title {
		return false, ErrTextMoved.Saying("the title of %s changed on %s since it was read", target.Ref, protoenum.Name(target.Forge))
	}
	if redescribed && target.Text.Body != p.EditedFrom.Body {
		return false, ErrTextMoved.Saying("the description of %s changed on %s since it was read", target.Ref, protoenum.Name(target.Forge))
	}

	return retitled || redescribed, nil
}
