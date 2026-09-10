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

// keyTypeRange is the span of tea.KeyType values pressableNames reads, the control keys running
// from 0 to 127 and the named keys counting down from minus one.
const (
	firstKeyType = -100
	lastKeyType  = 127
)

// pressableNames is every name bubbletea prints for a press, which is the set a binding can name
// besides a single rune.
var pressableNames = func() []string {
	var names []string

	for code := firstKeyType; code <= lastKeyType; code++ {
		name := tea.KeyType(code).String()
		if name == "" || name == "runes" {
			continue
		}

		names = append(names, name)
	}

	return names
}()

// isPressable reports whether a press exists that bubbletea would print as this string.
func isPressable(key string) bool {
	bare := strings.TrimPrefix(key, "alt+")
	if slices.Contains(pressableNames, bare) {
		return true
	}

	rune, size := utf8.DecodeRuneInString(bare)

	return size == len(bare) && rune != utf8.RuneError && unicode.IsGraphic(rune)
}

// validateKeybindings rejects the bindings the panel could never answer, naming the setting the
// way the file spells it.
func validateKeybindings(keys Keybindings) error {
	groups := reflect.ValueOf(keys)

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

			if len(bound) == 0 {
				return fmt.Errorf("%s: names no key, which leaves an action on screen that no press reaches", setting)
			}

			if err := validateBinding(setting, bound, held); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateBinding checks one setting's keys, held carrying the keys the group's earlier settings
// have already taken.
func validateBinding(setting string, bound []string, held map[string]string) error {
	for _, key := range bound {
		if !isPressable(key) {
			return fmt.Errorf("%s: %q is not a key any press produces", setting, key)
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
