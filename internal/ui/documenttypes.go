package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// documentTypeNameLimit is the longest name the service takes for a document type.
const documentTypeNameLimit = 100

var errDocumentTypesHere = errors.New("the document types open from a view or from the documents")

// documentTypeFormValues is what the new document type form asks for.
type documentTypeFormValues struct {
	Name    string
	Context string
}

// shelfActions is the keys only one shelf has: the way to the types from the documents, and
// renaming and deleting a type.
func (d *documentsScreen) shelfActions() []screenAction {
	if d.shelf != shelfDocumentTypes {
		return []screenAction{
			{
				id:   actionDocumentsTypes,
				keys: func(k config.Keybindings) []string { return k.Documents.Types },
				run: func(h host, _ panelShared, _ string) tea.Cmd {
					return h.enter(screenDocumentTypes, screenTarget{agentID: d.agentID, from: screenDocuments, under: d.from})
				},
			},
		}
	}

	return []screenAction{
		{
			id:   actionDocumentTypesRename,
			keys: func(k config.Keybindings) []string { return k.DocumentTypes.Rename },
			run:  func(h host, s panelShared, _ string) tea.Cmd { return d.renameTypeForm(h, s) },
		},
		{
			id:   actionDocumentTypesDelete,
			keys: func(k config.Keybindings) []string { return k.DocumentTypes.Delete },
			run:  func(h host, s panelShared, _ string) tea.Cmd { return d.confirmDeleteType(h, s) },
		},
	}
}

func (d *documentsScreen) shelfLegend() []screenLegendRow {
	if d.shelf != shelfDocumentTypes {
		return []screenLegendRow{
			{
				of:   []actionID{actionDocumentsTypes},
				desc: func(panelShared) string { return "document types" },
			},
		}
	}

	return []screenLegendRow{
		{
			of:   []actionID{actionDocumentTypesRename},
			desc: func(panelShared) string { return "rename" },
		},
		{
			of:   []actionID{actionDocumentTypesDelete},
			desc: func(panelShared) string { return "delete" },
		},
	}
}

// back leaves for the screen the shelf was opened from, and back to the documents it was opened
// over for the types.
func (d *documentsScreen) back(h host) tea.Cmd {
	if d.shelf == shelfDocumentTypes {
		return h.enter(d.from, screenTarget{agentID: d.agentID, from: screenDocumentTypes})
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
	if d.shelf == shelfDocumentTypes {
		return d.newTypeForm(h)
	}

	return d.newDocumentForm(h, s)
}

// newTypeForm puts up the form for a document type, asking its name and its context.
func (d *documentsScreen) newTypeForm(h host) tea.Cmd {
	if d.editing != nil {
		return nil
	}

	values := &documentTypeFormValues{}

	return h.openForm(redrawnForm(redrawnFormParams{
		Kind:     formDocumentType,
		Build:    func(model) (*huh.Form, []string) { return documentTypeForm(values, formKeys()) },
		Editable: []editableText{{key: "context", value: &values.Context}},
		Submit: func(m model) (tea.Model, tea.Cmd) {
			if checkDocumentTypeName(values.Name) != nil || checkDocumentTypeContext(values.Context) != nil {
				return m.closeForm()
			}

			name, typeContext := strings.TrimSpace(values.Name), values.Context
			cmd := m.writeCmd(
				fmt.Sprintf("Could not define document type %q", name),
				func(a api) error { return a.createDocumentType(name, typeContext) },
			)

			return m, tea.Batch(cmd, m.holdForm())
		},
	}))
}

func documentTypeForm(values *documentTypeFormValues, keys config.FormKeys) (*huh.Form, []string) {
	return formOf(keys,
		huh.NewInput().Key("name").Title("New document type").Value(&values.Name).Validate(checkDocumentTypeName),
		textField("context").Title("Context").
			Description(fmt.Sprintf("what an agent reads before writing one; %s opens $EDITOR", keyLabel(keys.Editor))).
			Lines(6).Value(&values.Context).Validate(checkDocumentTypeContext),
	)
}

// renameTypeForm puts up the form renaming the type under the cursor.
func (d *documentsScreen) renameTypeForm(h host, s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	read, _ := d.bodyOf(s, item.id)
	name := item.name

	return h.openForm(redrawnForm(redrawnFormParams{
		Kind: formDocumentType,
		Build: func(model) (*huh.Form, []string) {
			return formOf(formKeys(),
				huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q", item.name)).Value(&name).Validate(checkDocumentTypeName),
			)
		},
		Submit: func(m model) (tea.Model, tea.Cmd) {
			renamed := strings.TrimSpace(name)
			if checkDocumentTypeName(renamed) != nil || renamed == item.name {
				return m.closeForm()
			}

			cmd := m.writeCmd(
				fmt.Sprintf("Could not rename document type %q", item.name),
				func(a api) error { return a.writeDocumentType(item, read.version, renamed, read.body) },
			)

			return m, tea.Batch(cmd, m.holdForm())
		},
	}))
}

// confirmDeleteType asks before the type under the cursor is deleted.
func (d *documentsScreen) confirmDeleteType(h host, s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	form, confirmed := confirmForm(
		fmt.Sprintf("Delete document type %q?", item.name),
		"Agents can no longer write documents of this type. A type documents are still written as is not deleted.",
		"Delete",
		formKeys(),
	)

	return h.openForm(formState{
		kind:   formDocumentTypeDelete,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				fmt.Sprintf("Could not delete document type %q", item.name),
				func(a api) error { return a.deleteDocumentType(item.id) },
			)
		}),
	})
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

// isView reports whether a screen is one of the views on the switcher.
func (s screen) isView() bool {
	return slices.ContainsFunc(viewEntries(), func(entry viewEntry) bool { return entry.screen == s })
}

// documentTypesTarget is where the document types open from the screen that is up, and false on
// a screen they do not open from.
func (m *model) documentTypesTarget() (screenTarget, bool) {
	switch {
	case m.screen == screenDocuments:
		return screenTarget{agentID: m.documents.agentID, from: screenDocuments, under: m.documents.from}, true
	case m.screen.isView():
		return screenTarget{agentID: m.hoveredAgentID(), from: m.screen, under: m.screen}, true
	}

	return screenTarget{}, false
}
