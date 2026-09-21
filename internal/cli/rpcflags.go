package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"weave/internal/rpcapi"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const nestingLimit = 2

type requestFields struct {
	bound   []boundField
	skipped []string
	// exclusive groups the flags of one oneof.
	exclusive [][]string
	// groups is where each oneof's flags are being collected, by that oneof's place in the request.
	groups map[string]int
}

type boundField struct {
	flag string
	path []string
	read func(*pflag.FlagSet) (any, error)
}

type fieldLocation struct {
	path []string
	name []string
}

func (l fieldLocation) descend(field protoreflect.FieldDescriptor) fieldLocation {
	return fieldLocation{
		path: append(append([]string{}, l.path...), field.JSONName()),
		name: append(append([]string{}, l.name...), strings.ReplaceAll(string(field.Name()), "_", "-")),
	}
}

func bindRequestFlags(cmd *cobra.Command, message protoreflect.MessageDescriptor) *requestFields {
	fields := &requestFields{}
	fields.bind(cmd, message, fieldLocation{})

	for _, group := range fields.exclusive {
		cmd.MarkFlagsMutuallyExclusive(group...)
	}

	return fields
}

func (f *requestFields) bind(cmd *cobra.Command, message protoreflect.MessageDescriptor, at fieldLocation) {
	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)

		into := at.descend(field)
		flag := strings.Join(into.name, ".")

		if field.IsMap() {
			f.skipped = append(f.skipped, flag)

			continue
		}

		if field.Kind() == protoreflect.MessageKind && !field.IsList() {
			if len(into.name) >= nestingLimit {
				f.skipped = append(f.skipped, flag)

				continue
			}

			f.bind(cmd, field.Message(), into)

			continue
		}

		read := declareFlag(cmd, field, flag)
		if read == nil {
			f.skipped = append(f.skipped, flag)

			continue
		}

		f.bound = append(f.bound, boundField{
			flag: flag,
			path: into.path,
			read: read,
		})

		f.recordExclusive(field, at, flag)
	}
}

// recordExclusive files a flag under the oneof it belongs to, keyed by that oneof's place in the
// request rather than by its type, so one message reached twice keeps its two groups apart.
func (f *requestFields) recordExclusive(field protoreflect.FieldDescriptor, at fieldLocation, flag string) {
	oneof := field.ContainingOneof()
	if oneof == nil || oneof.IsSynthetic() {
		return
	}

	key := strings.Join(append(append([]string{}, at.name...), string(oneof.Name())), ".")
	if f.groups == nil {
		f.groups = map[string]int{}
	}

	group, ok := f.groups[key]
	if !ok {
		f.groups[key] = len(f.exclusive)
		f.exclusive = append(f.exclusive, []string{flag})

		return
	}

	f.exclusive[group] = append(f.exclusive[group], flag)
}

func declareFlag(cmd *cobra.Command, field protoreflect.FieldDescriptor, flag string) func(*pflag.FlagSet) (any, error) {
	usage := describeField(field)
	flags := cmd.Flags()

	if field.IsList() {
		return declareListFlag(cmd, field, flag)
	}

	switch field.Kind() {
	case protoreflect.StringKind:
		flags.String(flag, "", usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetString(flag) }
	case protoreflect.BoolKind:
		flags.Bool(flag, false, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetBool(flag) }
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		flags.Int32(flag, 0, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetInt32(flag) }
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		flags.Int64(flag, 0, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetInt64(flag) }
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		flags.Uint32(flag, 0, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetUint32(flag) }
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		flags.Uint64(flag, 0, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetUint64(flag) }
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		flags.Float64(flag, 0, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetFloat64(flag) }
	case protoreflect.EnumKind:
		flags.String(flag, "", usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetString(flag) }
	case protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
		return nil
	default:
		return nil
	}
}

func declareListFlag(cmd *cobra.Command, field protoreflect.FieldDescriptor, flag string) func(*pflag.FlagSet) (any, error) {
	usage := describeField(field) + ", repeatable"
	flags := cmd.Flags()

	switch field.Kind() {
	case protoreflect.StringKind, protoreflect.EnumKind:
		flags.StringSlice(flag, nil, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetStringSlice(flag) }
	case protoreflect.BoolKind:
		flags.BoolSlice(flag, nil, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetBoolSlice(flag) }
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		flags.Int32Slice(flag, nil, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetInt32Slice(flag) }
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		flags.Int64Slice(flag, nil, usage)

		return func(set *pflag.FlagSet) (any, error) { return set.GetInt64Slice(flag) }
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind,
		protoreflect.FloatKind, protoreflect.DoubleKind, protoreflect.BytesKind,
		protoreflect.MessageKind, protoreflect.GroupKind:
		return nil
	default:
		return nil
	}
}

func describeField(field protoreflect.FieldDescriptor) string {
	parts := []string{field.Kind().String()}
	if field.Kind() == protoreflect.EnumKind {
		parts = []string{strings.Join(rpcapi.EnumNames(field.Enum()), "|")}
	}
	if rpcapi.IsRequired(field) {
		parts = append(parts, "required")
	}

	rules := rpcapi.GetRules(field)
	if text := rules.GetString(); text != nil {
		if limit := text.GetMaxLen(); limit > 0 {
			parts = append(parts, fmt.Sprintf("at most %d characters", limit))
		}
		if pattern := text.GetPattern(); pattern != "" {
			parts = append(parts, "matching "+pattern)
		}
	}

	if number := rules.GetInt32(); number != nil {
		if number.GetGte() > 0 || number.GetLte() > 0 {
			parts = append(parts, fmt.Sprintf("between %d and %d", number.GetGte(), number.GetLte()))
		}
	}

	return strings.Join(parts, ", ")
}

func (f *requestFields) request(set *pflag.FlagSet, raw string) (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			return nil, fmt.Errorf("failed to read --request-json: %w", err)
		}
	}

	for _, field := range f.bound {
		if !set.Changed(field.flag) {
			continue
		}

		value, err := field.read(set)
		if err != nil {
			return nil, fmt.Errorf("failed to read --%s: %w", field.flag, err)
		}

		assign(body, field.path, value)
	}

	return body, nil
}

func assign(body map[string]any, path []string, value any) {
	for _, key := range path[:len(path)-1] {
		nested, ok := body[key].(map[string]any)
		if !ok {
			nested = map[string]any{}
			body[key] = nested
		}

		body = nested
	}

	body[path[len(path)-1]] = value
}

func takeFirstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")

	if end := strings.Index(text, ". "); end != -1 {
		return text[:end+1]
	}

	return text
}
