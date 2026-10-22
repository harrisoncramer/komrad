package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/internal/ui/form"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// documentNameLimit is the longest name the server takes for a document.
const documentNameLimit = 200

// documentFormValues is what the new document form asks for.
type documentFormValues struct {
	Name string
	Type string
}

// newDocumentForm puts up the form for a document written for an agent.
func newDocumentForm(m *model, agentID string) tea.Cmd {
	owner, types := agentID, m.listing.DocumentTypeNames()
	if item, ok := m.listing.AnyAgent(agentID); ok {
		owner = item.Agent.Name
	}
	values := &documentFormValues{Type: defaultDocumentType(types)}

	return m.apply(form.Open{Form: form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			return documentForm(values, in.Listing.DocumentTypeNames())
		},
		Submit: func(form.Inputs) screen.Request {
			name, typeName := strings.TrimSpace(values.Name), values.Type
			doing := "writing a document"
			if name != "" {
				doing = "writing " + name
			}

			return m.write(
				doing,
				fmt.Sprintf("Could not write a document for %s", owner),
				func(a api) error {
					if err := checkDocumentName(name); err != nil {
						return err
					}

					return a.createDocument(agentID, name, typeName, documentPlaceholder(typeName))
				},
			)
		},
	}})
}

func documentForm(values *documentFormValues, types []string) (*huh.Form, []string) {
	options := xslice.Map(types, func(name string) huh.Option[string] {
		return huh.NewOption(name, name)
	})

	return form.Of(fixedKeys(),
		huh.NewInput().Key("name").Title("New document name").Value(&values.Name).Validate(checkDocumentName),
		huh.NewSelect[string]().Key("type").Title("Type").Options(options...).Value(&values.Type),
	)
}

func checkDocumentName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("a document needs a name")
	}
	if utf8.RuneCountInString(trimmed) > documentNameLimit {
		return fmt.Errorf("a document's name is at most %d characters", documentNameLimit)
	}

	return nil
}

// documentPlaceholder is the text a new document starts with.
func documentPlaceholder(typeName string) string {
	return fmt.Sprintf("Empty %s document.\n", typeName)
}

func defaultDocumentType(types []string) string {
	if slices.Contains(types, "RFC") || len(types) == 0 {
		return "RFC"
	}

	return types[0]
}

// pressNew puts up the form for a new document of the one agent the filter names.
func (d *documentsScreen) pressNew(m *model) tea.Cmd {
	agentID, ok := m.namedAgentID()
	if d.editing != nil || !ok {
		return nil
	}

	return newDocumentForm(m, agentID)
}
