package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const enginesDirVariable = "KOMRAD_ENGINES_DIR"

// LocalEngine is the name that means this machine's own engine.
const LocalEngine = "local"

// EnginesDir holds one file per engine this machine knows how to reach, or is the directory
// KOMRAD_ENGINES_DIR names.
func EnginesDir() (string, error) {
	if dir := os.Getenv(enginesDirVariable); dir != "" {
		return dir, nil
	}

	dir, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "engines"), nil
}

// EnginePath is the file named engine, layered over the config and carrying only what differs
// from running the engine on this machine.
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

// EngineNames lists the engines this machine knows how to reach, sorted.
func EngineNames() ([]string, error) {
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
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" || entry.Name() == LocalEngine+".toml" {
			continue
		}

		names = append(names, strings.TrimSuffix(entry.Name(), ".toml"))
	}

	slices.Sort(names)

	return names
}

func knownEngines(dir string) string {
	names := engineNames(dir)
	if len(names) == 0 {
		return ", which holds no engines yet"
	}

	return ". There is " + strings.Join(names, ", ")
}
