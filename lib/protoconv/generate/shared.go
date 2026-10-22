package generate

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ErrSharedShape is a oneof that cannot be written as the Go type named for it, because its members
// differ from those of another oneof with the same name.
var ErrSharedShape = errors.New("oneofs with the same name must hold the same members")

// sharedType is one Go type every oneof with its name is written as.
type sharedType struct {
	owner   protoreflect.FullName
	members []sharedMember
}

// sharedMember is one value a shared type holds, named for the field that sets it.
type sharedMember struct {
	name   string
	goType []any
}

// Shared holds the Go types oneofs across every package of a run are written as, one for each
// oneof name.
type Shared struct {
	path  protogen.GoImportPath
	types map[string]*sharedType
}

// NewShared returns the shared types of a run, written into the Go package at path.
func NewShared(path protogen.GoImportPath) *Shared {
	return &Shared{
		path:  path,
		types: map[string]*sharedType{},
	}
}

// Write writes every shared type a oneof was written as into out, reporting false when there were none.
func (s *Shared) Write(out *protogen.GeneratedFile) bool {
	for _, name := range slices.Sorted(maps.Keys(s.types)) {
		out.P("// ", name, " holds the one member a caller set in a oneof written as ", name, ", leaving the others nil.")
		out.P("type ", name, " struct {")
		for _, member := range s.types[name].members {
			out.P(slices.Concat([]any{member.name, " *"}, member.goType)...)
		}
		out.P("}")
		out.P()
	}

	return len(s.types) > 0
}

// add records the members of the oneof owner under the shared type name, refusing members that
// differ from those recorded.
func (s *Shared) add(name string, owner protoreflect.FullName, members []sharedMember) error {
	recorded, ok := s.types[name]
	if !ok {
		s.types[name] = &sharedType{owner: owner, members: members}

		return nil
	}
	if !isSameShape(recorded.members, members) {
		return fmt.Errorf("%w: %s and %s are both %s", ErrSharedShape, recorded.owner, owner, name)
	}

	return nil
}

// sharedIdent returns the shared type a oneof is written as, reporting false for a oneof holding a
// message, which keeps a type of its own.
func (g *Generator) sharedIdent(oneof *protogen.Oneof) (protogen.GoIdent, bool) {
	if oneof.Desc.IsSynthetic() {
		return protogen.GoIdent{}, false
	}

	for _, field := range oneof.Fields {
		conversion := g.leafOf(planOf(field, false).value)
		if conversion == leafMessage || (conversion == leafRaw && field.Message != nil) {
			return protogen.GoIdent{}, false
		}
	}

	return g.shared.path.Ident(oneof.GoName), true
}

func (g *Generator) sharedMembers(oneof *protogen.Oneof) []sharedMember {
	members := make([]sharedMember, 0, len(oneof.Fields))
	for _, field := range oneof.Fields {
		members = append(members, sharedMember{name: field.GoName, goType: g.memberType(planOf(field, false).value)})
	}
	slices.SortFunc(members, func(a, b sharedMember) int { return strings.Compare(a.name, b.name) })

	return members
}

func isSameShape(recorded, members []sharedMember) bool {
	return slices.EqualFunc(recorded, members, func(a, b sharedMember) bool {
		return a.name == b.name && fmt.Sprint(a.goType...) == fmt.Sprint(b.goType...)
	})
}
