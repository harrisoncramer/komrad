package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"komrad/lib/xfile"
)

const trustKey = "hasTrustDialogAccepted"

func (h *Harness) resolveStatePath() (string, error) {
	if h.configDir != "" {
		return filepath.Join(h.configDir, ".claude.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".claude.json"), nil
}

// TrustDir records dir as trusted in Claude's own state, so it starts there without asking.
func (h *Harness) TrustDir(dir string) error {
	statePath, err := h.resolveStatePath()
	if err != nil {
		return err
	}

	current, err := os.ReadFile(statePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read the claude state %q: %w", statePath, err)
	}

	state := map[string]json.RawMessage{}
	if len(current) > 0 {
		if err := json.Unmarshal(current, &state); err != nil {
			return fmt.Errorf("failed to parse the claude state: %w", err)
		}
	}

	projects := map[string]map[string]json.RawMessage{}
	if raw, ok := state["projects"]; ok {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return fmt.Errorf("failed to parse the claude state's projects: %w", err)
		}
	}

	project := projects[dir]
	if project == nil {
		project = map[string]json.RawMessage{}
	}

	if string(project[trustKey]) == "true" {
		return nil
	}

	project[trustKey] = json.RawMessage("true")
	projects[dir] = project

	encodedProjects, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("failed to encode the claude state's projects: %w", err)
	}
	state["projects"] = encodedProjects

	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the claude state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return fmt.Errorf("failed to create the claude state dir: %w", err)
	}

	if err := xfile.WriteAtomic(statePath, body, 0o600); err != nil {
		return fmt.Errorf("failed to replace the claude state %q: %w", statePath, err)
	}

	return nil
}
