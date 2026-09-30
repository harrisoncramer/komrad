package setup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"komrad/lib/xfile"

	"github.com/pelletier/go-toml/v2"
)

// SetTopLevelKey writes a string key above the file's first table, keeping every other line as
// it was.
func SetTopLevelKey(path, key, value string) error {
	return setTopLevel(path, key, strconv.Quote(value))
}

// SetTopLevelBool writes a boolean key above the file's first table, keeping every other line as
// it was.
func SetTopLevelBool(path, key string, value bool) error {
	return setTopLevel(path, key, strconv.FormatBool(value))
}

func setTopLevel(path, key, literal string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	written := setKeyLine(string(existing), key, literal)

	var parsed map[string]any
	if err := toml.Unmarshal([]byte(written), &parsed); err != nil {
		return fmt.Errorf("failed to write %s, since the result would not parse: %w", path, err)
	}

	return writeAtomically(path, []byte(written))
}

var tableHeader = regexp.MustCompile(`^\s*\[`)

func setKeyLine(text, key, literal string) string {
	line := key + " = " + literal
	assignment := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)

	lines := strings.Split(text, "\n")
	firstTable := len(lines)
	for i, existing := range lines {
		if tableHeader.MatchString(existing) {
			firstTable = i

			break
		}
		if assignment.MatchString(existing) {
			lines[i] = line

			return strings.Join(lines, "\n")
		}
	}

	if strings.TrimSpace(text) == "" {
		return line + "\n"
	}

	inserted := make([]string, 0, len(lines)+2)
	if firstTable == len(lines) && lines[firstTable-1] == "" {
		firstTable--
	}
	inserted = append(inserted, lines[:firstTable]...)
	inserted = append(inserted, line)
	if firstTable < len(lines) && tableHeader.MatchString(lines[firstTable]) {
		inserted = append(inserted, "")
	}
	inserted = append(inserted, lines[firstTable:]...)

	return strings.Join(inserted, "\n")
}

func writeAtomically(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	if err := xfile.WriteAtomic(path, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}
