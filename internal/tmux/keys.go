package tmux

import (
	"fmt"
	"strings"
)

// GetPrefixKeys returns every key bound in the prefix table with its command as tmux prints it.
func (c *Client) GetPrefixKeys() (map[string]string, error) {
	listed, err := c.query("list-keys", "-T", "prefix")
	if err != nil {
		return nil, fmt.Errorf("failed to list the prefix table: %w", err)
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

	return token
}
