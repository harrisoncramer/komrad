package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// Explainer: a binding is matched by string equality against what bubbletea prints for a press,
// so the only strings that can ever match are the names it prints and the single runes a
// keyboard sends. Nothing about that is visible in a config file: "escape", "PgUp" and "ctrl+m"
// all read as careful spellings and none of them is a key, the last because bubbletea prints
// that press as "enter". A binding of two characters cannot match either, since typing it
// delivers two presses. These are all checked here, when the file is read, because the panel
// cannot tell a key nobody pressed from a key that cannot be pressed.

// literalKeys are the presses the panel answers where it stands rather than through a binding,
// in the filter, the search box, the marks, the details and the message region. A default may
// name one, since the two agree about what the key does, and a config may not move it somewhere
// else, where the literal arm would win and the binding would never run.
var literalKeys = []string{"esc", "enter", "backspace", "ctrl+c", " ", "tab", "shift+tab", "ctrl+g"}

// pressableNames is every name bubbletea prints for a press, which is the set a binding can name
// besides a single rune.
func pressableNames() []string {
	var names []string

	for code := -100; code <= 127; code++ {
		name := tea.KeyType(code).String()
		if name == "" || name == "runes" {
			continue
		}

		names = append(names, name)
	}

	return names
}

// isPressable reports whether a press exists that bubbletea would print as this string.
func isPressable(key string) bool {
	bare := strings.TrimPrefix(key, "alt+")
	if slices.Contains(pressableNames(), bare) {
		return true
	}

	rune, size := utf8.DecodeRuneInString(bare)

	return size == len(bare) && rune != utf8.RuneError && unicode.IsGraphic(rune)
}

// validateKeybindings rejects the bindings the panel could never answer, naming the setting the
// way the file spells it.
func validateKeybindings(keys Keybindings) error {
	groups := reflect.ValueOf(keys)
	defaults := reflect.ValueOf(DefaultKeybindings())

	for i := range groups.NumField() {
		group := groups.Field(i)
		groupName := groups.Type().Field(i).Tag.Get("mapstructure")
		held := map[string]string{}

		for j := range group.NumField() {
			field := group.Type().Field(j)
			setting := groupName + "." + field.Tag.Get("mapstructure")
			bound, ok := group.Field(j).Interface().([]string)
			if !ok {
				return fmt.Errorf("%s: is not a list of keys", setting)
			}

			shipped, ok := defaults.Field(i).Field(j).Interface().([]string)
			if !ok {
				return fmt.Errorf("%s: has no shipped binding", setting)
			}

			if len(bound) == 0 {
				return fmt.Errorf("%s: names no key, which leaves an action on screen that no press reaches", setting)
			}

			if err := validateBinding(setting, bound, shipped, held); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateBinding checks one setting's keys, held carrying the keys the group's earlier settings
// have already taken.
func validateBinding(setting string, bound, shipped []string, held map[string]string) error {
	for _, key := range bound {
		switch {
		case !isPressable(key):
			return fmt.Errorf("%s: %q is not a key any press produces", setting, key)
		case slices.Contains(literalKeys, key) && !slices.Contains(shipped, key):
			return fmt.Errorf("%s: %q is answered by the panel itself and cannot be bound elsewhere", setting, key)
		}

		if taken, ok := held[key]; ok {
			if taken == setting {
				return fmt.Errorf("%s: %q is listed twice", setting, key)
			}

			return fmt.Errorf("%s: %q is already bound to %s", setting, key, taken)
		}

		held[key] = setting
	}

	return nil
}
