// Package client holds the calls the panel and the CLI make that take more than one request.
// It pages through a listing or a transcript to its end, and [Watch] opens a fresh watch stream
// whenever the last one closes or goes quiet.
package client
