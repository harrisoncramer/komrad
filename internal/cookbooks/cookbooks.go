// Package cookbooks reads installable recipes: a cookbook.toml manifest describing a
// workflow and an optional schedule, plus the action scripts named alongside it. It runs
// nothing itself. Installing a cookbook is copying its scripts into the actions directory and
// calling the same create_workflow and create_schedule endpoints a person or an MCP tool
// already can, which is the caller's job, not this package's.
package cookbooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// manifestName is the file inside a cookbook's directory that describes it.
const manifestName = "cookbook.toml"

// ErrCookbookNotFound reports that no directory in the given set holds a cookbook by that name.
var ErrCookbookNotFound = errors.New("cookbook not found")

// Manifest is a cookbook's cookbook.toml, parsed.
type Manifest struct {
	// Description is a one-line summary shown by `weave cookbook list`.
	Description string       `toml:"description"`
	Workflow    WorkflowSpec `toml:"workflow"`
	// Actions are the scripts this cookbook installs alongside its workflow, whether or not
	// the workflow names them as its setup or cleanup action.
	Actions  []ActionSpec  `toml:"actions"`
	Schedule *ScheduleSpec `toml:"schedule"`
}

// WorkflowSpec is the workflow a cookbook creates, the same fields create_workflow takes.
type WorkflowSpec struct {
	Name          string `toml:"name"`
	Context       string `toml:"context"`
	Harness       string `toml:"harness"`
	Cwd           string `toml:"cwd"`
	Flags         string `toml:"flags"`
	SetupAction   string `toml:"setup_action"`
	CleanupAction string `toml:"cleanup_action"`
}

// ActionSpec is one script a cookbook installs. File is the path to it, relative to the
// cookbook's own directory, and Name is what it is written under in the actions directory,
// which is what a workflow's setup_action or cleanup_action names.
type ActionSpec struct {
	Name string `toml:"name"`
	File string `toml:"file"`
}

// ScheduleSpec is the recurring launch a cookbook arms, the same fields create_schedule takes
// aside from the workflow it launches into, which is whichever one this cookbook just created.
type ScheduleSpec struct {
	Name       string `toml:"name"`
	Cron       string `toml:"cron"`
	Timezone   string `toml:"timezone"`
	Prompt     string `toml:"prompt"`
	Enabled    bool   `toml:"enabled"`
	HideAgents bool   `toml:"hide_agents"`
}

// Cookbook is one manifest and the directory its action files live alongside.
type Cookbook struct {
	Name     string
	Dir      string
	Manifest Manifest
}

// InstallActions copies every action this cookbook declares into actionsDir, named and made
// executable the way a hand-written action would be, and returns those names in manifest order.
func (c Cookbook) InstallActions(actionsDir string) ([]string, error) {
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create the actions directory %s: %w", actionsDir, err)
	}

	names := make([]string, 0, len(c.Manifest.Actions))
	for _, spec := range c.Manifest.Actions {
		content, err := os.ReadFile(filepath.Join(c.Dir, spec.File))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s's action file %s: %w", c.Name, spec.File, err)
		}

		written := filepath.Join(actionsDir, spec.Name)
		if err := os.WriteFile(written, content, 0o755); err != nil {
			return nil, fmt.Errorf("failed to write action %s: %w", written, err)
		}

		names = append(names, spec.Name)
	}

	return names, nil
}

// Discover lists the cookbooks in dir, one per subdirectory holding a cookbook.toml. A
// directory that does not exist yields no cookbooks and no error, since not having written one
// is the ordinary case.
func Discover(dir string) ([]Cookbook, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read the cookbooks directory %s: %w", dir, err)
	}

	found := make([]Cookbook, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		cookbookDir := filepath.Join(dir, entry.Name())

		manifest, err := readManifest(filepath.Join(cookbookDir, manifestName))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}

		found = append(found, Cookbook{Name: entry.Name(), Dir: cookbookDir, Manifest: manifest})
	}

	slices.SortFunc(found, func(a, b Cookbook) int { return strings.Compare(a.Name, b.Name) })

	return found, nil
}

// List returns every cookbook across dirs in name order, where an earlier dir's cookbook wins
// over a later one offering the same name, so a user's own directory can override a built-in.
func List(dirs ...string) ([]Cookbook, error) {
	var found []Cookbook
	claimed := make(map[string]bool)

	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		listed, err := Discover(dir)
		if err != nil {
			return nil, err
		}

		for _, cookbook := range listed {
			if claimed[cookbook.Name] {
				continue
			}

			claimed[cookbook.Name] = true
			found = append(found, cookbook)
		}
	}

	slices.SortFunc(found, func(a, b Cookbook) int { return strings.Compare(a.Name, b.Name) })

	return found, nil
}

// Find is the one cookbook named name across dirs, in the same override order List resolves.
func Find(name string, dirs ...string) (Cookbook, error) {
	found, err := List(dirs...)
	if err != nil {
		return Cookbook{}, err
	}

	for _, cookbook := range found {
		if cookbook.Name == name {
			return cookbook, nil
		}
	}

	return Cookbook{}, fmt.Errorf("%w: %q", ErrCookbookNotFound, name)
}

func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	return manifest, nil
}
