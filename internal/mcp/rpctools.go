package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"komrade/lib/xhttp"
	annotationv1 "komrade/proto/gen/annotation/v1"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"

	"komrade/internal/caller"
	"komrade/internal/rpcapi"
)

const callTimeout = 30 * time.Second

type rpcTools struct {
	baseURL string
	client  *http.Client
}

func registerRPCTools(server *mcpsdk.Server, baseURL string, role annotationv1.Role) error {
	tools := &rpcTools{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  xhttp.NewClientOver(caller.Transport(xhttp.NewTransport()), callTimeout),
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
			Description: exposed.Surfaces.GetDescription(),
			InputSchema: schema,
			Annotations: toolHints(exposed.Method, exposed.Surfaces),
		}, tools.handler(string(exposed.Service.FullName()), string(exposed.Method.Name())))
	}

	return nil
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

		name := field.JSONName()
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

	if field.IsList() {
		return map[string]any{"type": "array", "items": value}, nil
	}

	return value, nil
}

// enumSchema describes an enum by the names its values are spelled with, which connect's JSON
// codec takes in place of the number.
func enumSchema(values protoreflect.EnumDescriptor) map[string]any {
	return map[string]any{"type": "string", "enum": rpcapi.EnumNames(values)}
}

func valueSchema(field protoreflect.FieldDescriptor, enclosing map[protoreflect.FullName]bool) (map[string]any, error) {
	switch field.Kind() {
	case protoreflect.MessageKind:
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

func (r *rpcTools) handler(service, method string) func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, any, error) {
	url := fmt.Sprintf("%s/%s/%s", r.baseURL, service, method)

	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args map[string]any) (*mcpsdk.CallToolResult, any, error) {
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
			return errorResult(fmt.Errorf("failed to reach the agent service: %w", err)), nil, nil
		}
		defer func() { _ = resp.Body.Close() }()

		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return errorResult(fmt.Errorf("failed to read the response: %w", err)), nil, nil
		}

		if resp.StatusCode != http.StatusOK {
			return errorResult(fmt.Errorf("%s", strings.TrimSpace(string(payload)))), nil, nil
		}

		return textResult(string(payload)), nil, nil
	}
}
