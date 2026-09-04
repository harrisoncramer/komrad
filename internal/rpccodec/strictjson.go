// Package rpccodec holds the codecs weave hangs on its connect handlers.
package rpccodec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	nameJSON            = "json"
	nameJSONCharsetUTF8 = nameJSON + "; charset=utf-8"
)

// NewStrictJSON replaces connect's JSON codec with one that refuses a request naming a field or an
// enum value the schema does not have, which connect discards so a client and a server on different
// versions of a schema still talk, at the cost of answering a misspelled request as if it were
// another one weave does understand.
func NewStrictJSON() connect.HandlerOption {
	return connect.WithHandlerOptions(
		connect.WithCodec(strictJSON{name: nameJSON}),
		connect.WithCodec(strictJSON{name: nameJSONCharsetUTF8}),
	)
}

type strictJSON struct {
	name string
}

func (c strictJSON) Name() string { return c.name }

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

func (c strictJSON) Unmarshal(binary []byte, message any) error {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return notProto(message)
	}

	if len(binary) == 0 {
		return errors.New("a zero-length payload is not a valid JSON object")
	}

	if err := (protojson.UnmarshalOptions{}).Unmarshal(binary, protoMessage); err != nil {
		return fmt.Errorf("failed to read the request as %T: %w", message, err)
	}

	return nil
}

func (c strictJSON) MarshalStable(message any) ([]byte, error) {
	encoded, err := c.Marshal(message)
	if err != nil {
		return nil, err
	}

	var compacted bytes.Buffer
	if err := json.Compact(&compacted, encoded); err != nil {
		return nil, err
	}

	return compacted.Bytes(), nil
}

func (c strictJSON) IsBinary() bool { return false }

func notProto(message any) error {
	return fmt.Errorf("%T is not a protobuf message", message)
}
