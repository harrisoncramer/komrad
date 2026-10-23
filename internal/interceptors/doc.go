// Package interceptors holds what every request to the server passes through before a handler
// sees it. That covers the log line, panic recovery, the protovalidate constraints in the proto,
// and the role check that keeps an agent to the methods its role is offered. The package also
// holds the guard that refuses a non-loopback Host or Origin on a local server, and the sign-in
// check on a remote engine.
package interceptors
