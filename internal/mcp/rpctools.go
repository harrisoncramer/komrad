package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"komrad/lib/xhttp"
	annotationv1 "komrad/proto/gen/annotation/v1"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"

	"komrad/internal/caller"
	"komrad/internal/rpcapi"
)

const callTimeout = 30 * time.Second

// connectFailure is the envelope connect answers a failed call with.
type connectFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type rpcTools struct {
	baseURL string
	client  *http.Client
}

func registerRPCTools(server *mcpsdk.Server, baseURL string, role annotationv1.Role) error {
	tools := &rpcTools{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  xhttp.NewClientOver(caller.Transport(rpcapi.NewTransport()), 0),
	}

	for exposed := range rpcapi.GetMethods() {
		if !exposed.Surfaces.GetMcp() || !rpcapi.ServesRole(exposed.Surfaces, role) {
			continue
		}

		schema, err := inputSchema(exposed.Method.Input())
		if err != nil {
			return fmt.Errorf("failed to build input schema for %s: %w", exposed.Method.Name(), err)
		}

		mcpsdk.AddTool(server, &mcpsdk.Tool{
			Name:        exposed.Surfaces.GetName(),
			Description: toolDescription(exposed.Surfaces),
			InputSchema: schema,
			Annotations: toolHints(exposed.Method, exposed.Surfaces),
		}, tools.handler(exposed.GetPath(), exposed.GetCallTimeout(callTimeout)))
	}

	return nil
}

func toolDescription(surfaces *annotationv1.Surfaces) string {
	if surfaces.GetMcpGuidance() == "" {
		return surfaces.GetDescription()
	}

	return surfaces.GetDescription() + "\n\n" + surfaces.GetMcpGuidance()
}

func toolHints(method protoreflect.MethodDescriptor, surfaces *annotationv1.Surfaces) *mcpsdk.ToolAnnotations {
	readOnly := rpcapi.IsReadOnly(method)

	hints := &mcpsdk.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: rpcapi.IsIdempotent(method, surfaces)}
	if !readOnly {
		destructive := rpcapi.IsDestructive(method, surfaces)
		hints.DestructiveHint = &destructive
	}

	return hints
}

func inputSchema(message protoreflect.MessageDescriptor) (map[string]any, error) {
	return messageSchema(message, map[protoreflect.FullName]bool{})
}

func messageSchema(message protoreflect.MessageDescriptor, enclosing map[protoreflect.FullName]bool) (map[string]any, error) {
	if enclosing[message.FullName()] {
		return nil, fmt.Errorf("message %q contains itself, which the tool schema cannot describe", message.FullName())
	}
	enclosing[message.FullName()] = true
	defer delete(enclosing, message.FullName())

	properties := map[string]any{}
	var required []string

	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)

		property, err := fieldSchema(field, enclosing)
		if err != nil {
			return nil, err
		}

		name := string(field.Name())
		properties[name] = property
		if rpcapi.IsRequired(field) {
			required = append(required, name)
		}
	}

	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema, nil
}

func fieldSchema(field protoreflect.FieldDescriptor, enclosing map[protoreflect.FullName]bool) (map[string]any, error) {
	if field.IsMap() {
		return nil, fmt.Errorf("field %q is a map, which the tool schema does not describe yet", field.Name())
	}

	value, err := valueSchema(field, enclosing)
	if err != nil {
		return nil, err
	}

	constraints := rpcapi.GetConstraints(field)
	if !field.IsList() {
		if err := addConstraints(value, constraints); err != nil {
			return nil, err
		}

		return value, nil
	}

	if constraints.Items != nil {
		if err := addConstraints(value, *constraints.Items); err != nil {
			return nil, err
		}
	}

	constraints.Items = nil
	list := map[string]any{"type": "array", "items": value}
	if err := addConstraints(list, constraints); err != nil {
		return nil, err
	}

	return list, nil
}

// addConstraints adds a value's constraints to its schema, describing those JSON Schema has no keyword for.
func addConstraints(schema map[string]any, constraints rpcapi.Constraints) error {
	notes := constraints.Rules
	if constraints.MaxBytes != nil {
		notes = append([]string{fmt.Sprintf("at most %d bytes", *constraints.MaxBytes)}, notes...)
	}
	if len(notes) > 0 {
		schema["description"] = strings.Join(notes, "; ")
	}
	constraints.Rules, constraints.MaxBytes = nil, nil

	keywords, err := json.Marshal(constraints)
	if err != nil {
		return fmt.Errorf("failed to encode constraints: %w", err)
	}

	return json.Unmarshal(keywords, &schema)
}

// enumSchema describes an enum by the names of its values, which connect's JSON
// codec accepts in place of the number.
func enumSchema(values protoreflect.EnumDescriptor) map[string]any {
	return map[string]any{"type": "string", "enum": rpcapi.EnumNames(values)}
}

func valueSchema(field protoreflect.FieldDescriptor, enclosing map[protoreflect.FullName]bool) (map[string]any, error) {
	switch field.Kind() {
	case protoreflect.MessageKind:
		if rpcapi.IsTimestamp(field) {
			return map[string]any{"type": "string", "format": "date-time"}, nil
		}

		return messageSchema(field.Message(), enclosing)
	case protoreflect.StringKind:
		return map[string]any{"type": "string"}, nil
	case protoreflect.BoolKind:
		return map[string]any{"type": "boolean"}, nil
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Uint32Kind, protoreflect.Uint64Kind:
		return map[string]any{"type": "integer"}, nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return map[string]any{"type": "number"}, nil
	case protoreflect.EnumKind:
		return enumSchema(field.Enum()), nil
	case protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind,
		protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind,
		protoreflect.BytesKind, protoreflect.GroupKind:
	}

	return nil, fmt.Errorf("field %q has kind %s, which the tool schema does not describe yet", field.Name(), field.Kind())
}

func (r *rpcTools) handler(path string, timeout time.Duration) func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, any, error) {
	url := r.baseURL + path

	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args map[string]any) (*mcpsdk.CallToolResult, any, error) {
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		if args == nil {
			args = map[string]any{}
		}

		body, err := json.Marshal(args)
		if err != nil {
			return errorResult(fmt.Errorf("failed to encode arguments: %w", err)), nil, nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return errorResult(fmt.Errorf("failed to build request: %w", err)), nil, nil
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := r.client.Do(req)
		if err != nil {
			return errorResult(fmt.Errorf("failed to reach the server: %w", err)), nil, nil
		}
		defer func() { _ = resp.Body.Close() }()

		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return errorResult(fmt.Errorf("failed to read the response: %w", err)), nil, nil
		}

		if resp.StatusCode != http.StatusOK {
			return errorResult(decodeFailure(payload)), nil, nil
		}

		return textResult(string(payload)), nil, nil
	}
}

func decodeFailure(payload []byte) error {
	var failure connectFailure
	if err := json.Unmarshal(payload, &failure); err != nil || failure.Code == "" {
		return errors.New(strings.TrimSpace(string(payload)))
	}

	if failure.Message == "" {
		return errors.New(failure.Code)
	}

	return fmt.Errorf("%s: %s", failure.Code, failure.Message)
}
