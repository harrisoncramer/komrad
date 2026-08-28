package config

import (
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
)

//go:generate go run gen/main.go

// pkgPath is the import path of this package, the key AddGoComments builds its
// comment map against.
const pkgPath = "weave/config"

// Schema reflects Config into a JSON schema, taking each field's description from its Go doc
// comment in dir and each leaf's default from DefaultKeybindings.
func Schema(dir string) (*jsonschema.Schema, error) {
	reflector := &jsonschema.Reflector{
		FieldNameTag: "mapstructure",
		// The schema is walked, not published, so inline every definition rather
		// than emitting $refs the renderer would have to follow.
		DoNotReference: true,
		ExpandedStruct: true,
	}

	if err := reflector.AddGoComments(pkgPath, dir); err != nil {
		return nil, fmt.Errorf("failed to extract doc comments: %w", err)
	}

	schema := reflector.Reflect(&Config{})

	if err := setDefaults(schema, reflect.ValueOf(*Defaults())); err != nil {
		return nil, err
	}

	return schema, nil
}

// setDefaults walks the schema alongside the default value so DefaultKeybindings stays the
// only place a default is written.
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
