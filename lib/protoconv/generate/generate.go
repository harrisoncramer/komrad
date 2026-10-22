// Package generate writes, for a protobuf message, a Go type holding each value its protovalidate
// rules describe as the Go type it stands for, and a function converting the message into it.
package generate

import (
	"errors"
	"fmt"
	"strconv"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/gofeaturespb"
)

const (
	protoconvPackage = protogen.GoImportPath("komrad/lib/protoconv")
	uuidPackage      = protogen.GoImportPath("github.com/google/uuid")
	timePackage      = protogen.GoImportPath("time")
)

const timestampName protoreflect.FullName = "google.protobuf.Timestamp"

// ErrSamePackage is a message whose parsed type would land in the Go package of the message itself.
var ErrSamePackage = errors.New("a parsed type cannot share the Go package of its message")

// ErrOpaqueAPI is a message generated with the opaque API, whose fields cannot be read directly.
var ErrOpaqueAPI = errors.New("a message generated with the opaque API cannot be converted")

// ErrNameTaken is two declarations wanting the same Go name in the output package.
var ErrNameTaken = errors.New("two declarations want the same Go name")

// ErrUnrepresentable is a rule letting through an empty value the converted type cannot hold.
var ErrUnrepresentable = errors.New("an empty value the rules let through has no converted form")

// leaf is the conversion one value goes through.
type leaf int

const (
	leafRaw leaf = iota
	leafUUID
	leafTrimmedUUID
	leafBytesUUID
	leafTime
	leafMessage
)

// slot is one value a field holds, as its single value, a list element, or a map key or value.
type slot struct {
	field   *protogen.Field
	rules   *validate.FieldRules
	ignored bool
}

// plan is how one field converts.
type plan struct {
	field    *protogen.Field
	value    slot
	key      slot
	required bool
	skipZero bool
}

// generator writes parsed types and their conversion functions into one generated Go file.
type generator struct {
	out     *protogen.GeneratedFile
	path    protogen.GoImportPath
	parsed  map[protoreflect.FullName]bool
	written map[protoreflect.FullName]bool
	names   map[string]string
}

// New returns a generator writing into out, a file of the Go package at path.
func New(out *protogen.GeneratedFile, path protogen.GoImportPath) *generator {
	return &generator{
		out:     out,
		path:    path,
		parsed:  map[protoreflect.FullName]bool{},
		written: map[protoreflect.FullName]bool{},
		names:   map[string]string{},
	}
}

// Write writes the parsed form of message and of every parsed message it reaches, reporting false when nothing in message converts.
func (g *generator) Write(message *protogen.Message) (bool, error) {
	g.decide(message)
	if !g.parsed[message.Desc.FullName()] {
		return false, nil
	}

	queue := []*protogen.Message{message}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if g.written[next.Desc.FullName()] {
			continue
		}
		g.written[next.Desc.FullName()] = true

		reached, err := g.writeMessage(next)
		if err != nil {
			return false, err
		}
		queue = append(queue, reached...)
	}

	return true, nil
}

// decide settles which messages reachable from root have a parsed form.
func (g *generator) decide(root *protogen.Message) {
	var reached []*protogen.Message
	g.reach(root, map[protoreflect.FullName]bool{}, &reached)

	for changed := true; changed; {
		changed = false
		for _, message := range reached {
			if !g.parsed[message.Desc.FullName()] && g.hasConversion(message) {
				g.parsed[message.Desc.FullName()] = true
				changed = true
			}
		}
	}

	for _, message := range reached {
		if _, decided := g.parsed[message.Desc.FullName()]; !decided {
			g.parsed[message.Desc.FullName()] = false
		}
	}
}

func (g *generator) reach(message *protogen.Message, seen map[protoreflect.FullName]bool, reached *[]*protogen.Message) {
	name := message.Desc.FullName()
	if _, decided := g.parsed[name]; decided || seen[name] {
		return
	}
	seen[name] = true
	*reached = append(*reached, message)

	for _, field := range message.Fields {
		value := planOf(field, false).value
		if value.ignored || value.field.Message == nil || value.field.Message.Desc.FullName() == timestampName {
			continue
		}
		g.reach(value.field.Message, seen, reached)
	}
}

func (g *generator) hasConversion(message *protogen.Message) bool {
	for _, field := range message.Fields {
		fieldPlan := planOf(field, false)
		if g.leafOf(fieldPlan.value) != leafRaw || g.leafOf(fieldPlan.key) != leafRaw {
			return true
		}
	}

	return false
}

func (g *generator) leafOf(value slot) leaf {
	if value.field == nil || value.ignored {
		return leafRaw
	}

	kind := value.field.Desc.Kind()
	if kind == protoreflect.StringKind && value.rules.GetString().GetUuid() {
		return leafUUID
	}
	if kind == protoreflect.StringKind && value.rules.GetString().GetTuuid() {
		return leafTrimmedUUID
	}
	if kind == protoreflect.BytesKind && value.rules.GetBytes().GetUuid() {
		return leafBytesUUID
	}
	if value.field.Message == nil {
		return leafRaw
	}
	if value.field.Message.Desc.FullName() == timestampName {
		return leafTime
	}
	if g.parsed[value.field.Message.Desc.FullName()] {
		return leafMessage
	}

	return leafRaw
}

func (l leaf) isUUID() bool {
	return l == leafUUID || l == leafTrimmedUUID || l == leafBytesUUID
}

// writeMessage writes one message's parsed type, its oneofs and its conversion function, returning the parsed messages it reaches.
func (g *generator) writeMessage(message *protogen.Message) ([]*protogen.Message, error) {
	if message.GoIdent.GoImportPath == g.path {
		return nil, fmt.Errorf("%w: %s", ErrSamePackage, message.Desc.FullName())
	}
	if message.APILevel == gofeaturespb.GoFeatures_API_OPAQUE {
		return nil, fmt.Errorf("%w: %s", ErrOpaqueAPI, message.Desc.FullName())
	}

	name := message.GoIdent.GoName
	if err := g.claim(name, string(message.Desc.FullName())); err != nil {
		return nil, err
	}
	if err := g.claim("Parse"+name, string(message.Desc.FullName())); err != nil {
		return nil, err
	}

	ruled := oneofRuled(message)
	plans := make([]plan, 0, len(message.Fields))
	var reached []*protogen.Message
	for _, field := range message.Fields {
		fieldPlan := planOf(field, ruled[field.Desc.Name()])
		if err := g.requireRepresentable(fieldPlan); err != nil {
			return nil, err
		}
		if g.leafOf(fieldPlan.value) == leafMessage {
			reached = append(reached, fieldPlan.value.field.Message)
		}
		plans = append(plans, fieldPlan)
	}

	if err := g.writeType(message, plans); err != nil {
		return nil, err
	}
	g.writeParse(message, plans)

	return reached, nil
}

func (g *generator) requireRepresentable(fieldPlan plan) error {
	if !fieldPlan.field.Desc.IsList() && !fieldPlan.field.Desc.IsMap() {
		return nil
	}

	for _, value := range []slot{fieldPlan.value, fieldPlan.key} {
		if g.leafOf(value).isUUID() && value.rules.GetIgnore() == validate.Ignore_IGNORE_IF_ZERO_VALUE {
			return fmt.Errorf("%w: %s", ErrUnrepresentable, fieldPlan.field.Desc.FullName())
		}
	}

	return nil
}

func (g *generator) claim(name, owner string) error {
	if existing, ok := g.names[name]; ok && existing != owner {
		return fmt.Errorf("%w: %s and %s both want %s", ErrNameTaken, existing, owner, name)
	}
	g.names[name] = owner

	return nil
}

func (g *generator) writeType(message *protogen.Message, plans []plan) error {
	name := message.GoIdent.GoName
	g.out.P("// ", name, " is ", message.GoIdent, " with the values its rules describe converted.")
	g.out.P("type ", name, " struct {")
	for _, fieldPlan := range plans {
		oneof := realOneof(fieldPlan.field)
		if oneof == nil {
			g.out.P(append([]any{fieldPlan.field.GoName, " "}, g.fieldType(fieldPlan)...)...)

			continue
		}
		if oneof.Fields[0] == fieldPlan.field {
			g.out.P(oneof.GoName, " ", oneofInterface(oneof))
		}
	}
	g.out.P("}")
	g.out.P()

	for _, oneof := range message.Oneofs {
		if oneof.Desc.IsSynthetic() {
			continue
		}
		if err := g.writeOneof(oneof); err != nil {
			return err
		}
	}

	return nil
}

func (g *generator) writeOneof(oneof *protogen.Oneof) error {
	marker := oneofInterface(oneof)
	if err := g.claim(marker, string(oneof.Desc.FullName())); err != nil {
		return err
	}

	g.out.P("type ", marker, " interface {")
	g.out.P(marker, "()")
	g.out.P("}")
	g.out.P()

	for _, field := range oneof.Fields {
		wrapper := field.GoIdent.GoName
		if err := g.claim(wrapper, string(field.Desc.FullName())); err != nil {
			return err
		}

		g.out.P("// ", wrapper, " is ", field.GoIdent, " with its value converted.")
		g.out.P("type ", wrapper, " struct {")
		g.out.P(append([]any{field.GoName, " "}, g.memberType(planOf(field, false).value)...)...)
		g.out.P("}")
		g.out.P()
		g.out.P("func (*", wrapper, ") ", marker, "() {}")
		g.out.P()
	}

	return nil
}

func (g *generator) fieldType(fieldPlan plan) []any {
	if fieldPlan.field.Desc.IsMap() {
		return append(append([]any{"map["}, g.memberType(fieldPlan.key)...), append([]any{"]"}, g.memberType(fieldPlan.value)...)...)
	}
	if fieldPlan.field.Desc.IsList() {
		return append([]any{"[]"}, g.memberType(fieldPlan.value)...)
	}

	switch g.leafOf(fieldPlan.value) {
	case leafUUID, leafTrimmedUUID, leafBytesUUID:
		if isOptionalUUID(fieldPlan) {
			return []any{"*", uuidPackage.Ident("UUID")}
		}

		return []any{uuidPackage.Ident("UUID")}
	case leafTime:
		if fieldPlan.required {
			return []any{timePackage.Ident("Time")}
		}

		return []any{"*", timePackage.Ident("Time")}
	case leafMessage:
		return g.memberType(fieldPlan.value)
	case leafRaw:
		if hasPointerScalar(fieldPlan.field) {
			return append([]any{"*"}, rawType(fieldPlan.field)...)
		}

		return rawType(fieldPlan.field)
	}

	return nil
}

// memberType is the Go type of a value with no presence of its own, as a list element, a map key or value, or a oneof member holds.
func (g *generator) memberType(value slot) []any {
	switch g.leafOf(value) {
	case leafUUID, leafTrimmedUUID, leafBytesUUID:
		return []any{uuidPackage.Ident("UUID")}
	case leafTime:
		return []any{timePackage.Ident("Time")}
	case leafMessage:
		return []any{"*", value.field.Message.GoIdent.GoName}
	case leafRaw:
		return rawType(value.field)
	}

	return nil
}

func (g *generator) writeParse(message *protogen.Message, plans []plan) {
	name := message.GoIdent.GoName
	parse := &body{}
	for _, fieldPlan := range plans {
		oneof := realOneof(fieldPlan.field)
		if oneof == nil {
			g.parseField(parse, fieldPlan)

			continue
		}
		if oneof.Fields[0] == fieldPlan.field {
			g.parseOneof(parse, oneof)
		}
	}

	g.out.P("// Parse", name, " converts a ", message.GoIdent, " into its parsed form.")
	g.out.P("func Parse", name, "(m *", message.GoIdent, ") (*", name, ", error) {")
	g.out.P("if m == nil {")
	g.out.P("return nil, ", protoconvPackage.Ident("ErrMissing"))
	g.out.P("}")
	g.out.P()
	if parse.usesErr {
		g.out.P("var err error")
	}
	g.out.P("parsed := &", name, "{}")
	for _, line := range parse.lines {
		g.out.P(line...)
	}
	g.out.P()
	g.out.P("return parsed, nil")
	g.out.P("}")
	g.out.P()
}

func (g *generator) parseField(parse *body, fieldPlan plan) {
	switch {
	case fieldPlan.field.Desc.IsMap():
		g.parseMap(parse, fieldPlan)
	case fieldPlan.field.Desc.IsList():
		g.parseList(parse, fieldPlan)
	default:
		g.parseSingular(parse, fieldPlan)
	}
}

func (g *generator) parseSingular(parse *body, fieldPlan plan) {
	path := string(fieldPlan.field.Desc.Name())
	source := "m." + fieldPlan.field.GoName
	target := "parsed." + fieldPlan.field.GoName
	conversion := g.leafOf(fieldPlan.value)

	switch conversion {
	case leafRaw:
		parse.line(target, " = ", source)
	case leafMessage, leafTime:
		call := g.convert(fieldPlan.value, source)
		switch {
		case fieldPlan.required:
			parse.assign(target, call, path)
		case conversion == leafMessage:
			parse.line("if ", source, " != nil {")
			parse.assign(target, call, path)
			parse.line("}")
		default:
			parse.line("if ", source, " != nil {")
			parse.point(target, call, path)
			parse.line("}")
		}
	case leafUUID, leafTrimmedUUID, leafBytesUUID:
		g.parseUUID(parse, fieldPlan)
	}
}

func (g *generator) parseUUID(parse *body, fieldPlan plan) {
	path := string(fieldPlan.field.Desc.Name())
	source := "m." + fieldPlan.field.GoName
	target := "parsed." + fieldPlan.field.GoName
	explicit := fieldPlan.field.Desc.HasPresence()

	value := source
	if explicit && fieldPlan.field.Desc.Kind() == protoreflect.StringKind {
		value = "*" + source
	}
	call := g.convert(fieldPlan.value, value)

	switch {
	case explicit && fieldPlan.required:
		parse.line("if ", source, " == nil {")
		parse.line("return nil, ", protoconvPackage.Ident("Field"), "(", strconv.Quote(path), ", ", protoconvPackage.Ident("ErrMissing"), ")")
		parse.line("}")
		parse.assign(target, call, path)
	case explicit:
		parse.line("if ", source, " != nil {")
		parse.point(target, call, path)
		parse.line("}")
	case fieldPlan.skipZero && fieldPlan.field.Desc.Kind() == protoreflect.BytesKind:
		parse.line("if len(", source, ") > 0 {")
		parse.point(target, call, path)
		parse.line("}")
	case fieldPlan.skipZero:
		parse.line("if ", source, ` != "" {`)
		parse.point(target, call, path)
		parse.line("}")
	default:
		parse.assign(target, call, path)
	}
}

func (g *generator) parseList(parse *body, fieldPlan plan) {
	source := "m." + fieldPlan.field.GoName
	target := "parsed." + fieldPlan.field.GoName
	if g.leafOf(fieldPlan.value) == leafRaw {
		parse.line(target, " = ", source)

		return
	}

	parse.usesErr = true
	parse.line("if ", source, " != nil {")
	parse.line(append(append([]any{target, " = make("}, g.fieldType(fieldPlan)...), ", len(", source, "))")...)
	parse.line("for index, item := range ", source, " {")
	parse.line(append(append([]any{"if ", target, "[index], err = "}, g.convert(fieldPlan.value, "item")...), "; err != nil {")...)
	parse.fail(string(fieldPlan.field.Desc.Name()), protoconvPackage.Ident("Index"), "(index, err)")
	parse.line("}")
	parse.line("}")
	parse.line("}")
}

func (g *generator) parseMap(parse *body, fieldPlan plan) {
	path := string(fieldPlan.field.Desc.Name())
	source := "m." + fieldPlan.field.GoName
	target := "parsed." + fieldPlan.field.GoName
	keyConverted := g.leafOf(fieldPlan.key) != leafRaw
	valueConverted := g.leafOf(fieldPlan.value) != leafRaw
	if !keyConverted && !valueConverted {
		parse.line(target, " = ", source)

		return
	}

	parse.line("if ", source, " != nil {")
	parse.line(append(append([]any{target, " = make("}, g.fieldType(fieldPlan)...), ", len(", source, "))")...)
	parse.line("for key, item := range ", source, " {")

	entry := "key"
	if keyConverted {
		entry = "converted"
		parse.line(append([]any{"converted, err := "}, g.convert(fieldPlan.key, "key")...)...)
		parse.line("if err != nil {")
		parse.fail(path, protoconvPackage.Ident("Key"), "(key, err)")
		parse.line("}")
		parse.line("if _, ok := ", target, "[converted]; ok {")
		parse.fail(path, protoconvPackage.Ident("Key"), "(key, ", protoconvPackage.Ident("ErrDuplicateKey"), ")")
		parse.line("}")
	}

	if valueConverted {
		if !keyConverted {
			parse.usesErr = true
		}
		parse.line(append(append([]any{"if ", target, "[", entry, "], err = "}, g.convert(fieldPlan.value, "item")...), "; err != nil {")...)
		parse.fail(path, protoconvPackage.Ident("Key"), "(key, err)")
		parse.line("}")
	} else {
		parse.line(target, "[", entry, "] = item")
	}

	parse.line("}")
	parse.line("}")
}

func (g *generator) parseOneof(parse *body, oneof *protogen.Oneof) {
	parse.line("switch member := m.", oneof.GoName, ".(type) {")
	for _, field := range oneof.Fields {
		wrapper := field.GoIdent.GoName
		value := planOf(field, false).value
		parse.line("case *", field.GoIdent, ":")
		if g.leafOf(value) == leafRaw {
			parse.line("parsed.", oneof.GoName, " = &", wrapper, "{", field.GoName, ": member.", field.GoName, "}")

			continue
		}

		parse.line(append([]any{"value, err := "}, g.convert(value, "member."+field.GoName)...)...)
		parse.line("if err != nil {")
		parse.fail(string(field.Desc.Name()), "err")
		parse.line("}")
		parse.line("parsed.", oneof.GoName, " = &", wrapper, "{", field.GoName, ": value}")
	}
	parse.line("}")
}

// convert is the call converting the value source holds.
func (g *generator) convert(value slot, source string) []any {
	switch g.leafOf(value) {
	case leafUUID:
		return []any{protoconvPackage.Ident("UUID"), "(", source, ")"}
	case leafTrimmedUUID:
		return []any{protoconvPackage.Ident("TrimmedUUID"), "(", source, ")"}
	case leafBytesUUID:
		return []any{protoconvPackage.Ident("BytesUUID"), "(", source, ")"}
	case leafTime:
		return []any{protoconvPackage.Ident("Time"), "(", source, ")"}
	case leafMessage:
		return []any{"Parse", value.field.Message.GoIdent.GoName, "(", source, ")"}
	case leafRaw:
		return []any{source}
	}

	return nil
}

// body is a conversion function's statements, held back until it is known whether they share one err.
type body struct {
	lines   [][]any
	usesErr bool
}

func (b *body) line(parts ...any) {
	b.lines = append(b.lines, parts)
}

// assign converts into target through the function's shared err.
func (b *body) assign(target string, call []any, path string) {
	b.usesErr = true
	b.line(append(append([]any{"if ", target, ", err = "}, call...), "; err != nil {")...)
	b.fail(path, "err")
	b.line("}")
}

// point converts into a value of its own and points target at it.
func (b *body) point(target string, call []any, path string) {
	b.line(append([]any{"value, err := "}, call...)...)
	b.line("if err != nil {")
	b.fail(path, "err")
	b.line("}")
	b.line(target, " = &value")
}

func (b *body) fail(path string, cause ...any) {
	b.line(append(append([]any{"return nil, ", protoconvPackage.Ident("Field"), "(", strconv.Quote(path), ", "}, cause...), ")")...)
}

func planOf(field *protogen.Field, oneofRuled bool) plan {
	rules := fieldRules(field)
	ignored := rules.GetIgnore() == validate.Ignore_IGNORE_ALWAYS
	fieldPlan := plan{
		field:    field,
		required: rules.GetRequired() || field.Desc.Cardinality() == protoreflect.Required,
	}

	switch {
	case field.Desc.IsMap():
		mapRules := rules.GetMap()
		fieldPlan.key = slot{
			field:   field.Message.Fields[0],
			rules:   mapRules.GetKeys(),
			ignored: ignored || mapRules.GetKeys().GetIgnore() == validate.Ignore_IGNORE_ALWAYS,
		}
		fieldPlan.value = slot{
			field:   field.Message.Fields[1],
			rules:   mapRules.GetValues(),
			ignored: ignored || mapRules.GetValues().GetIgnore() == validate.Ignore_IGNORE_ALWAYS,
		}
	case field.Desc.IsList():
		items := rules.GetRepeated().GetItems()
		fieldPlan.value = slot{
			field:   field,
			rules:   items,
			ignored: ignored || items.GetIgnore() == validate.Ignore_IGNORE_ALWAYS,
		}
	default:
		fieldPlan.value = slot{
			field:   field,
			rules:   rules,
			ignored: ignored,
		}
		fieldPlan.skipZero = !field.Desc.HasPresence() &&
			(oneofRuled || rules.GetIgnore() == validate.Ignore_IGNORE_IF_ZERO_VALUE)
	}

	return fieldPlan
}

func fieldRules(field *protogen.Field) *validate.FieldRules {
	options, ok := field.Desc.Options().(*descriptorpb.FieldOptions)
	if !ok || options == nil {
		return nil
	}

	rules, ok := proto.GetExtension(options, validate.E_Field).(*validate.FieldRules)
	if !ok {
		return nil
	}

	return rules
}

// oneofRuled names the fields a message-level oneof rule lists, whose zero value counts as unset.
func oneofRuled(message *protogen.Message) map[protoreflect.Name]bool {
	ruled := map[protoreflect.Name]bool{}
	options, ok := message.Desc.Options().(*descriptorpb.MessageOptions)
	if !ok || options == nil {
		return ruled
	}

	rules, ok := proto.GetExtension(options, validate.E_Message).(*validate.MessageRules)
	if !ok {
		return ruled
	}

	for _, rule := range rules.GetOneof() {
		for _, name := range rule.GetFields() {
			ruled[protoreflect.Name(name)] = true
		}
	}

	return ruled
}

func realOneof(field *protogen.Field) *protogen.Oneof {
	if field.Oneof == nil || field.Oneof.Desc.IsSynthetic() {
		return nil
	}

	return field.Oneof
}

func oneofInterface(oneof *protogen.Oneof) string {
	return "is" + oneof.GoIdent.GoName
}

func isOptionalUUID(fieldPlan plan) bool {
	return fieldPlan.skipZero || (fieldPlan.field.Desc.HasPresence() && !fieldPlan.required)
}

// hasPointerScalar reports whether protoc-gen-go holds the field as a pointer to a scalar.
func hasPointerScalar(field *protogen.Field) bool {
	if !field.Desc.HasPresence() || field.Desc.IsList() || field.Desc.IsMap() {
		return false
	}

	kind := field.Desc.Kind()

	return kind != protoreflect.MessageKind && kind != protoreflect.GroupKind && kind != protoreflect.BytesKind
}

// rawType is the Go type protoc-gen-go gives one value of the field.
func rawType(field *protogen.Field) []any {
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return []any{"bool"}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return []any{"int32"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return []any{"uint32"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return []any{"int64"}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return []any{"uint64"}
	case protoreflect.FloatKind:
		return []any{"float32"}
	case protoreflect.DoubleKind:
		return []any{"float64"}
	case protoreflect.StringKind:
		return []any{"string"}
	case protoreflect.BytesKind:
		return []any{"[]byte"}
	case protoreflect.EnumKind:
		return []any{field.Enum.GoIdent}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return []any{"*", field.Message.GoIdent}
	}

	return nil
}
