// Package generate writes, for a protobuf message, a Go type holding each value as the type its
// protovalidate rules describe, and a function that converts the message into it. rpcgen uses it
// to generate the request parsers.
package generate
