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

// The span of tea.KeyType values a key name can be read from.
const (
	firstKeyType = -100
	lastKeyType  = 127
)

// pressableNames is every name bubbletea prints for a press.
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

// keyChecker reads the bindings a config asks for and refuses the ones no press could reach.
type keyChecker struct {
	// taken maps a key to the setting holding it in the group being read.
	taken map[string]string
}

func newKeyChecker() *keyChecker {
	return &keyChecker{taken: map[string]string{}}
}

// checkKeybindings refuses a binding no press reaches, naming the setting as the file spells it.
func (c *keyChecker) checkKeybindings(keys Keybindings) error {
	groups := reflect.ValueOf(keys)

	for i := range groups.NumField() {
		name := groups.Type().Field(i).Tag.Get("mapstructure")

		if err := c.checkGroup(name, groups.Field(i)); err != nil {
			return err
		}
	}

	return nil
}

// checkGroup refuses a key used twice inside one group.
func (c *keyChecker) checkGroup(name string, group reflect.Value) error {
	c.taken = map[string]string{}

	for i := range group.NumField() {
		setting := name + "." + group.Type().Field(i).Tag.Get("mapstructure")

		bound, ok := group.Field(i).Interface().([]string)
		if !ok {
			return fmt.Errorf("%s: is not a list of keys", setting)
		}

		if len(bound) == 0 {
			return fmt.Errorf("%s: names no key, so nothing reaches what it does", setting)
		}

		if err := c.checkBinding(setting, bound); err != nil {
			return err
		}
	}

	return nil
}

// checkBinding refuses the keys one setting names.
func (c *keyChecker) checkBinding(setting string, bound []string) error {
	for _, key := range bound {
		if !c.isPressable(key) {
			return fmt.Errorf("%s: %q is not a key any press produces", setting, key)
		}

		if holder, ok := c.taken[key]; ok {
			if holder == setting {
				return fmt.Errorf("%s: %q is listed twice", setting, key)
			}

			return fmt.Errorf("%s: %q is already bound to %s", setting, key, holder)
		}

		c.taken[key] = setting
	}

	return nil
}

// isPressable reports whether a press exists that bubbletea would print as this string.
func (c *keyChecker) isPressable(key string) bool {
	bare := strings.TrimPrefix(key, "alt+")
	if slices.Contains(pressableNames, bare) {
		return true
	}

	pressed, size := utf8.DecodeRuneInString(bare)

	return size == len(bare) && pressed != utf8.RuneError && unicode.IsGraphic(pressed)
}
