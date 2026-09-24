package config

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// specialKeys is how many key codes bubbletea defines past its extended marker.
const specialKeys = 512

// lastFunctionKey is the highest function key a terminal without keyboard enhancements sends.
const lastFunctionKey = 20

// pressableNames is every name bubbletea prints for a key that is not a character.
var pressableNames = func() []string {
	names := []string{}
	codes := []rune{tea.KeyEnter, tea.KeyTab, tea.KeyBackspace, tea.KeyEscape, tea.KeySpace}
	for code := tea.KeyExtended + 1; code <= tea.KeyExtended+specialKeys; code++ {
		codes = append(codes, code)
	}

	for _, code := range codes {
		name := tea.Key{Code: code}.Keystroke()
		if utf8.RuneCountInString(name) > 1 && !isPastLastFunctionKey(name) {
			names = append(names, name)
		}
	}

	return names
}()

func isPastLastFunctionKey(name string) bool {
	number, err := strconv.Atoi(strings.TrimPrefix(name, "f"))

	return strings.HasPrefix(name, "f") && err == nil && number > lastFunctionKey
}

// controlAliases are the control characters a terminal sends for a key bubbletea names otherwise.
var controlAliases = []rune{'m', 'i', '['}

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
	bare := key

	var ctrl, shift bool
	if after, ok := strings.CutPrefix(bare, "ctrl+"); ok {
		ctrl, bare = true, after
	}
	bare = strings.TrimPrefix(bare, "alt+")
	if after, ok := strings.CutPrefix(bare, "shift+"); ok {
		shift, bare = true, after
	}

	if slices.Contains(pressableNames, bare) {
		return true
	}

	pressed, size := utf8.DecodeRuneInString(bare)
	if size != len(bare) || pressed == utf8.RuneError || !unicode.IsGraphic(pressed) || unicode.IsSpace(pressed) {
		return false
	}

	if ctrl && (slices.Contains(controlAliases, pressed) || unicode.IsUpper(pressed)) {
		return false
	}

	return !shift
}
