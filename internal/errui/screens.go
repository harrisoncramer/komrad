package errui

// Config is shown instead of the panel when the user's config file could not be
// read. It is a different kind of failure from the service being down: it cannot
// fix itself, and the file it is complaining about is the one holding the keys the
// panel would otherwise tell you to press.
//
// It replaces printing to stderr and exiting. That wrote the message to a popup
// tmux destroyed in the same instant, so the only way to read it was to run the
// binary by hand.
func Config(err error) string {
	return Screen{
		Title: "Your configuration could not be read",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and",
			"the file is only read at startup, so nothing else has acted on it.",
		},
		Details: []Detail{{Label: "error", Value: err.Error()}},
		Hints: []string{
			"Fix the file, then reopen the panel.",
			"Press any key to close.",
		},
	}.Render()
}

// Startup is shown instead of the panel when the server it works through could not
// be started. Its most common cause by far is a Postgres that is not up, so the
// hints name the way to bring it up rather than leaving a refused connection as the
// last word on screen.
//
// It is a takeover rather than the unreachable screen because that one waits for a
// server that is coming back. This one has nothing to wait for: the panel starts a
// server once, and if that attempt failed nothing tries again.
func Startup(err error) string {
	return Screen{
		Title: "The agent server could not be started",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and none",
			"of them were stopped: the server is only how the panel reads and changes them.",
		},
		Details: []Detail{{Label: "error", Value: err.Error()}},
		Hints: []string{
			"A refused connection usually means the database is not running: mise run docker:up",
			"Press any key to close.",
		},
	}.Render()
}

// Unreachable is shown in place of every other screen while the service is not
// answering.
//
// It takes over rather than sitting in a corner because the panel is a view onto
// the service. With the service gone the agent list is a stale snapshot, the counts
// are wrong, and every key that acts on an agent fails. Left on screen, a dead
// service reads as an empty store.
//
// There is nothing to press. The list keeps polling underneath, so the panel
// returns to itself within a poll of the service coming back.
func Unreachable(addr string, err error) string {
	screen := Screen{
		Title: "Cannot reach the server",
		Body: []string{
			"The panel could not list your agents.",
		},
		Hints: []string{
			"Start it with: weave serve",
			"This screen clears itself once the server answers.",
		},
	}

	if addr != "" {
		screen.Details = append(screen.Details, Detail{Label: "address", Value: addr})
	}
	if err != nil {
		screen.Details = append(screen.Details, Detail{Label: "error", Value: err.Error()})
	}

	return screen.Render()
}
