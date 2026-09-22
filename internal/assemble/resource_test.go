package assemble_test

import (
	"reflect"
	"testing"

	"weave/internal/assemble"
	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// TestWorldIsTheResources pins the world to the resource option: one map per annotated
// message, named after its table and holding that message, and nothing else; and one arm of
// Apply per member of Row, putting by the resource's key and dropping by its enum value.
func TestWorldIsTheResources(t *testing.T) {
	t.Parallel()

	resources := watch.Resources()
	require.NotEmpty(t, resources)
	assert.Equal(t, len(resources), reflect.TypeFor[assemble.World]().NumField(), "World has a map per resource")

	members := (&watchv1.Row{}).ProtoReflect().Descriptor().Oneofs().ByName("row").Fields()
	assert.Equal(t, len(resources), members.Len(), "Row has a member per resource")

	for _, r := range resources {
		t.Run(r.Table, func(t *testing.T) {
			t.Parallel()

			messageType, err := protoregistry.GlobalTypes.FindMessageByName(r.Message.FullName())
			require.NoError(t, err)

			field, ok := reflect.TypeFor[assemble.World]().FieldByName(r.Plural())
			require.True(t, ok, "World has no %s", r.Plural())
			assert.Equal(t, reflect.MapOf(reflect.TypeFor[string](), reflect.TypeOf(messageType.New().Interface())), field.Type)

			var member protoreflect.FieldDescriptor
			for i := range members.Len() {
				if members.Get(i).Message().FullName() == r.Message.FullName() {
					member = members.Get(i)
				}
			}
			require.NotNil(t, member, "Row has no member for %s", string(r.Message.Name()))

			row := messageType.New()
			row.Set(row.Descriptor().Fields().ByName(protoreflect.Name(r.Key)), protoreflect.ValueOfString("k"))
			wire := &watchv1.Row{}
			wire.ProtoReflect().Set(member, protoreflect.ValueOfMessage(row))

			var world assemble.World
			world.Apply(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Put{Put: wire}})
			held := reflect.ValueOf(world).FieldByName(r.Plural())
			assert.True(t, held.MapIndex(reflect.ValueOf("k")).IsValid(), "Apply does not put a %s by its key", string(r.Message.Name()))

			world.Apply(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Resource: r.Enum, Key: "k"}}})
			held = reflect.ValueOf(world).FieldByName(r.Plural())
			assert.False(t, held.MapIndex(reflect.ValueOf("k")).IsValid(), "Apply does not drop a %s by its key", string(r.Message.Name()))
		})
	}
}
