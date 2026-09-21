package tmux

import (
	"errors"
	"fmt"
	"strings"
)

const panelPaneFormat = "#{==:#{pane_id},#{" + panelPaneOption + "}}"

var keyNames = map[string]string{
	" ":         "Space",
	"enter":     "Enter",
	"esc":       "Escape",
	"tab":       "Tab",
	"shift+tab": "BTab",
	"backspace": "BSpace",
	"pgup":      "PageUp",
	"pgdown":    "PageDown",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"delete":    "Delete",
	"insert":    "IC",
}

// SpellKey is how tmux names the key a panel binding names.
func SpellKey(key string) string {
	if named, ok := keyNames[key]; ok {
		return named
	}

	if control, ok := strings.CutPrefix(key, "ctrl+"); ok {
		return "C-" + control
	}

	if alt, ok := strings.CutPrefix(key, "alt+"); ok {
		return "M-" + alt
	}

	return key
}

// GetPrefixKeys returns every key bound in the prefix table with its command as tmux prints it.
func (c *Client) GetPrefixKeys() (map[string]string, error) {
	return c.getKeys("prefix")
}

// GetRootKeys returns every key bound outside the prefix table with its command as tmux prints it.
func (c *Client) GetRootKeys() (map[string]string, error) {
	return c.getKeys("root")
}

// TakePanelKeys rebinds every key the panel binds that the root table holds, so a press on the
// panel's own pane reaches the panel while a press anywhere else runs the command the key held.
func (c *Client) TakePanelKeys(keys []string) error {
	held, err := c.GetRootKeys()
	if err != nil {
		return err
	}

	var errs []error

	for _, key := range keys {
		spelled := SpellKey(key)

		command, ok := held[spelled]
		if !ok || IsPanelPassThrough(command) {
			continue
		}

		if err := c.passKeyToPanel(spelled, command); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// IsPanelPassThrough reports whether a root binding is one TakePanelKeys made.
func IsPanelPassThrough(command string) bool {
	return strings.HasPrefix(command, "if-shell") && strings.Contains(command, panelPaneFormat)
}

func (c *Client) passKeyToPanel(key, command string) error {
	otherwise := strings.ReplaceAll(command, ` \; `, " ; ")

	if _, err := c.query("bind-key", "-n", key, "if-shell", "-F", panelPaneFormat, "send-keys", otherwise); err != nil {
		return fmt.Errorf("failed to pass %q to the panel: %w", key, err)
	}

	return nil
}

func (c *Client) getKeys(table string) (map[string]string, error) {
	listed, err := c.query("list-keys", "-T", table)
	if err != nil {
		return nil, fmt.Errorf("failed to list the %s table: %w", table, err)
	}

	bindings := map[string]string{}
	for line := range strings.SplitSeq(listed, "\n") {
		key, command, ok := parseKeyBinding(line)
		if ok {
			bindings[key] = command
		}
	}

	return bindings, nil
}

func parseKeyBinding(line string) (key, command string, ok bool) {
	rest := line
	for {
		var field string

		field, rest = cutField(rest)
		switch field {
		case "":
			return "", "", false
		case "-T":
			_, rest = cutField(rest)
			key, command = cutField(rest)

			return unquoteKey(key), command, key != ""
		}
	}
}

func cutField(text string) (field, rest string) {
	text = strings.TrimLeft(text, " \t")

	end := strings.IndexAny(text, " \t")
	if end < 0 {
		return text, ""
	}

	return text[:end], strings.TrimLeft(text[end:], " \t")
}

func unquoteKey(token string) string {
	switch {
	case len(token) == 2 && token[0] == '\\':
		return token[1:]
	case len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"':
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(token[1 : len(token)-1])
	case len(token) >= 2 && token[0] == '\'' && token[len(token)-1] == '\'':
		return token[1 : len(token)-1]
	}

	return strings.ReplaceAll(token, `\\`, `\`)
}
