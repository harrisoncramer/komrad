package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EnginesDir holds one file per engine this machine knows how to reach.
func EnginesDir() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "engines"), nil
}

// EnginePath is the file named engine, which is layered over the config rather than replacing
// it, so it carries only what differs from running the engine on this machine.
func EnginePath(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("--engine was given an empty name")
	}

	if name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf("%q is not an engine name: an engine is a file in the engines directory, not a path", name)
	}

	dir, err := EnginesDir()
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, name+".toml")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("there is no engine called %q in %s%s", name, dir, knownEngines(dir))
	}

	return path, nil
}

// AvailableEngines is every engine this machine can be pointed at, in the order they list.
func AvailableEngines() ([]string, error) {
	dir, err := EnginesDir()
	if err != nil {
		return nil, err
	}

	return engineNames(dir), nil
}

func engineNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}

		names = append(names, strings.TrimSuffix(entry.Name(), ".toml"))
	}

	sort.Strings(names)

	return names
}

func knownEngines(dir string) string {
	names := engineNames(dir)
	if len(names) == 0 {
		return ", which holds no engines yet"
	}

	return ". There is " + strings.Join(names, ", ")
}
