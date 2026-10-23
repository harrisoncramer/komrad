package documents

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
)

// nameLimit is the longest name the server takes for a document.
const nameLimit = 200

// formValues is what the new document form asks for.
type formValues struct {
	Name string
	Type string
}

// newForm puts up the form for a document written for an agent.
func (d *Screen) newForm(in screen.Inputs, agentID string) screen.Request {
	owner, types := agentID, in.Listing.DocumentTypeNames()
	if item, ok := in.Listing.AnyAgent(agentID); ok {
		owner = item.Agent.Name
	}
	values := &formValues{Type: defaultType(types)}

	return form.Open{Form: form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			return buildForm(values, in.Listing.DocumentTypeNames())
		},
		Submit: func(form.Inputs) screen.Request {
			name, typeName := strings.TrimSpace(values.Name), values.Type
			doing := "writing a document"
			if name != "" {
				doing = "writing " + name
			}

			return d.deps.Clients.Write(
				doing,
				fmt.Sprintf("Could not write a document for %s", owner),
				func(a api.Clients) error {
					if err := checkName(name); err != nil {
						return err
					}

					return a.CreateDocument(agentID, name, typeName, placeholder(typeName))
				},
			)
		},
	}}
}

func buildForm(values *formValues, types []string) (*huh.Form, []string) {
	options := xslice.Map(types, func(name string) huh.Option[string] {
		return huh.NewOption(name, name)
	})

	return form.Of(config.Fixed(),
		huh.NewInput().Key("name").Title("New document name").Value(&values.Name).Validate(checkName),
		huh.NewSelect[string]().Key("type").Title("Type").Options(options...).Value(&values.Type),
	)
}

func checkName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("a document needs a name")
	}
	if utf8.RuneCountInString(trimmed) > nameLimit {
		return fmt.Errorf("a document's name is at most %d characters", nameLimit)
	}

	return nil
}

// placeholder is the text a new document starts with.
func placeholder(typeName string) string {
	return fmt.Sprintf("Empty %s document.\n", typeName)
}

func defaultType(types []string) string {
	if slices.Contains(types, "RFC") || len(types) == 0 {
		return "RFC"
	}

	return types[0]
}

// pressNew puts up the form for a new document of the one agent the filter names.
func (d *Screen) pressNew(in screen.Inputs) screen.Request {
	agentID, ok := d.namedAgentID(in)
	if d.editing != nil || !ok {
		return nil
	}

	return d.newForm(in, agentID)
}

// deleteForm asks before it deletes a document.
func (d *Screen) deleteForm(item document) form.Form {
	built, confirmed := form.Confirm(
		fmt.Sprintf("Delete document %q?", item.name),
		"Komrad deletes every version of it, and agents can no longer read it.",
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
				fmt.Sprintf("Could not delete document %q", item.name),
				func(a api.Clients) error { return a.DeleteDocument(item.id) },
			)
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}
}
