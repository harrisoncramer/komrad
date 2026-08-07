package errui

// Finding is one thing the doctor found wrong, restated here rather than imported so
// drawing a screen doesn't pull the checks and their database handles in behind it.
type Finding struct {
	Name   string
	Reason string
	Fix    string
}

// Config is shown instead of the panel when the user's config file could not be read.
// Unlike the service being down, it can't fix itself, and the file it's complaining about
// is the one holding the keys the panel would otherwise tell you to press.
//
// Printing to stderr and exiting instead would write to a popup tmux destroys in the same
// instant, leaving the binary run by hand as the only way to read it.
func Config(width int, err error) string {
	return Screen{
		Width: width,
		Title: "Your configuration could not be read",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and the file is only read at startup, so nothing else has acted on it.",
		},
		Details: []Detail{{Label: "error", Value: err.Error()}},
		Hints: []string{
			"Fix the file, then reopen the panel.",
			"Press any key to close.",
		},
	}.Render()
}

// Startup is shown instead of the panel when the server it works through could not be
// started. The reason the server gave is a log tail saying what failed rather than what to
// do about it; the doctor's findings sit alongside it, naming the dependency and the
// command that repairs it.
//
// It's a takeover rather than the unreachable screen, which waits for a server that's
// coming back, since the panel starts a server only once and nothing retries if that
// attempt failed.
func Startup(width int, err error, findings []Finding) string {
	screen := Screen{
		Width: width,
		Title: "The agent server could not be started",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and none of them were stopped: the server is only how the panel reads and changes them.",
		},
	}

	// Ahead of the error, since these name the dependency behind it and whichever comes
	// first is what gets read.
	for _, finding := range findings {
		screen.Details = append(screen.Details, Detail{Label: finding.Name, Value: finding.Reason})
		if finding.Fix != "" {
			screen.Hints = append(screen.Hints, finding.Name+": "+finding.Fix)
		}
	}

	screen.Details = append(screen.Details, Detail{Label: "error", Value: err.Error()})

	// With nothing found, the log tail is all anyone has to act on.
	if len(findings) == 0 {
		screen.Hints = append(screen.Hints, "Run weave doctor for what this machine is missing.")
	}

	screen.Hints = append(screen.Hints, "Press any key to close.")

	return screen.Render()
}

// Unreachable is shown in place of every other screen while the service is not answering.
// It takes over rather than sitting in a corner because the panel is a view onto the
// service: with the service gone the agent list is a stale snapshot, the counts are wrong,
// and every key that acts on an agent fails, so left on screen a dead service would read as
// an empty store.
//
// There is nothing to press since the list keeps polling underneath, so the panel returns
// to itself within a poll of the service coming back.
func Unreachable(width int, addr string, err error) string {
	screen := Screen{
		Width: width,
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
