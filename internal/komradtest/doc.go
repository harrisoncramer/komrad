// Package komradtest runs a whole komrad in a test. It brings up its own database, its own tmux
// server and a server over both, with the loops driven by hand instead of by the clock. The
// scenario tests in e2e use it to launch an agent for real, make its harness look busy, run one
// reconciler pass, and check what was written and what reached each agent.
//
// A pane can outlive the test that opened it, and its CLI may write into its harness root after
// the test ends. The harness root is therefore a directory komradtest removes itself, tolerating a
// late write, and the temp directories are claimed before the tmux server so the panes die first.
package komradtest
