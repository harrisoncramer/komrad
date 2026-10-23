// Package doctor checks what komrad needs from the machines it runs on and reports each problem
// in terms a person can act on.
//
// [HereChecks] cover the machine the doctor runs on. They check tmux, the configuration, the
// terminal and its glyphs, the forges, and whether the server answers. [EngineChecks] cover the
// machine the engine runs on. They check tmux's shell, the hooks and worktree directories, the
// database and its schema, orphaned worktrees, and each harness's CLI. The server runs the engine
// checks itself when the doctor asks a remote engine for them. [Run] makes every check and keeps
// going past a failure.
//
// Each of these problems shows up far from its cause, such as a pane that dies on launch when
// tmux's shell is missing. The doctor asks about each one directly.
package doctor
