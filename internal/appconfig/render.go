package appconfig

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
)

// commentTextWidth is how wide a comment's prose is allowed to run, before the
// "# " prefix. Descriptions are re-flowed to it rather than copied verbatim, so
// rewrapping a doc comment in the source does not churn the generated file.
const commentTextWidth = 76

const preamble = `# claude-agents configuration (github.com/harrisoncramer/tmux-claude-agents).
#
# Generated from the Config struct in internal/appconfig. Do not edit by hand;
# change the struct's doc comments or DefaultKeybindings and run
# 'mise run gen:config'.
#
# Every value below is the tool's built-in default, spelled out. Copy this file
# to ~/.config/claude-agents/config.toml and change what you like; anything you
# delete falls back to the same default shown here. An unknown key is an error,
# so nothing outside this file is configurable.
`

// RenderConfigTOML builds the documented config file from the Config struct. dir
// is the directory holding this package's source, which the comment extractor
// parses; callers running from the package directory pass ".".
func RenderConfigTOML(dir string) ([]byte, error) {
	schema, err := Schema(dir)
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

// renderSection writes one table and then descends into its sub-tables. A table
// header is only emitted when the level has values of its own, so a struct that
// exists purely to group other structs contributes its prose without an empty
// [header] above it.
func renderSection(buf *bytes.Buffer, schema *jsonschema.Schema, path []string) error {
	var values, tables []property

	for name, child := range schema.Properties.FromOldest() {
		if child.Properties != nil {
			tables = append(tables, property{name: name, schema: child})

			continue
		}

		values = append(values, property{name: name, schema: child})
	}

	buf.WriteString("\n")

	if len(path) > 0 {
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

// renderValue formats a default as TOML. Only the kinds the config actually
// holds are supported, so a new field of some other type fails generation rather
// than emitting something that will not parse.
func renderValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed), nil
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
	case []Mark:
		// One inline table per line rather than a [[marks]] block each, so the marks
		// stay one value in a file whose every other value is one line too. A reader
		// who prefers the block form can write it and it parses the same.
		var buf strings.Builder
		buf.WriteString("[\n")
		for _, mark := range typed {
			fmt.Fprintf(&buf, "  { glyph = %s, color = %s },\n", strconv.Quote(mark.Glyph), strconv.Quote(mark.Color))
		}
		buf.WriteString("]")

		return buf.String(), nil
	default:
		return "", fmt.Errorf("unsupported default type %T", value)
	}
}

// writeComment emits text as wrapped TOML comment lines, collapsing the source
// comment's own line breaks first so only the words matter.
func writeComment(buf *bytes.Buffer, text string) {
	for _, line := range wrap(strings.Fields(text), commentTextWidth) {
		fmt.Fprintf(buf, "# %s\n", line)
	}
}

// wrap greedily packs words into lines no longer than width.
func wrap(words []string, width int) []string {
	var lines []string
	var line string

	for _, word := range words {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	if line != "" {
		lines = append(lines, line)
	}

	return lines
}
