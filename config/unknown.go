package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// reportUnknownKeys is the error for the keys in the file that no setting answers to, and
// nil when there are none. Each one is named with the settings its table does take.
//
// Listing what the table holds rather than guessing at the key that was meant: every table
// here is short enough to print, a list cannot point at the wrong thing the way a nearest
// match can, and it answers what else can be set at that level, which a guess does not.
func reportUnknownKeys(unused []string) error {
	if len(unused) == 0 {
		return nil
	}

	// Sorted because the decoder reports them in whatever order it walked the file, so two
	// runs over the same file would otherwise name the same keys in a different order.
	paths := slices.Clone(unused)
	slices.Sort(paths)

	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		lines = append(lines, describeUnknownKey(path))
	}

	return errors.New(strings.Join(lines, "\n"))
}

// describeUnknownKey is the one line reporting a key nothing answers to. A path whose table
// cannot be resolved is reported on its own, since a key under a table that does not exist
// has nothing to be listed beside.
func describeUnknownKey(path string) string {
	table := tableOf(path)

	known := listKnownKeys(table)
	if len(known) == 0 {
		return fmt.Sprintf("%s is not a setting", path)
	}

	return fmt.Sprintf("%s is not a setting; %s takes %s", path, describeTable(table), strings.Join(known, ", "))
}

// tableOf is the table a dotted key sits in, empty for a key at the top level.
func tableOf(path string) string {
	index := strings.LastIndex(path, ".")
	if index < 0 {
		return ""
	}

	return path[:index]
}

// describeTable names a table the way the file writes it, so what the error says to look
// for is what the reader is scanning for. A table reached through a list is named as one of
// its entries, since the position the key was found at is already on the key itself.
func describeTable(table string) string {
	if table == "" {
		return "the top level"
	}

	if open := strings.LastIndexByte(table, '['); open > 0 && strings.HasSuffix(table, "]") {
		return "an entry in " + table[:open]
	}

	return "[" + table + "]"
}

// listKnownKeys is the settings a table holds, keyed as the file keys them and in the order
// the generated reference lists them, or nil when the path names no table.
func listKnownKeys(table string) []string {
	current := reflect.TypeOf(Config{})

	if table != "" {
		for _, segment := range strings.Split(table, ".") {
			next, found := findFieldType(current, segment)
			if !found {
				return nil
			}

			current = next
		}
	}

	if current.Kind() != reflect.Struct {
		return nil
	}

	keys := make([]string, 0, current.NumField())
	for i := range current.NumField() {
		if tag := current.Field(i).Tag.Get("mapstructure"); tag != "" {
			keys = append(keys, tag)
		}
	}

	return keys
}

// findFieldType is the type of the field a segment keys, following a slice to what it holds
// so a key inside one of the marks resolves to the mark rather than to the list.
func findFieldType(structType reflect.Type, segment string) (reflect.Type, bool) {
	if structType.Kind() != reflect.Struct {
		return nil, false
	}

	// A key inside a list is reported with its position, which names the same type every
	// other entry has.
	if open := strings.IndexByte(segment, '['); open > 0 {
		segment = segment[:open]
	}

	for i := range structType.NumField() {
		field := structType.Field(i)
		if field.Tag.Get("mapstructure") != segment {
			continue
		}

		if field.Type.Kind() == reflect.Slice {
			return field.Type.Elem(), true
		}

		return field.Type, true
	}

	return nil, false
}
