package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Explainer: connect's own JSON codec unmarshals with DiscardUnknown, so a caller that gets a
// field or an enum value slightly wrong is answered rather than corrected: the value is dropped,
// the field keeps its zero value, and the server does whatever that zero value means. Asking for
// the mail an agent sent with "SENT" rather than "MAIL_DIRECTION_SENT" handed back its inbox with
// nothing on stderr, which is worse than an error because the two look alike enough to act on.
// That laxness buys a schema mismatch between client and server, and weave ships both halves in
// one binary, so there is no version skew to forgive and nothing to lose by refusing.

// strictJSON reads a request the way weave wants it read, refusing what connect's codec would
// discard, and writes exactly what that codec writes, since only reading was ever wrong.
type strictJSON struct {
	name string
}

func strictJSONCodecs() []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithCodec(strictJSON{name: "json"}),
		connect.WithCodec(strictJSON{name: "json; charset=utf-8"}),
	}
}

func (c strictJSON) Name() string { return c.name }

func (c strictJSON) IsBinary() bool { return false }

func (c strictJSON) Marshal(message any) ([]byte, error) {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return nil, notProto(message)
	}

	return protojson.MarshalOptions{}.Marshal(protoMessage)
}

func (c strictJSON) MarshalAppend(dst []byte, message any) ([]byte, error) {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return nil, notProto(message)
	}

	return protojson.MarshalOptions{}.MarshalAppend(dst, protoMessage)
}

// MarshalStable is what a GET request is built from, and protojson orders fields consistently
// but not its whitespace, so the output is compacted the way connect's codec compacts it.
func (c strictJSON) MarshalStable(message any) ([]byte, error) {
	encoded, err := c.Marshal(message)
	if err != nil {
		return nil, err
	}

	compact := bytes.NewBuffer(encoded[:0])
	if err := json.Compact(compact, encoded); err != nil {
		return nil, err
	}

	return compact.Bytes(), nil
}

func (c strictJSON) Unmarshal(binary []byte, message any) error {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return notProto(message)
	}

	if len(binary) == 0 {
		return errors.New("zero-length payload is not a valid JSON object")
	}

	if err := (protojson.UnmarshalOptions{}).Unmarshal(binary, protoMessage); err != nil {
		return fmt.Errorf("unmarshal into %T: %w", message, err)
	}

	return nil
}

func notProto(message any) error {
	return fmt.Errorf("%T does not implement proto.Message", message)
}
