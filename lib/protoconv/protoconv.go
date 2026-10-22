// Package protoconv converts the protobuf values protovalidate rules describe into the Go types
// they stand for, accepting exactly what each rule accepts.
package protoconv

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	hyphenatedUUIDLen = 36
	trimmedUUIDLen    = 32
	binaryUUIDLen     = 16
)

// ErrMissing is a value that must be present and is not.
var ErrMissing = errors.New("value is required")

// ErrDuplicateKey is a map key that converts to the same value as another key in the map.
var ErrDuplicateKey = errors.New("key converts to the same value as another key")

var (
	errNotUUID        = errors.New("must be a valid UUID")
	errNotTrimmedUUID = errors.New("must be a valid trimmed UUID")
	errNotTimestamp   = errors.New("must be a valid timestamp")
)

// Error is a value that would not convert, named by the path to its field.
type Error struct {
	Path string
	Err  error
}

// Error names the field and says what was wrong with its value.
func (e *Error) Error() string {
	return e.Path + ": " + e.Err.Error()
}

// Unwrap returns what was wrong with the value.
func (e *Error) Unwrap() error {
	return e.Err
}

// UUID converts the hyphenated form protovalidate's string.uuid rule accepts.
func UUID(value string) (uuid.UUID, error) {
	if len(value) != hyphenatedUUIDLen {
		return uuid.Nil, errNotUUID
	}

	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, errNotUUID
	}

	return parsed, nil
}

// TrimmedUUID converts the form without hyphens that protovalidate's string.tuuid rule accepts.
func TrimmedUUID(value string) (uuid.UUID, error) {
	if len(value) != trimmedUUIDLen {
		return uuid.Nil, errNotTrimmedUUID
	}

	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, errNotTrimmedUUID
	}

	return parsed, nil
}

// BytesUUID converts the 16 bytes protovalidate's bytes.uuid rule accepts.
func BytesUUID(value []byte) (uuid.UUID, error) {
	if len(value) != binaryUUIDLen {
		return uuid.Nil, errNotUUID
	}

	parsed, err := uuid.FromBytes(value)
	if err != nil {
		return uuid.Nil, errNotUUID
	}

	return parsed, nil
}

// Time converts a timestamp, refusing one that is missing or outside the range a timestamp may hold.
func Time(timestamp *timestamppb.Timestamp) (time.Time, error) {
	if timestamp == nil {
		return time.Time{}, ErrMissing
	}

	if err := timestamp.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errNotTimestamp, err)
	}

	return timestamp.AsTime(), nil
}

// Field puts the name of the field err came from in front of the path err already carries.
func Field(name string, err error) error {
	return prefix(name, err)
}

// Index puts the position of the list element err came from in front of the path err already carries.
func Index(index int, err error) error {
	return prefix("["+strconv.Itoa(index)+"]", err)
}

// Key puts the key of the map entry err came from in front of the path err already carries.
func Key[K comparable](key K, err error) error {
	if text, ok := any(key).(string); ok {
		return prefix("["+strconv.Quote(text)+"]", err)
	}

	return prefix(fmt.Sprintf("[%v]", key), err)
}

func prefix(segment string, err error) error {
	var failure *Error
	if !errors.As(err, &failure) {
		return &Error{Path: segment, Err: err}
	}

	if strings.HasPrefix(failure.Path, "[") {
		return &Error{Path: segment + failure.Path, Err: failure.Err}
	}

	return &Error{Path: segment + "." + failure.Path, Err: failure.Err}
}
