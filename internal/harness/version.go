package harness

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/fail"
)

var (
	errUnsupportedVersion = fail.Precondition("the CLI is older than komrad supports")
	errNoVersion          = fail.Precondition("the CLI printed no version")
)

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

// CheckVersion refuses a harness whose CLI reports a version older than its MinVersion, leaving a
// CLI komrad cannot find to fail in its pane.
func CheckVersion(ctx context.Context, hn Harness) error {
	floor := hn.MinVersion()
	if floor == "" {
		return nil
	}

	output, err := exec.CommandContext(ctx, hn.Binary(), "--version").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the version of %s: %w", hn.Binary(), err)
	}

	return requireVersion(hn.Binary(), string(output), floor)
}

// requireVersion refuses the version a CLI printed when it is older than floor.
func requireVersion(binary, output, floor string) error {
	found, older, err := ReadVersion(output, floor)
	if errors.Is(err, errNoVersion) {
		return fmt.Errorf("%w: %s --version printed no version, and komrad needs %s or newer", errUnsupportedVersion, binary, floor)
	}
	if err != nil {
		return err
	}

	if older {
		return fmt.Errorf("%w: %s is %s, and komrad needs %s or newer", errUnsupportedVersion, binary, found, floor)
	}

	return nil
}

// ReadVersion returns the version a CLI printed for --version, and whether it is older than floor.
func ReadVersion(output, floor string) (string, bool, error) {
	found := versionPattern.FindString(output)
	if found == "" {
		return "", false, errNoVersion
	}

	foundParts, err := splitVersion(found)
	if err != nil {
		return "", false, err
	}
	floorParts, err := splitVersion(floor)
	if err != nil {
		return "", false, err
	}

	return found, slices.Compare(foundParts, floorParts) < 0, nil
}

func splitVersion(version string) ([]int, error) {
	parts := strings.Split(version, ".")
	numbers := make([]int, len(parts))
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("failed to read version %q: %w", version, err)
		}
		numbers[i] = number
	}

	return numbers, nil
}
