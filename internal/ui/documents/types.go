package documents

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/huh/v2"
)

// typeNameLimit is the longest name the server takes for a document type.
const typeNameLimit = 100

// typeFormValues is what the document type forms ask for.
type typeFormValues struct {
	Name    string
	Context string
}

// newTypeForm is the form that defines a document type, asking its name and its context.
func (d *Screen) newTypeForm() form.Form {
	values := &typeFormValues{}

	return d.typeFormOf(typeFormOfParams{
		values:  values,
		title:   "New document type",
		doing:   "defining",
		refused: "Could not define",
		write: func(a api.Clients) error {
			return a.CreateDocumentType(strings.TrimSpace(values.Name), values.Context)
		},
	})
}

// editTypeForm is the form that writes a document type's name and context over the ones it has.
func (d *Screen) editTypeForm(t *agentv1.DocumentType) form.Form {
	values := &typeFormValues{Name: t.GetName(), Context: t.GetContext()}

	return d.typeFormOf(typeFormOfParams{
		values:  values,
		title:   "Edit document type",
		doing:   "editing",
		refused: "Could not edit",
		write: func(a api.Clients) error {
			return a.WriteDocumentType(api.WriteDocumentTypeParams{
				ID:      t.GetId(),
				Version: t.GetVersion(),
				Name:    strings.TrimSpace(values.Name),
				Context: values.Context,
			})
		},
	})
}

// typeFormOfParams is what a document type form is opened from.
type typeFormOfParams struct {
	values  *typeFormValues
	title   string
	doing   string
	refused string
	write   func(api.Clients) error
}

func (d *Screen) typeFormOf(params typeFormOfParams) form.Form {
	values := params.values

	return form.Form{
		Build:    func(form.Inputs) (*huh.Form, []string) { return typeForm(values, params.title, config.Fixed()) },
		Editable: []form.Editable{{Key: "context", Value: &values.Context}},
		Submit: func(form.Inputs) screen.Request {
			name, typeContext := strings.TrimSpace(values.Name), values.Context
			doing, summary := params.doing+" a document type", params.refused+" a document type"
			if name != "" {
				doing, summary = params.doing+" "+name, fmt.Sprintf("%s document type %q", params.refused, name)
			}

			return d.deps.Clients.Write(
				doing,
				summary,
				func(a api.Clients) error {
					if err := errors.Join(checkTypeName(name), checkTypeContext(typeContext)); err != nil {
						return err
					}

					return params.write(a)
				},
			)
		},
	}
}

func typeForm(values *typeFormValues, title string, fixed config.FixedKeys) (*huh.Form, []string) {
	return form.Of(fixed,
		huh.NewInput().Key("name").Title(title).Value(&values.Name).Validate(checkTypeName),
		form.Text("context").Title("Context").
			Description(fmt.Sprintf("what an agent reads before writing one; %s opens $EDITOR", keys.Label(fixed.Editor))).
			Lines(6).Value(&values.Context).Validate(checkTypeContext),
	)
}

// deleteTypeForm asks before it deletes a document type.
func (d *Screen) deleteTypeForm(item document) form.Form {
	built, confirmed := form.Confirm(
		fmt.Sprintf("Delete document type %q?", item.name),
		"Agents can no longer write documents of this type. Komrad refuses the delete while any document still uses it.",
		"Delete",
		config.Fixed(),
	)

	return form.Form{
		Built: built,
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return d.deps.Clients.Write(
				"deleting "+item.name,
				fmt.Sprintf("Could not delete document type %q", item.name),
				func(a api.Clients) error { return a.DeleteDocumentType(item.id) },
			)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}
}

func checkTypeName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("a document type needs a name")
	}
	if utf8.RuneCountInString(trimmed) > typeNameLimit {
		return fmt.Errorf("a document type's name is at most %d characters", typeNameLimit)
	}

	return nil
}

func checkTypeContext(typeContext string) error {
	if strings.TrimSpace(typeContext) == "" {
		return errors.New("a document type needs the context an agent reads")
	}

	return nil
}
