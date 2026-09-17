// Package tfvars changes one value in a Terraform variables file, leaving everything else alone.
//
// A variables file is edited by people, so a task that has to change one value in it cannot
// simply rewrite the file. Nor can it reach for sed, since a value may be a list spread over
// several lines and the file carries the alignment and comments somebody chose. This reads the
// one assignment it was asked about and writes back the same file with that value replaced.
//
// Comments are blanked byte for byte rather than rune by rune when the list is scanned. Everything the scan looks for is ASCII, and keeping the length is what lets an index
// taken from the blanked copy point at the same place in the original. One space per rune
// shortens the copy wherever a comment holds a wider character, and slicing the original at a
// short index cuts an entry mid-value, which leaves a file that still parses and an address
// that is quietly wrong.
package tfvars

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// Command is the subcommand this package is reached through.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tfvars",
		Short: "Change one value in a Terraform variables file",
		Long: `Change one value in a Terraform variables file.

The file is edited by people, so this replaces the one value it was asked about and
leaves the alignment, the comments and everything else as they were.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(setCommand(), addCommand())

	return cmd
}

func setCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <file> <name> <value>",
		Short: "Replace a value, keeping the padding the file uses",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			return apply(args[0], args[1], args[2], set)
		},
	}
}

func addCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <file> <name> <value>",
		Short: "Append to a list, doing nothing when the value is already in it",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			return apply(args[0], args[1], args[2], add)
		},
	}
}

type edit func(text, name, value string) (string, bool, error)

func apply(path, name, value string, change edit) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	updated, changed, err := change(string(raw), name, value)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if !changed {
		return nil
	}

	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// set replaces a quoted scalar, keeping the padding the file already uses.
func set(text, name, value string) (string, bool, error) {
	pattern := regexp.MustCompile(`(?m)^([ \t]*` + regexp.QuoteMeta(name) + `[ \t]*=[ \t]*)"[^"]*"`)

	found := pattern.FindStringSubmatchIndex(text)
	if found == nil {
		return "", false, fmt.Errorf("no %s to change", name)
	}

	prefix := text[found[2]:found[3]]

	return text[:found[0]] + prefix + `"` + value + `"` + text[found[1]:], true, nil
}

// add puts a value into a list without rewriting the rest of it, because the file is written by
// people: it carries their alignment, the lines they chose to break on, and the comments they
// wrote against particular entries.
func add(text, name, value string) (string, bool, error) {
	pattern := regexp.MustCompile(`(?ms)^[ \t]*` + regexp.QuoteMeta(name) + `[ \t]*=[ \t]*\[(.*?)\]`)

	found := pattern.FindStringSubmatchIndex(text)
	if found == nil {
		return "", false, fmt.Errorf("no %s to add to", name)
	}

	inner := text[found[2]:found[3]]

	if holds(inner, value) {
		return "", false, nil
	}

	return text[:found[2]] + extended(inner, value) + text[found[3]:], true, nil
}

// holds reports whether the list already carries a value, reading past comments so that an
// address named in one is not taken for an entry.
func holds(inner, value string) bool {
	quoted := regexp.MustCompile(`"` + regexp.QuoteMeta(value) + `"`)

	return quoted.MatchString(withoutComments(inner))
}

// withoutComments blanks what a comment covers, byte for byte, so every offset stays where it was.
func withoutComments(inner string) string {
	out := []byte(inner)

	commented := false

	for index := 0; index < len(out); index++ {
		switch {
		case out[index] == '\n':
			commented = false

			continue
		case out[index] == '#':
			commented = true
		case out[index] == '/' && index+1 < len(out) && out[index+1] == '/':
			commented = true
		}

		if commented {
			out[index] = ' '
		}
	}

	return string(out)
}

// extended is the list with a value added, written so that whatever is already inside the
// brackets comes back unchanged, comments and line breaks included.
func extended(inner, value string) string {
	quoted := `"` + value + `"`

	last := lastEntry(inner)
	if last < 0 {
		// An empty list written over lines keeps its shape too, rather than having its closing
		// bracket pulled up onto the entry.
		if strings.Contains(inner, "\n") {
			indent := indentOf(inner, len(inner))
			if indent == "" {
				indent = "  "
			}

			return "\n" + indent + quoted + "," + inner
		}

		return inner + quoted
	}

	if !strings.Contains(inner, "\n") {
		if comma, ok := commaAfter(inner, last); ok {
			return inner[:comma+1] + " " + quoted + inner[comma+1:]
		}

		return inner[:last] + ", " + quoted + inner[last:]
	}

	// A comment sits after the entry it belongs to, so a new line goes after the whole line
	// rather than after the closing quote, or the comment ends up annotating the new entry.
	end := strings.Index(inner[last:], "\n")
	if end < 0 {
		end = len(inner)
	} else {
		end += last
	}

	line := inner[:end]

	if _, ok := commaAfter(inner, last); !ok {
		line = inner[:last] + "," + inner[last:end]
	}

	return line + "\n" + indentOf(inner, last) + quoted + "," + inner[end:]
}

// commaAfter is where the comma separating an entry from the next one is, if it is there at all.
func commaAfter(inner string, from int) (int, bool) {
	for index := from; index < len(inner); index++ {
		switch inner[index] {
		case ' ', '\t':
			continue
		case ',':
			return index, true
		default:
			return 0, false
		}
	}

	return 0, false
}

// lastEntry is just past the closing quote of the last entry, or -1 when the list is empty.
func lastEntry(inner string) int {
	quoted := regexp.MustCompile(`"[^"]*"`)

	matches := quoted.FindAllStringIndex(withoutComments(inner), -1)
	if len(matches) == 0 {
		return -1
	}

	return matches[len(matches)-1][1]
}

// indentOf is the whitespace the line holding an offset begins with, so a new line sits where
// the ones around it do.
func indentOf(inner string, at int) string {
	start := strings.LastIndex(inner[:at], "\n")
	if start < 0 {
		return "  "
	}

	line := inner[start+1:]

	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
