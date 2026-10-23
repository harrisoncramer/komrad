// Package tmuxtest runs a real tmux server for a test on a socket of its own, killed when the test
// ends. A fake tmux would only agree with what komrad already assumes, so tests spawn panes, type
// into them and read frames back for real.
package tmuxtest
