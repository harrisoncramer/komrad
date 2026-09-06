package scripts

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"weave/lib/xslice"
)

var ErrScriptNotFound = errors.New("script not found")

// List returns every script across dirs in name order, where an earlier dir's file wins over
// a later one offering the same name.
func List(dirs ...string) ([]Script, error) {
	var all []Script

	// The directories are read again on every call, since a person who chmods a hook or edits
	// its header expects the next listing to say so rather than to wait for a restart.
	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		listed, err := discover(dir)
		if err != nil {
			return nil, err
		}

		all = append(all, listed...)
	}

	found := xslice.UniqueBy(all, func(script Script) string { return script.Name })
	slices.SortFunc(found, func(a, b Script) int { return strings.Compare(a.Name, b.Name) })

	return found, nil
}

// Find is the one script named name across dirs.
func Find(name string, dirs ...string) (Script, error) {
	found, err := List(dirs...)
	if err != nil {
		return Script{}, err
	}

	for _, script := range found {
		if script.Name == name {
			return script, nil
		}
	}

	var primary string
	if len(dirs) > 0 {
		primary = dirs[0]
	}

	return Script{}, fmt.Errorf("%w: %q is not in %s", ErrScriptNotFound, name, primary)
}
