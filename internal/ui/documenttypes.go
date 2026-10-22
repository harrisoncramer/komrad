package ui

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

// documentTypeNameLimit is the longest name the server takes for a document type.
const documentTypeNameLimit = 100

// documentTypeFormValues is what the document type forms ask for.
type documentTypeFormValues struct {
	Name    string
	Context string
}

// newTypeForm is the form that defines a document type, asking its name and its context.
func (d *documentsScreen) newTypeForm() form.Form {
	values := &documentTypeFormValues{}

	return d.documentTypeFormOf(documentTypeFormOfParams{
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
func (d *documentsScreen) editTypeForm(t *agentv1.DocumentType) form.Form {
	values := &documentTypeFormValues{Name: t.GetName(), Context: t.GetContext()}

	return d.documentTypeFormOf(documentTypeFormOfParams{
		values:  values,
		title:   "Edit document type",
		doing:   "editing",
		refused: "Could not edit",
		write: func(a api.Clients) error {
			return a.WriteDocumentType(t.GetId(), t.GetVersion(), strings.TrimSpace(values.Name), values.Context)
		},
	})
}

// documentTypeFormOfParams is what a document type form is opened from.
type documentTypeFormOfParams struct {
	values  *documentTypeFormValues
	title   string
	doing   string
	refused string
	write   func(api.Clients) error
}

func (d *documentsScreen) documentTypeFormOf(params documentTypeFormOfParams) form.Form {
	values := params.values

	return form.Form{
		Build:    func(form.Inputs) (*huh.Form, []string) { return documentTypeForm(values, params.title, fixedKeys()) },
		Editable: []form.Editable{{Key: "context", Value: &values.Context}},
		Submit: func(form.Inputs) screen.Request {
			name, typeContext := strings.TrimSpace(values.Name), values.Context
			doing, summary := params.doing+" a document type", params.refused+" a document type"
			if name != "" {
				doing, summary = params.doing+" "+name, fmt.Sprintf("%s document type %q", params.refused, name)
			}

			return d.deps.clients.Write(
				doing,
				summary,
				func(a api.Clients) error {
					if err := errors.Join(checkDocumentTypeName(name), checkDocumentTypeContext(typeContext)); err != nil {
						return err
					}

					return params.write(a)
				},
			)
		},
	}
}

func documentTypeForm(values *documentTypeFormValues, title string, fixed config.FixedKeys) (*huh.Form, []string) {
	return form.Of(fixed,
		huh.NewInput().Key("name").Title(title).Value(&values.Name).Validate(checkDocumentTypeName),
		form.Text("context").Title("Context").
			Description(fmt.Sprintf("what an agent reads before writing one; %s opens $EDITOR", keys.Label(fixed.Editor))).
			Lines(6).Value(&values.Context).Validate(checkDocumentTypeContext),
	)
}

// deleteTypeForm asks before it deletes a document type.
func (d *documentsScreen) deleteTypeForm(item documentItem) form.Form {
	built, confirmed := form.Confirm(
		fmt.Sprintf("Delete document type %q?", item.name),
		"Agents can no longer write documents of this type. Komrad refuses the delete while any document still uses it.",
		"Delete",
		fixedKeys(),
	)

	return form.Form{
		Built: built,
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return d.deps.clients.Write(
				"deleting "+item.name,
				fmt.Sprintf("Could not delete document type %q", item.name),
				func(a api.Clients) error { return a.DeleteDocumentType(item.id) },
			)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}
}

func checkDocumentTypeName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("a document type needs a name")
	}
	if utf8.RuneCountInString(trimmed) > documentTypeNameLimit {
		return fmt.Errorf("a document type's name is at most %d characters", documentTypeNameLimit)
	}

	return nil
}

func checkDocumentTypeContext(typeContext string) error {
	if strings.TrimSpace(typeContext) == "" {
		return errors.New("a document type needs the context an agent reads")
	}

	return nil
}
