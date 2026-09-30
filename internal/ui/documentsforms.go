package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// documentNameLimit is the longest name the service takes for a document.
const documentNameLimit = 200

// documentFormValues is what the new document form asks for.
type documentFormValues struct {
	Name string
	Type string
}

// newDocumentForm puts up the form for a document written for an agent.
func newDocumentForm(h host, s panelShared, agentID string) tea.Cmd {
	owner, types := agentID, s.listing.documentTypeNames()
	if item, ok := s.listing.anyAgent(agentID); ok {
		owner = item.Agent.Name
	}
	values := &documentFormValues{Type: defaultDocumentType(types)}

	return h.openForm(redrawnForm(redrawnFormParams{
		Kind:  formDocument,
		Build: func(m model) (*huh.Form, []string) { return documentForm(values, m.listing.documentTypeNames()) },
		Submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if checkDocumentName(values.Name) != nil {
				return m, nil
			}

			name, typeName := strings.TrimSpace(values.Name), values.Type
			cmd := m.writeCmd(
				"writing "+name,
				fmt.Sprintf("Could not write a document for %s", owner),
				func(a api) error { return a.createDocument(agentID, name, typeName, documentPlaceholder(typeName)) },
			)

			return m, cmd
		}),
	}))
}

func documentForm(values *documentFormValues, types []string) (*huh.Form, []string) {
	options := xslice.Map(types, func(name string) huh.Option[string] {
		return huh.NewOption(name, name)
	})

	return formOf(formKeys(),
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
