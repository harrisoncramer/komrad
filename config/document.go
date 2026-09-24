package config

import (
	"fmt"
	"strings"

	"github.com/invopop/jsonschema"
)

// Section is one table of the config file, as the docs site describes it.
type Section struct {
	Path        string    `json:"path"`
	Description string    `json:"description"`
	Settings    []Setting `json:"settings"`
	Sections    []Section `json:"sections"`
}

// Setting is one key of the config file with its built-in default.
type Setting struct {
	Key         string `json:"key"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     string `json:"default"`
}

// Document describes the Config struct for the docs site, with dir naming the package source the
// comment extractor reads.
func Document(dir string) (Section, error) {
	schema, err := reflectSchema(dir)
	if err != nil {
		return Section{}, err
	}

	return documentSection(schema, nil)
}

func documentSection(schema *jsonschema.Schema, path []string) (Section, error) {
	section := Section{
		Path:        strings.Join(path, "."),
		Description: strings.TrimSpace(schema.Description),
		Settings:    []Setting{},
		Sections:    []Section{},
	}

	for name, child := range schema.Properties.FromOldest() {
		at := append(append([]string{}, path...), name)

		if child.Properties != nil {
			nested, err := documentSection(child, at)
			if err != nil {
				return Section{}, err
			}

			section.Sections = append(section.Sections, nested)

			continue
		}

		rendered, err := renderValue(child.Default)
		if err != nil {
			return Section{}, fmt.Errorf("failed to render %s: %w", strings.Join(at, "."), err)
		}

		section.Settings = append(section.Settings, Setting{
			Key:         name,
			Path:        strings.Join(at, "."),
			Type:        child.Type,
			Description: strings.TrimSpace(child.Description),
			Default:     rendered,
		})
	}

	return section, nil
}
