// Package interceptors holds the log line, panic recovery, protovalidate constraints and role
// check that every request to the server passes through before a handler sees it. The role check
// keeps an agent to the methods its role is offered. The package also holds the guard that refuses
// a non-loopback Host or Origin on a local server, and the sign-in check on a remote engine.
package interceptors
