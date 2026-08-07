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

// Manifest is a cookbook's cookbook.toml, parsed. Nothing in it names the cookbook itself,
// the workflow it creates, the schedule it arms, or the actions it installs: those all take
// the cookbook's own name, or its own directory listing, so a manifest cannot drift from the
// identity and the files Find and InstallActions already see.
type Manifest struct {
	// Description is a one-line summary shown by `weave cookbook list`.
	Description string        `toml:"description"`
	Workflow    WorkflowSpec  `toml:"workflow"`
	Schedule    *ScheduleSpec `toml:"schedule"`
}

// WorkflowSpec is the workflow a cookbook creates. SetupAction and CleanupAction name one of
// the cookbook's sibling files by its bare name; InstalledActionName is what that resolves to
// once installed.
type WorkflowSpec struct {
	Context       string `toml:"context"`
	Harness       string `toml:"harness"`
	Cwd           string `toml:"cwd"`
	Flags         string `toml:"flags"`
	SetupAction   string `toml:"setup_action"`
	CleanupAction string `toml:"cleanup_action"`
}

// ScheduleSpec is the recurring launch a cookbook arms, the same fields create_schedule takes
// aside from the name and the workflow it launches into, which are the cookbook's own name and
// whichever workflow install just created.
type ScheduleSpec struct {
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

// InstalledActionName is what a sibling file is written under in the actions directory:
// prefixed with the cookbook's own name, so two cookbooks whose scripts share a generic file
// name like "setup" do not collide once installed.
func (c Cookbook) InstalledActionName(file string) string {
	return c.Name + "-" + file
}

// InstallActions copies every file in this cookbook's own directory, other than its manifest,
// into actionsDir, named and made executable the way a hand-written action would be. Nothing
// has to list them: a cookbook's directory works the same way the actions directory itself
// does, where every file present is offered rather than registered. Returns those installed
// names in directory order.
func (c Cookbook) InstallActions(actionsDir string) ([]string, error) {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cookbook %s: %w", c.Name, err)
	}

	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create the actions directory %s: %w", actionsDir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == manifestName || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(c.Dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s's action file %s: %w", c.Name, entry.Name(), err)
		}

		name := c.InstalledActionName(entry.Name())
		written := filepath.Join(actionsDir, name)
		if err := os.WriteFile(written, content, 0o755); err != nil {
			return nil, fmt.Errorf("failed to write action %s: %w", written, err)
		}

		names = append(names, name)
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
