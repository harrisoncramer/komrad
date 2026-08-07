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

	"weave/internal/actions"

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

// Cookbook is one manifest and the directory its action files live alongside. ManifestErr is
// set when cookbook.toml would not parse, in which case Manifest is zero-valued; Discover
// still returns the cookbook rather than dropping it, so a broken one is reported by
// Validate rather than silently missing from a listing.
type Cookbook struct {
	Name        string
	Dir         string
	Manifest    Manifest
	ManifestErr error
}

// InstalledActionName is what a sibling file is written under in the actions directory:
// prefixed with the cookbook's own name, so two cookbooks whose scripts share a generic file
// name like "setup" do not collide once installed.
func (c Cookbook) InstalledActionName(file string) string {
	return c.Name + "-" + file
}

// Validate reports every reason installing this cookbook would go wrong, joined into one
// error: a manifest that never parsed, a setup or cleanup action naming a file that is not
// one of its siblings, and a sibling file that would not run as an action once installed. A
// cookbook's own scripts are read the same way a real action is, since they are the same
// resource; ValidateAsInstalled is what accounts for InstallActions always chmoding them,
// which a file sitting in the cookbook's own directory has not been yet.
func (c Cookbook) Validate() error {
	if c.ManifestErr != nil {
		return c.ManifestErr
	}

	files, err := c.actionFiles()
	if err != nil {
		return err
	}

	var problems []error
	for _, action := range []string{c.Manifest.Workflow.SetupAction, c.Manifest.Workflow.CleanupAction} {
		if action != "" && !slices.Contains(files, action) {
			problems = append(problems, fmt.Errorf("names %q, which is not one of its sibling files", action))
		}
	}

	for _, file := range files {
		if err := actions.ValidateAsInstalled(filepath.Join(c.Dir, file)); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", file, err))
		}
	}

	return errors.Join(problems...)
}

// actionFiles is every sibling file this cookbook installs as an action: everything in its
// own directory other than the manifest, in directory order. InstallActions and Validate
// share this rather than each scanning the directory their own way.
func (c Cookbook) actionFiles() ([]string, error) {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cookbook %s: %w", c.Name, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == manifestName || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		files = append(files, entry.Name())
	}

	return files, nil
}

// InstallActions copies every file in this cookbook's own directory, other than its manifest,
// into actionsDir, named and made executable the way a hand-written action would be. Nothing
// has to list them: a cookbook's directory works the same way the actions directory itself
// does, where every file present is offered rather than registered. Returns those installed
// names in directory order.
func (c Cookbook) InstallActions(actionsDir string) ([]string, error) {
	files, err := c.actionFiles()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create the actions directory %s: %w", actionsDir, err)
	}

	names := make([]string, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(c.Dir, file))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s's action file %s: %w", c.Name, file, err)
		}

		name := c.InstalledActionName(file)
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
// is the ordinary case. A cookbook whose manifest will not parse is still returned, with
// ManifestErr set, so one broken cookbook cannot hide every other one in the same directory.
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

		found = append(found, Cookbook{Name: entry.Name(), Dir: cookbookDir, Manifest: manifest, ManifestErr: err})
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
