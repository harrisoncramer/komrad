// Package server assembles the running engine: the stores over one pool, the domain services
// over those, the ConnectRPC and MCP handlers over those, the watch broker, and the background
// loops.
//
// Each loop runs on an interval, and sooner when woken, such as the reconciler, mail delivery,
// the cwd sync, the cascade and the pull request sync. The server owns the wiring and the
// shutdown, so the packages it assembles take their dependencies instead of finding them.
package server
