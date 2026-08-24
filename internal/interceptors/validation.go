package interceptors

import (
	"context"
	"fmt"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

type validationInterceptor struct {
	validator protovalidate.Validator
}

// NewValidation builds the interceptor, failing if the validator cannot be
// constructed. That only happens when a compiled-in constraint is malformed, which
// is a programming error rather than a runtime condition, so the caller cannot
// usefully continue without it.
func NewValidation() (connect.Interceptor, error) {
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

// WrapStreamingClient passes through. The service is unary only, and the
// streaming halves exist so the interceptor still satisfies the interface.
func (v *validationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (v *validationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
