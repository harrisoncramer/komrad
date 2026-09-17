// Command tfvars changes one value in a Terraform variables file, leaving everything else alone.
//
// A variables file is edited by people, so a task that has to change one value in it cannot
// simply rewrite the file. Nor can it reach for sed, since a value may be a list spread over
// several lines and the file carries the alignment and comments somebody chose. This reads the
// one assignment it was asked about and writes back the same file with that value replaced.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("usage: tfvars set|add <file> <name> <value>")
	}

	action, path, name, value := args[0], args[1], args[2], args[3]

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	var updated string
	var changed bool

	switch action {
	case "set":
		updated, changed, err = set(string(raw), name, value)
	case "add":
		updated, changed, err = add(string(raw), name, value)
	default:
		return fmt.Errorf("%q is not something this does, which is set or add", action)
	}

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

// add appends to a list, and reports no change when the value is already in it, so that running
// a task twice does not write the same address in twice and have the cloud refuse the pair.
func add(text, name, value string) (string, bool, error) {
	pattern := regexp.MustCompile(`(?ms)^[ \t]*` + regexp.QuoteMeta(name) + `[ \t]*=[ \t]*\[(.*?)\]`)

	found := pattern.FindStringSubmatchIndex(text)
	if found == nil {
		return "", false, fmt.Errorf("no %s to add to", name)
	}

	inner := text[found[2]:found[3]]
	wanted := `"` + value + `"`

	existing := make([]string, 0, 4)

	for _, part := range strings.Split(inner, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		if trimmed == wanted {
			return "", false, nil
		}

		existing = append(existing, trimmed)
	}

	existing = append(existing, wanted)

	return text[:found[2]] + strings.Join(existing, ", ") + text[found[3]:], true, nil
}
