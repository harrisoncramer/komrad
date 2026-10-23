package main

import (
	"cmp"
	"regexp"
	"strings"

	"komrad/internal/eventprompts"
	"komrad/internal/rpcapi"
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	annotationv1 "komrad/proto/gen/annotation/v1"
	eventv1 "komrad/proto/gen/event/v1"

	"google.golang.org/protobuf/reflect/protoreflect"
)

type apiDoc struct {
	Services []serviceDoc          `json:"services"`
	Messages map[string]messageDoc `json:"messages"`
	Enums    map[string]enumDoc    `json:"enums"`
	Events   []eventDoc            `json:"events"`
}

type serviceDoc struct {
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Methods     []methodDoc `json:"methods"`
}

type methodDoc struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Command     string   `json:"command"`
	Tool        string   `json:"tool"`
	MCP         bool     `json:"mcp"`
	Roles       []string `json:"roles"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	ReadOnly    bool     `json:"readOnly"`
	Idempotent  bool     `json:"idempotent"`
	Destructive bool     `json:"destructive"`
	Request     string   `json:"request"`
	Response    string   `json:"response"`
}

type messageDoc struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Fields      []fieldDoc `json:"fields"`
	Oneofs      []oneofDoc `json:"oneofs"`
}

type oneofDoc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type fieldDoc struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Ref         string             `json:"ref"`
	Repeated    bool               `json:"repeated"`
	Map         bool               `json:"map"`
	Oneof       string             `json:"oneof"`
	Required    bool               `json:"required"`
	Constraints rpcapi.Constraints `json:"constraints"`
	Description string             `json:"description"`
}

type enumDoc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Values      []enumValueDoc `json:"values"`
}

type enumValueDoc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type eventDoc struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	DefaultPrompt string `json:"defaultPrompt"`
}

var firstSentence = regexp.MustCompile(`^.*?[.!?](\s|$)`)

func buildAPI(notes comments) apiDoc {
	doc := apiDoc{
		Services: []serviceDoc{},
		Messages: map[string]messageDoc{},
		Enums:    map[string]enumDoc{},
		Events: xslice.Map(protoenum.Values[eventv1.Event](), func(event eventv1.Event) eventDoc {
			return eventDoc{Name: protoenum.Name(event), Description: protoenum.Label(event).GetDescription(), DefaultPrompt: eventprompts.DefaultPrompt(event)}
		}),
	}
	index := map[protoreflect.FullName]int{}

	for exposed := range rpcapi.GetMethods() {
		at, ok := index[exposed.Service.FullName()]
		if !ok {
			title := strings.TrimSuffix(string(exposed.Service.Name()), "Service")
			doc.Services = append(doc.Services, serviceDoc{
				Slug:        kebab(title),
				Title:       splitWords(title),
				Name:        string(exposed.Service.FullName()),
				Description: notes.of(exposed.Service),
				Methods:     []methodDoc{},
			})
			at = len(doc.Services) - 1
			index[exposed.Service.FullName()] = at
		}

		description := strings.Join(strings.Fields(exposed.Surfaces.GetDescription()), " ")
		tool := exposed.Surfaces.GetName()
		readOnly := rpcapi.IsReadOnly(exposed.Method)

		roles := xslice.Map(exposed.Surfaces.GetRoles(), func(role annotationv1.Role) string {
			return strings.ToLower(strings.TrimPrefix(role.String(), "ROLE_"))
		})

		doc.Services[at].Methods = append(doc.Services[at].Methods, methodDoc{
			Slug:        strings.ReplaceAll(tool, "_", "-"),
			Name:        string(exposed.Method.Name()),
			Path:        exposed.GetPath(),
			Command:     "komrad rpc " + strings.ReplaceAll(tool, "_", "-"),
			Tool:        tool,
			MCP:         exposed.Surfaces.GetMcp(),
			Roles:       roles,
			Summary:     strings.TrimSpace(firstSentence.FindString(description)),
			Description: description,
			ReadOnly:    readOnly,
			Idempotent:  rpcapi.IsIdempotent(exposed.Method, exposed.Surfaces),
			Destructive: rpcapi.IsDestructive(exposed.Method, exposed.Surfaces),
			Request:     string(exposed.Method.Input().FullName()),
			Response:    string(exposed.Method.Output().FullName()),
		})

		addMessage(doc, notes, exposed.Method.Input())
		addMessage(doc, notes, exposed.Method.Output())
	}

	return doc
}

func addMessage(doc apiDoc, notes comments, message protoreflect.MessageDescriptor) {
	name := string(message.FullName())
	if _, seen := doc.Messages[name]; seen {
		return
	}

	entry := messageDoc{
		Name:        string(message.Name()),
		Description: notes.of(message),
		Fields:      []fieldDoc{},
		Oneofs:      []oneofDoc{},
	}
	doc.Messages[name] = entry

	for i := range message.Oneofs().Len() {
		oneof := message.Oneofs().Get(i)
		if oneof.IsSynthetic() {
			continue
		}

		entry.Oneofs = append(entry.Oneofs, oneofDoc{
			Name:        string(oneof.Name()),
			Description: notes.of(oneof),
			Required:    rpcapi.IsOneofRequired(oneof),
		})
	}

	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)

		described := fieldDoc{
			Name:        string(field.Name()),
			Repeated:    field.IsList(),
			Map:         field.IsMap(),
			Required:    rpcapi.IsRequired(field),
			Constraints: rpcapi.GetConstraints(field),
			Description: notes.of(field),
		}

		if oneof := field.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
			described.Oneof = string(oneof.Name())
		}

		value := field
		if field.IsMap() {
			value = field.MapValue()
		}

		described.Type, described.Ref = fieldType(value)
		if rpcapi.IsUUID(field) {
			described.Type = "uuid"
		}

		switch {
		case value.Kind() == protoreflect.EnumKind:
			addEnum(doc, notes, value.Enum())
		case value.Message() != nil && !isWellKnown(value.Message()):
			addMessage(doc, notes, value.Message())
		}

		entry.Fields = append(entry.Fields, described)
	}

	doc.Messages[name] = entry
}

func addEnum(doc apiDoc, notes comments, enum protoreflect.EnumDescriptor) {
	name := string(enum.FullName())
	if _, seen := doc.Enums[name]; seen {
		return
	}

	entry := enumDoc{
		Name:        string(enum.Name()),
		Description: notes.of(enum),
		Values:      []enumValueDoc{},
	}
	for i := range enum.Values().Len() {
		value := enum.Values().Get(i)
		if value.Number() == 0 {
			continue
		}

		entry.Values = append(entry.Values, enumValueDoc{Name: protoenum.NameOf(value), Description: cmp.Or(notes.of(value), protoenum.LabelOf(value).GetDescription())})
	}

	doc.Enums[name] = entry
}

func fieldType(field protoreflect.FieldDescriptor) (string, string) {
	switch field.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if isWellKnown(field.Message()) {
			return wellKnownName(field.Message()), ""
		}

		return string(field.Message().Name()), string(field.Message().FullName())
	case protoreflect.EnumKind:
		return string(field.Enum().Name()), string(field.Enum().FullName())
	case protoreflect.BoolKind, protoreflect.StringKind, protoreflect.BytesKind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind,
		protoreflect.FloatKind, protoreflect.DoubleKind:
		return field.Kind().String(), ""
	}

	return "", ""
}

func isWellKnown(message protoreflect.MessageDescriptor) bool {
	return message.ParentFile().Package() == "google.protobuf"
}

func wellKnownName(message protoreflect.MessageDescriptor) string {
	switch message.Name() {
	case "Timestamp":
		return "timestamp"
	case "Duration":
		return "duration"
	case "Struct", "Value":
		return "json"
	default:
		return strings.ToLower(string(message.Name()))
	}
}

var wordBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func kebab(name string) string {
	return strings.ToLower(wordBoundary.ReplaceAllString(name, "$1-$2"))
}

func splitWords(name string) string {
	return wordBoundary.ReplaceAllString(name, "$1 $2")
}
