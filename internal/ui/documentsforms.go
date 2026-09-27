package ui

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// documentNameLimit is the longest name the service takes for a document.
const documentNameLimit = 200

// documentFormValues is what the new document form asks for.
type documentFormValues struct {
	Name string
	Kind agentv1.DocumentKind
}

// newDocumentForm puts up the form for a document written for the agent whose documents are
// listed.
func (d *documentsScreen) newDocumentForm(h host, s panelShared) tea.Cmd {
	if d.editing != nil {
		return nil
	}

	agentID, owner := d.agentID, d.agentName(s)
	values := &documentFormValues{Kind: agentv1.DocumentKind_DOCUMENT_KIND_RFC}

	return h.openForm(redrawnForm(redrawnFormParams{
		Kind:  formDocument,
		Build: func(model) (*huh.Form, []string) { return documentForm(values) },
		Submit: func(m model) (tea.Model, tea.Cmd) {
			if checkDocumentName(values.Name) != nil {
				return m.closeForm()
			}

			name, kind := strings.TrimSpace(values.Name), values.Kind
			cmd := m.writeCmd(
				fmt.Sprintf("Could not write a document for %s", owner),
				func(a api) error { return a.createDocument(agentID, name, kind, documentPlaceholder(kind)) },
			)

			return m, tea.Batch(cmd, m.holdForm())
		},
	}))
}

func documentForm(values *documentFormValues) (*huh.Form, []string) {
	kinds := []agentv1.DocumentKind{
		agentv1.DocumentKind_DOCUMENT_KIND_RFC,
		agentv1.DocumentKind_DOCUMENT_KIND_REPORT,
		agentv1.DocumentKind_DOCUMENT_KIND_EXPLAINER,
		agentv1.DocumentKind_DOCUMENT_KIND_NOTES,
	}
	options := xslice.Map(kinds, func(kind agentv1.DocumentKind) huh.Option[agentv1.DocumentKind] {
		return huh.NewOption(documentKindLabel(kind), kind)
	})

	return formOf(formKeys(),
		huh.NewInput().Key("name").Title("New document name").Value(&values.Name).Validate(checkDocumentName),
		huh.NewSelect[agentv1.DocumentKind]().Key("kind").Title("Kind").Options(options...).Value(&values.Kind),
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

// documentPlaceholder is the text a new document starts with, since a document is never empty.
func documentPlaceholder(kind agentv1.DocumentKind) string {
	return fmt.Sprintf("Empty %s document.\n", documentKindLabel(kind))
}
