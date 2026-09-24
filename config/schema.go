package config

import (
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
)

//go:generate go run gen/main.go

// pkgPath is the import path of this package, the key AddGoComments builds its
// comment map against.
const pkgPath = "weave/config"

// reflectSchema reflects Config into a JSON schema, taking each field's description from its Go doc
// comment in dir and each leaf's default from Defaults.
func reflectSchema(dir string) (*jsonschema.Schema, error) {
	reflector := &jsonschema.Reflector{
		FieldNameTag: "mapstructure",
		// The schema is walked, not published, so inline every definition rather
		// than emitting $refs the renderer would have to follow.
		DoNotReference: true,
		ExpandedStruct: true,
	}

	if err := reflector.AddGoComments("", dir); err != nil {
		return nil, fmt.Errorf("failed to extract doc comments: %w", err)
	}

	// The extractor keys each comment by the directory as it was named, not by import path.
	named := path.Clean(filepath.ToSlash(dir)) + "."
	for key, comment := range reflector.CommentMap {
		if rest, ok := strings.CutPrefix(key, named); ok {
			delete(reflector.CommentMap, key)
			reflector.CommentMap[pkgPath+"."+rest] = comment
		}
	}

	schema := reflector.Reflect(&Config{})

	if err := setDefaults(schema, reflect.ValueOf(*Defaults())); err != nil {
		return nil, err
	}

	return schema, nil
}

// setDefaults walks the schema alongside the default value so Defaults stays the only place a
// default is read from.
func setDefaults(schema *jsonschema.Schema, value reflect.Value) error {
	if schema.Properties == nil {
		return nil
	}

	for name, property := range schema.Properties.FromOldest() {
		field, err := fieldByName(value, name)
		if err != nil {
			return err
		}

		if property.Properties != nil {
			if err := setDefaults(property, field); err != nil {
				return err
			}

			continue
		}

		property.Default = field.Interface()
	}

	return nil
}

// fieldByName finds the struct field whose mapstructure tag is name, so the walk
// follows the same names the schema and the TOML file use.
func fieldByName(value reflect.Value, name string) (reflect.Value, error) {
	structType := value.Type()

	for i := range structType.NumField() {
		if structType.Field(i).Tag.Get("mapstructure") == name {
			return value.Field(i), nil
		}
	}

	return reflect.Value{}, fmt.Errorf("no field tagged mapstructure:%q on %s", name, structType)
}
