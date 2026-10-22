// Package pane learns what an agent is doing by reading what the terminal it runs in draws.
//
// It sits behind harness.Observer, and each harness reads its own frames. Komrad says things to
// an agent through the harness's channel rather than the terminal, and the agent still runs in a
// pane a person can attach to and take over.
package pane
