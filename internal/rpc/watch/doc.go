// Package watch serves WatchService. It opens a subscription on the watch broker, sends the
// snapshot and then each change, and sends a heartbeat when the stream has been quiet a while.
package watch
