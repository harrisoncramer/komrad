package config

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

const preamble = `# weave configuration (github.com/harrisoncramer/weave).
#
# Generated from the Config struct in config. Do not edit by hand;
# change the struct's doc comments or DefaultKeybindings and run
# 'mise run gen:config'.
#
# These are the built-in defaults. Do not copy this file over: write
# ~/.config/weave/config.toml holding only what you are changing. An unknown key
# is an error.
`

// RenderConfigTOML builds the documented config file from the Config struct, with dir naming
// the package source the comment extractor reads.
func RenderConfigTOML(dir string) ([]byte, error) {
	schema, err := reflectSchema(dir)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(preamble)

	if err := renderSection(&buf, schema, nil); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// property is a schema property paired with the name it is keyed by, so the
// renderer can hold onto the ordering the reflector gave it.
type property struct {
	name   string
	schema *jsonschema.Schema
}

// renderSection writes one table and descends into its sub-tables, emitting a header only
// when the level has values of its own.
func renderSection(buf *bytes.Buffer, schema *jsonschema.Schema, path []string) error {
	var values, tables []property

	for name, child := range schema.Properties.FromOldest() {
		if child.Properties != nil {
			tables = append(tables, property{name: name, schema: child})

			continue
		}

		values = append(values, property{name: name, schema: child})
	}

	if len(path) == 0 {
		buf.WriteString("\n")
	} else if strings.TrimSpace(schema.Description) != "" || len(values) > 0 {
		buf.WriteString("\n")
		writeComment(buf, schema.Description)

		if len(values) > 0 {
			fmt.Fprintf(buf, "[%s]\n", strings.Join(path, "."))
		}
	}

	width := 0
	for _, value := range values {
		if len(value.name) > width {
			width = len(value.name)
		}
	}

	for _, value := range values {
		writeComment(buf, value.schema.Description)

		rendered, err := renderValue(value.schema.Default)
		if err != nil {
			return fmt.Errorf("failed to render %s: %w", strings.Join(append(path, value.name), "."), err)
		}

		// A value that runs to several lines opens on its own name rather than a
		// padded one, since there is no column of neighbours for it to line up with.
		if strings.Contains(rendered, "\n") {
			fmt.Fprintf(buf, "%s = %s\n", value.name, rendered)

			continue
		}

		fmt.Fprintf(buf, "%-*s = %s\n", width, value.name, rendered)
	}

	for _, table := range tables {
		if err := renderSection(buf, table.schema, append(path, table.name)); err != nil {
			return err
		}
	}

	return nil
}

// renderValue formats a default as TOML, failing on a kind the config does not hold rather
// than emitting something that will not parse.
func renderValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case float64:
		// 'f' rather than the shortest form, since a fraction written in exponent
		// notation is still a TOML float but not one a reader would recognize as the
		// share of a width.
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case []string:
		quoted := make([]string, 0, len(typed))
		for _, item := range typed {
			quoted = append(quoted, strconv.Quote(item))
		}

		return "[" + strings.Join(quoted, ", ") + "]", nil
	default:
		return "", fmt.Errorf("unsupported default type %T", value)
	}
}

// writeComment emits text as TOML comment lines, keeping the author's line breaks rather
// than re-flowing them to a width.
func writeComment(buf *bytes.Buffer, text string) {
	documented := strings.TrimSpace(text)
	if documented == "" {
		return
	}

	for _, line := range strings.Split(documented, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			buf.WriteString("#\n")

			continue
		}

		fmt.Fprintf(buf, "# %s\n", trimmed)
	}
}
