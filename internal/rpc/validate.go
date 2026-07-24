package rpc

import (
	"context"
	"fmt"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

// validationInterceptor enforces the constraints declared on the request
// messages in session.proto, so what counts as a well-formed request lives with
// the schema rather than being re-litigated at the top of every handler. A
// violation is an invalid_argument, carrying protovalidate's own description of
// which field failed and why.
type validationInterceptor struct {
	validator protovalidate.Validator
}

// NewValidationInterceptor builds the interceptor, failing if the validator
// cannot be constructed. That only happens when a compiled-in constraint is
// malformed, which is a programming error rather than a runtime condition, so
// the caller cannot usefully continue without it.
func NewValidationInterceptor() (connect.Interceptor, error) {
	validator, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("failed to build request validator: %w", err)
	}

	return &validationInterceptor{validator: validator}, nil
}

func (v *validationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		msg, ok := req.Any().(proto.Message)
		if !ok {
			return next(ctx, req)
		}

		if err := v.validator.Validate(msg); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}

		return next(ctx, req)
	}
}

// The service is unary only. The streaming halves pass through so the
// interceptor still satisfies the interface.
func (v *validationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (v *validationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
