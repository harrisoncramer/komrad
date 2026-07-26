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

	annotationv1 "claude-agents/proto/gen/annotation/v1"
	sessionv1 "claude-agents/proto/gen/session/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// callTimeout bounds a single RPC invoked through a tool. Creating a chat opens
// a tmux window and types into it, which is the slowest thing here and still
// well inside this.
const callTimeout = 30 * time.Second

// rpcTools invokes the service's own Connect endpoint over loopback. Going back
// through HTTP rather than calling the handler directly means a tool call takes
// exactly the path a real client takes, validation interceptor included, so the
// tool surface cannot drift into accepting what the API rejects.
type rpcTools struct {
	baseURL string
	client  *http.Client
}

// registerRPCTools adds one MCP tool per annotated method across every service
// the server exposes. The annotation is the gate: a method without one is not registered, so
// widening the tool surface is an edit to the proto rather than a side effect of
// adding an RPC.
func registerRPCTools(server *mcpsdk.Server, baseURL string) error {
	tools := &rpcTools{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: callTimeout},
	}

	// Every service the server exposes is listed here. A new one is a line in
	// this slice, and its methods appear as tools only if they are annotated.
	files := []protoreflect.FileDescriptor{
		sessionv1.File_session_v1_session_proto,
		workflowv1.File_workflow_v1_workflow_proto,
	}

	for _, file := range files {
		for s := range file.Services().Len() {
			if err := tools.registerService(server, file.Services().Get(s)); err != nil {
				return err
			}
		}
	}

	return nil
}

// registerService adds one tool per annotated method on a service.
func (r *rpcTools) registerService(server *mcpsdk.Server, service protoreflect.ServiceDescriptor) error {
	for i := range service.Methods().Len() {
		method := service.Methods().Get(i)

		annotation := toolAnnotation(method)
		if annotation == nil {
			continue
		}

		schema, err := inputSchema(method.Input())
		if err != nil {
			return fmt.Errorf("failed to build input schema for %s: %w", method.Name(), err)
		}

		mcpsdk.AddTool(server, &mcpsdk.Tool{
			Name:        annotation.GetToolName(),
			Description: annotation.GetDescription(),
			InputSchema: schema,
			Annotations: toolHints(method, annotation),
		}, r.handler(string(service.FullName()), string(method.Name())))
	}

	return nil
}

// toolAnnotation returns the method's tool annotation, or nil when it has none
// and so is not exposed.
func toolAnnotation(method protoreflect.MethodDescriptor) *annotationv1.ToolAnnotation {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(opts, annotationv1.E_Tool) {
		return nil
	}

	annotation, ok := proto.GetExtension(opts, annotationv1.E_Tool).(*annotationv1.ToolAnnotation)
	if !ok || annotation.GetToolName() == "" {
		return nil
	}

	return annotation
}

// toolHints derives the MCP behavior hints, letting the annotation override what
// the method's idempotency level implies. A read-only tool leaves the
// destructive hint unset, where the MCP spec says it carries no meaning.
func toolHints(method protoreflect.MethodDescriptor, annotation *annotationv1.ToolAnnotation) *mcpsdk.ToolAnnotations {
	level := descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN
	if opts, ok := method.Options().(*descriptorpb.MethodOptions); ok {
		level = opts.GetIdempotencyLevel()
	}

	readOnly := level == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
	if annotation.ReadOnly != nil {
		readOnly = annotation.GetReadOnly()
	}

	idempotent := level == descriptorpb.MethodOptions_IDEMPOTENT || level == descriptorpb.MethodOptions_NO_SIDE_EFFECTS
	if annotation.Idempotent != nil {
		idempotent = annotation.GetIdempotent()
	}

	hints := &mcpsdk.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: idempotent}
	if !readOnly {
		destructive := true
		if annotation.Destructive != nil {
			destructive = annotation.GetDestructive()
		}
		hints.DestructiveHint = &destructive
	}

	return hints
}

// inputSchema describes a request message as JSON Schema, so a client knows
// which arguments a tool takes. A field is required when the service would
// reject it empty, which is what the validation rules say and not what proto3
// field presence says: a blank title or flags is perfectly acceptable, and
// demanding them would make a client send empty strings to satisfy a schema.
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
		if rejectsEmpty(field) {
			required = append(required, name)
		}
	}

	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema, nil
}

// rejectsEmpty reports whether the validation rules on a field would refuse an
// empty value, which is what makes it required from a caller's point of view. A
// field the proto marks optional is never required: leaving it out is the whole
// point of the keyword, and its rules apply only once it is present.
func rejectsEmpty(field protoreflect.FieldDescriptor) bool {
	if field.HasOptionalKeyword() {
		return false
	}

	opts, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok || !proto.HasExtension(opts, validate.E_Field) {
		return false
	}

	rules, ok := proto.GetExtension(opts, validate.E_Field).(*validate.FieldRules)
	if !ok {
		return false
	}
	if rules.GetRequired() {
		return true
	}

	// A minimum length is the usual way a string says it cannot be blank. CEL
	// rules are not read: what they accept is only knowable by running them, so a
	// field guarded solely by CEL is advertised as optional and refused by the
	// server if it is genuinely needed.
	return rules.GetString().GetMinLen() > 0
}

// fieldSchema maps one field to its JSON Schema type. A message field becomes a
// nested object, which is how a request carrying pagination is described.
// Anything else is refused loudly at startup rather than advertised to a model
// as a type it cannot satisfy.
func fieldSchema(field protoreflect.FieldDescriptor, enclosing map[protoreflect.FullName]bool) (map[string]any, error) {
	if field.IsList() || field.IsMap() {
		return nil, fmt.Errorf("field %q is a list or map, which the tool schema does not describe yet", field.Name())
	}

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

// handler builds the tool's invoker: it posts the arguments to the service's own
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
			return errorResult(fmt.Errorf("failed to reach the session service: %w", err)), nil, nil
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
