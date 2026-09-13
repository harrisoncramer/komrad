//go:build integration

package cli

import (
	"encoding/json"
	"strconv"
	"testing"

	"weave/internal/rpcapi"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TestEveryMethodsFlagsBuildARequestItsSchemaAccepts walks the whole exposed surface rather than
// one command, the flags being built by reflection.
func TestEveryMethodsFlagsBuildARequestItsSchemaAccepts(t *testing.T) {
	for exposed := range rpcapi.GetMethods() {
		t.Run(string(exposed.Method.FullName()), func(t *testing.T) {
			input := exposed.Method.Input()

			cmd := &cobra.Command{Use: "x"}
			fields := bindRequestFlags(cmd, input)

			set := cmd.Flags()
			for _, field := range fields.bound {
				require.NoError(t, set.Set(field.flag, valueFor(t, input, field.path)),
					"--%s takes a value of its own kind", field.flag)
			}

			body, err := fields.request(set, "")
			require.NoError(t, err)

			encoded, err := json.Marshal(body)
			require.NoError(t, err)

			err = protojson.UnmarshalOptions{}.Unmarshal(encoded, dynamicpb.NewMessage(input))
			require.NoError(t, err, "the request the flags built is one the schema reads: %s", encoded)
		})
	}
}

// TestNoRequiredFieldIsUnreachableFromTheFlags pins that a field the schema insists on is one
// the command line can carry.
func TestNoRequiredFieldIsUnreachableFromTheFlags(t *testing.T) {
	for exposed := range rpcapi.GetMethods() {
		t.Run(string(exposed.Method.FullName()), func(t *testing.T) {
			input := exposed.Method.Input()

			cmd := &cobra.Command{Use: "x"}
			fields := bindRequestFlags(cmd, input)

			for _, skipped := range fields.skipped {
				field := fieldAt(input, skipped)
				require.NotNil(t, field, "the skipped flag %q names a field of the request", skipped)
				assert.False(t, rpcapi.IsRequired(field),
					"%q is required and has no flag to carry it", skipped)
			}
		})
	}
}

func fieldAt(message protoreflect.MessageDescriptor, flag string) protoreflect.FieldDescriptor {
	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)
		name := flagNameOf(field)

		if name == flag {
			return field
		}
		if field.Kind() == protoreflect.MessageKind && !field.IsList() && !field.IsMap() {
			if len(flag) > len(name)+1 && flag[:len(name)+1] == name+"." {
				return fieldAt(field.Message(), flag[len(name)+1:])
			}
		}
	}

	return nil
}

func flagNameOf(field protoreflect.FieldDescriptor) string {
	name := ""
	for _, r := range string(field.Name()) {
		if r == '_' {
			name += "-"

			continue
		}
		name += string(r)
	}

	return name
}

func valueFor(t *testing.T, message protoreflect.MessageDescriptor, path []string) string {
	t.Helper()
	field := fieldAtJSON(message, path)
	require.NotNil(t, field, "the bound path %v names a field", path)

	if field.Kind() == protoreflect.BoolKind {
		return "true"
	}
	if field.Kind() == protoreflect.EnumKind {
		values := field.Enum().Values()
		require.Positive(t, values.Len())

		return string(values.Get(values.Len() - 1).Name())
	}
	if field.Kind() == protoreflect.StringKind {
		return "a value"
	}

	return strconv.Itoa(1)
}

func fieldAtJSON(message protoreflect.MessageDescriptor, path []string) protoreflect.FieldDescriptor {
	field := message.Fields().ByJSONName(path[0])
	if field == nil {
		return nil
	}
	if len(path) == 1 {
		return field
	}

	return fieldAtJSON(field.Message(), path[1:])
}
