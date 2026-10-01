package ui

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"komrad/config"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// documentTypeNameLimit is the longest name the service takes for a document type.
const documentTypeNameLimit = 100

// documentTypeFormValues is what the document type forms ask for.
type documentTypeFormValues struct {
	Name    string
	Context string
}

// newTypeForm is the form that defines a document type, asking its name and its context.
func newTypeForm() formState {
	values := &documentTypeFormValues{}

	return documentTypeFormState(documentTypeFormStateParams{
		values:  values,
		title:   "New document type",
		doing:   "defining",
		refused: "Could not define",
		write: func(a api) error {
			return a.createDocumentType(strings.TrimSpace(values.Name), values.Context)
		},
	})
}

// editTypeForm is the form that writes a document type's name and context over the ones it has.
func editTypeForm(t *agentv1.DocumentType) formState {
	values := &documentTypeFormValues{Name: t.GetName(), Context: t.GetContext()}

	return documentTypeFormState(documentTypeFormStateParams{
		values:  values,
		title:   "Edit document type",
		doing:   "editing",
		refused: "Could not edit",
		write: func(a api) error {
			return a.writeDocumentType(documentTypeItem(t), t.GetVersion(), strings.TrimSpace(values.Name), values.Context)
		},
	})
}

// documentTypeFormStateParams is what a document type form is opened from.
type documentTypeFormStateParams struct {
	values  *documentTypeFormValues
	title   string
	doing   string
	refused string
	write   func(api) error
}

func documentTypeFormState(params documentTypeFormStateParams) formState {
	values := params.values

	return redrawnForm(redrawnFormParams{
		Kind:     formDocumentType,
		Build:    func(model) (*huh.Form, []string) { return documentTypeForm(values, params.title, formKeys()) },
		Editable: []editableText{{key: "context", value: &values.Context}},
		Submit: holdSubmit(func(m model) (model, tea.Cmd) {
			name, typeContext := strings.TrimSpace(values.Name), values.Context
			doing, summary := params.doing+" a document type", params.refused+" a document type"
			if name != "" {
				doing, summary = params.doing+" "+name, fmt.Sprintf("%s document type %q", params.refused, name)
			}
			cmd := m.writeCmd(
				doing,
				summary,
				func(a api) error {
					if err := errors.Join(checkDocumentTypeName(name), checkDocumentTypeContext(typeContext)); err != nil {
						return err
					}

					return params.write(a)
				},
			)

			return m, cmd
		}),
	})
}

func documentTypeForm(values *documentTypeFormValues, title string, keys config.FormKeys) (*huh.Form, []string) {
	return formOf(keys,
		huh.NewInput().Key("name").Title(title).Value(&values.Name).Validate(checkDocumentTypeName),
		textField("context").Title("Context").
			Description(fmt.Sprintf("what an agent reads before writing one; %s opens $EDITOR", keyLabel(keys.Editor))).
			Lines(6).Value(&values.Context).Validate(checkDocumentTypeContext),
	)
}

// deleteTypeForm asks before a document type is deleted.
func deleteTypeForm(item documentItem) formState {
	form, confirmed := confirmForm(
		fmt.Sprintf("Delete document type %q?", item.name),
		"Agents can no longer write documents of this type. A type documents are still written as is not deleted.",
		"Delete",
		formKeys(),
	)

	return formState{
		kind:   formDocumentTypeDelete,
		form:   form,
		update: model.updateForm,
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				"deleting "+item.name,
				fmt.Sprintf("Could not delete document type %q", item.name),
				func(a api) error { return a.deleteDocumentType(item.id) },
			)
		}),
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
