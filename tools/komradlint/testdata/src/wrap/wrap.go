package wrap

import (
	"errors"
	"fmt"
)

var errBase = errors.New("base")

func wrapped(name string) error {
	if name == "" {
		return fmt.Errorf("failed to read %s: %w", name, errBase)
	}

	if name == "sentinel" {
		return fmt.Errorf("%w: %s", errBase, name)
	}

	if name == "raw" {
		return fmt.Errorf(`failed to read %s: %w`, name, errBase)
	}

	return fmt.Errorf("reading %s: %w", name, errBase) // want `a message wrapping another error opens with "failed to " or with %w`
}

func unwrapped(name string) error {
	if name == "" {
		return fmt.Errorf("%s is not a name", name)
	}

	if name == "middle" {
		return fmt.Errorf("%s: %w happened", name, errBase)
	}

	return fmt.Errorf("git %s: %w", name, errBase) // want `a message wrapping another error opens with "failed to " or with %w`
}
