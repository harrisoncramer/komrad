package ui

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// documentTypeNameLimit is the longest name the service takes for a document type.
const documentTypeNameLimit = 100

// documentTypeFormValues is what the new document type form asks for.
type documentTypeFormValues struct {
	Name    string
	Context string
}

// back leaves for the screen the shelf was opened from.
func (d *documentsScreen) back(h navHost, s panelShared) tea.Cmd {
	if d.shelf == shelfDocumentTypes {
		if _, ok := s.listing.liveAgent(d.returning.agentID); d.from == screenReview && !ok {
			return h.enter(screenAgents, screenTarget{})
		}

		return h.enter(d.from, d.returning)
	}

	return h.enter(d.from, screenTarget{agentID: d.agentID})
}

func (d *documentsScreen) newDesc() string {
	if d.shelf == shelfDocumentTypes {
		return "new type"
	}

	return "new document"
}

// pressNew puts up the form for a new document, or for a new type.
func (d *documentsScreen) pressNew(h host, s panelShared) tea.Cmd {
	if d.editing != nil {
		return nil
	}

	if d.shelf == shelfDocumentTypes {
		return h.openForm(newTypeForm())
	}

	return newDocumentForm(h, s, d.agentID)
}

// newTypeForm is the form that defines a document type, asking its name and its context.
func newTypeForm() formState {
	values := &documentTypeFormValues{}

	return redrawnForm(redrawnFormParams{
		Kind:     formDocumentType,
		Build:    func(model) (*huh.Form, []string) { return documentTypeForm(values, formKeys()) },
		Editable: []editableText{{key: "context", value: &values.Context}},
		Submit: holdSubmit(func(m model) (model, tea.Cmd) {
			name, typeContext := strings.TrimSpace(values.Name), values.Context
			doing, summary := "defining a document type", "Could not define a document type"
			if name != "" {
				doing, summary = "defining "+name, fmt.Sprintf("Could not define document type %q", name)
			}
			cmd := m.writeCmd(
				doing,
				summary,
				func(a api) error {
					if err := errors.Join(checkDocumentTypeName(name), checkDocumentTypeContext(typeContext)); err != nil {
						return err
					}

					return a.createDocumentType(name, typeContext)
				},
			)

			return m, cmd
		}),
	})
}

func documentTypeForm(values *documentTypeFormValues, keys config.FormKeys) (*huh.Form, []string) {
	return formOf(keys,
		huh.NewInput().Key("name").Title("New document type").Value(&values.Name).Validate(checkDocumentTypeName),
		textField("context").Title("Context").
			Description(fmt.Sprintf("what an agent reads before writing one; %s opens $EDITOR", keyLabel(keys.Editor))).
			Lines(6).Value(&values.Context).Validate(checkDocumentTypeContext),
	)
}

// confirmDeleteType asks before the type under the cursor is deleted.
func (d *documentsScreen) confirmDeleteType(h host, s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	return h.openForm(deleteTypeForm(item))
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
