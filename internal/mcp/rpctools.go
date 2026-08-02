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

	annotationv1 "weave/proto/gen/annotation/v1"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/reflect/protoreflect"

	"weave/internal/caller"
	"weave/internal/rpcapi"
)

// callTimeout bounds a single RPC invoked through a tool. Creating an agent opens a tmux
// window and types into it, which is the slowest thing here and still well inside this.
const callTimeout = 30 * time.Second

// rpcTools invokes the service's own Connect endpoint over loopback. Going back through
// HTTP rather than calling the handler directly means a tool call takes the path a real
// client takes, validation interceptor included, so the tool surface cannot drift into
// accepting what the API rejects.
type rpcTools struct {
	baseURL string
	client  *http.Client
}

// registerRPCTools adds one MCP tool per exposed method. What is exposed is rpcapi's to
// say, so widening the tool surface is an edit to the proto rather than a side effect of
// adding an RPC.
func registerRPCTools(server *mcpsdk.Server, baseURL string) error {
	// The transport carries whoever the served request is from onto the call this makes
	// on their behalf, so an RPC reached through a tool is logged as the agent that asked
	// for it.
	tools := &rpcTools{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: callTimeout, Transport: caller.Transport(nil)},
	}

	for exposed := range rpcapi.GetMethods() {
		schema, err := inputSchema(exposed.Method.Input())
		if err != nil {
			return fmt.Errorf("failed to build input schema for %s: %w", exposed.Method.Name(), err)
		}

		mcpsdk.AddTool(server, &mcpsdk.Tool{
			Name:        exposed.Annotation.GetToolName(),
			Description: exposed.Annotation.GetDescription(),
			InputSchema: schema,
			Annotations: toolHints(exposed.Method, exposed.Annotation),
		}, tools.handler(string(exposed.Service.FullName()), string(exposed.Method.Name())))
	}

	return nil
}

// toolHints derives the MCP behavior hints. A read-only tool leaves the destructive hint
// unset, where the MCP spec says it carries no meaning.
func toolHints(method protoreflect.MethodDescriptor, annotation *annotationv1.ToolAnnotation) *mcpsdk.ToolAnnotations {
	readOnly := rpcapi.IsReadOnly(method, annotation)

	hints := &mcpsdk.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: rpcapi.IsIdempotent(method, annotation)}
	if !readOnly {
		destructive := true
		if annotation.Destructive != nil {
			destructive = annotation.GetDestructive()
		}
		hints.DestructiveHint = &destructive
	}

	return hints
}

// inputSchema describes a request message as JSON Schema, so a client knows which
// arguments a tool takes. A field is required when the service would reject it
// empty, which is what the validation rules say rather than what proto3 field
// presence says. A blank title or flags is acceptable, and demanding them would
// make a client send empty strings to satisfy a schema.
func inputSchema(message protoreflect.MessageDescriptor) (map[string]any, error) {
	return messageSchema(message, map[protoreflect.FullName]bool{})
}

// messageSchema describes one message as a JSON Schema object. enclosing holds
// the messages already being described further up, so a message that contains
// itself is refused rather than expanded until the schema runs out of memory.
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

// fieldSchema maps one field to its JSON Schema type. A repeated field becomes an
// array of whatever one of its values is. A map is refused, since nothing here takes
// one.
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

// valueSchema describes one of a field's values, which for a repeated field is one
// element rather than the list. A message becomes a nested object, which is how a
// request carrying pagination is described. Anything else is refused loudly at startup
// rather than advertised to a model as a type it cannot satisfy.
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
	default:
		return nil, fmt.Errorf("field %q has kind %s, which the tool schema does not describe yet", field.Name(), field.Kind())
	}
}

// handler builds the tool's invoker. It posts the arguments to the service's own
// Connect endpoint and hands the response back as text. A non-200 is returned as
// a tool error carrying the server's own message, so a model reads why its call
// was refused rather than a bare status.
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
