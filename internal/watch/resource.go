package watch

import (
	"slices"
	"strings"

	annotationv1 "weave/proto/gen/annotation/v1"
	watchv1 "weave/proto/gen/watch/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// resources is the resource each table a notification names is the rows of.
var resources = resourcesByTable()

// resourcesByTable reads the annotated resources into the table each one is keyed by on the
// wire.
func resourcesByTable() map[string]watchv1.Resource {
	byTable := map[string]watchv1.Resource{}
	for _, r := range AllResources() {
		number, ok := watchv1.Resource_value[r.EnumValue()]
		if !ok {
			continue
		}

		byTable[r.Option.GetTable()] = watchv1.Resource(number)
	}

	return byTable
}

// Resource is one message declared as a row of a table.
type Resource struct {
	Message protoreflect.MessageDescriptor
	Option  *annotationv1.Resource
}

// Name is the message's own name.
func (r Resource) Name() string {
	return string(r.Message.Name())
}

// Key is the column the row is keyed by.
func (r Resource) Key() string {
	if key := r.Option.GetKey(); key != "" {
		return key
	}

	return "id"
}

// Plural is the table's name in camel case.
func (r Resource) Plural() string {
	return camel(r.Option.GetTable())
}

// EnumValue is the Resource enum value the row is sent under.
func (r Resource) EnumValue() string {
	return "RESOURCE_" + strings.ToUpper(snake(r.Name()))
}

// AllResources is every message carrying the option, ordered by table.
func AllResources() []Resource {
	var all []Resource

	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		messages := file.Messages()
		for i := range messages.Len() {
			if option := optionOf(messages.Get(i)); option != nil {
				all = append(all, Resource{Message: messages.Get(i), Option: option})
			}
		}

		return true
	})

	slices.SortFunc(all, func(a, b Resource) int {
		return strings.Compare(a.Option.GetTable(), b.Option.GetTable())
	})

	return all
}

// optionOf is the option a message carries, nil when it carries none.
func optionOf(message protoreflect.MessageDescriptor) *annotationv1.Resource {
	opts, ok := message.Options().(*descriptorpb.MessageOptions)
	if !ok || !proto.HasExtension(opts, annotationv1.E_Resource) {
		return nil
	}

	option, _ := proto.GetExtension(opts, annotationv1.E_Resource).(*annotationv1.Resource)

	return option
}

// camel turns pull_requests into PullRequests.
func camel(table string) string {
	var b strings.Builder
	for word := range strings.SplitSeq(table, "_") {
		if word == "" {
			continue
		}

		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}

	return b.String()
}

// snake turns PullRequest into pull_request.
func snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}

		b.WriteRune(r)
	}

	return strings.ToLower(b.String())
}
