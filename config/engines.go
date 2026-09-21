package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// enginePanelKey is the one connection setting an engine file may not carry.
const enginePanelKey = "connection.panel_key"

// EnginesDir holds one file per engine this machine knows how to reach.
func EnginesDir() (string, error) {
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

// requireEngineLayer refuses an engine file that names a panel key.
func requireEngineLayer(path string) error {
	overlay := viper.New()
	overlay.SetConfigFile(path)

	if err := overlay.ReadInConfig(); err != nil {
		return nil //nolint:nilerr // a file that cannot be read is reported by the load that follows
	}

	if overlay.IsSet(enginePanelKey) {
		return fmt.Errorf("%s sets %s, which an engine cannot: one panel key opens every engine, and the "+
			"picker chooses between them", path, enginePanelKey)
	}

	return nil
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
