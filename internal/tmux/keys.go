package tmux

import (
	"errors"
	"fmt"
	"strings"
)

const panelPaneFormat = "#{==:#{pane_id},#{" + panelPaneOption + "}}"

// wheelOptionPrefix names the global options holding the user's own wheel commands.
const wheelOptionPrefix = "@komrad_wheel_"

// panelWheel is the wheel binding's arm over the panel's pane.
const panelWheel = "send-keys -M -t ="

// wheelPaneFormat is true of the panel's pane outside copy-mode.
const wheelPaneFormat = "#{&&:" + panelPaneFormat + ",#{==:#{pane_in_mode},0}}"

var keyNames = map[string]string{
	"space":     "Space",
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

// GetRootKeys returns every key bound in the root table with its command as tmux prints it.
func (c *Client) GetRootKeys() (map[string]string, error) {
	return c.getKeys("root")
}

// TakePanelKeys rebinds every panel key the root table holds to reach the panel on its own pane
// and keep the user's command everywhere else.
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

// TakePanelWheel rebinds the mouse wheel to hand the panel's own pane the mouse event and keep
// the user's command everywhere else.
func (c *Client) TakePanelWheel() error {
	held, err := c.GetRootKeys()
	if err != nil {
		return err
	}

	var errs []error

	for _, wheel := range []string{"WheelUpPane", "WheelDownPane"} {
		// tmux binds no WheelDownPane of its own, so an unbound wheel falls back to what the pane
		// would have been given.
		command, ok := held[wheel]
		if !ok {
			command = "send-keys -M"
		}

		option := wheelOptionPrefix + wheel
		if IsPanelPassThrough(command) {
			if strings.Contains(command, panelWheel) {
				continue
			}

			command = c.ShowGlobal(option)
			if command == "" {
				continue
			}
		} else {
			c.SetGlobal(option, command)
		}

		otherwise := strings.ReplaceAll(command, ` \; `, " ; ")

		if _, err := c.query("bind-key", "-n", wheel, "if-shell", "-F", "-t", "=", wheelPaneFormat, panelWheel, otherwise); err != nil {
			errs = append(errs, fmt.Errorf("failed to pass %s to the panel: %w", wheel, err))
		}
	}

	return errors.Join(errs...)
}

// IsPanelPassThrough reports whether a root binding is one the panel made for itself.
func IsPanelPassThrough(command string) bool {
	return strings.HasPrefix(command, "if-shell") && strings.Contains(command, panelPaneFormat)
}

func (c *Client) passKeyToPanel(key, command string) error {
	bound := key
	if key == ";" {
		bound = `\;`
	}

	through := "send-keys " + quoteKey(key)
	otherwise := strings.ReplaceAll(command, ` \; `, " ; ")

	if _, err := c.query("bind-key", "-n", bound, "if-shell", "-F", panelPaneFormat, through, otherwise); err != nil {
		return fmt.Errorf("failed to pass %q to the panel: %w", key, err)
	}

	return nil
}

func quoteKey(key string) string {
	if !strings.Contains(key, "'") {
		return "'" + key + "'"
	}

	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(key) + `"`
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
