package errui

// Config is shown instead of the panel when the user's config file could not be
// read. It is a different kind of failure from the service being down: it cannot
// fix itself, and the file it is complaining about is the one holding the keys the
// panel would otherwise tell you to press.
//
// It replaces printing to stderr and exiting. That wrote the message to a popup
// tmux destroyed in the same instant, so the only way to read it was to run the
// binary by hand.
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
// started.
//
// The reason the server gave is the tail of a log, which says what failed and not what to
// do about it. So the screen carries the doctor's findings underneath, and those name the
// dependency and the command that repairs it. Without them this screen could only guess,
// and the guess it used to make was that the database was down, which is wrong exactly as
// often as the readonly login is the thing that is missing.
//
// It is a takeover rather than the unreachable screen because that one waits for a server
// that is coming back. This one has nothing to wait for: the panel starts a server once,
// and if that attempt failed nothing tries again.
func Startup(width int, err error, findings []Finding) string {
	screen := Screen{
		Width: width,
		Title: "The agent server could not be started",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and none of them were stopped: the server is only how the panel reads and changes them.",
		},
	}

	// Ahead of the error, since the error is a log line the server wrote about itself
	// and these name the dependency behind it. Whichever is first is what gets read.
	for _, finding := range findings {
		screen.Details = append(screen.Details, Detail{Label: finding.Name, Value: finding.Reason})
		if finding.Fix != "" {
			screen.Hints = append(screen.Hints, finding.Name+": "+finding.Fix)
		}
	}

	screen.Details = append(screen.Details, Detail{Label: "error", Value: err.Error()})

	// Only when nothing was found, since a run that turned up no failing dependency
	// leaves the log tail as the only thing anyone can act on.
	if len(findings) == 0 {
		screen.Hints = append(screen.Hints, "Run weave doctor for what this machine is missing.")
	}

	screen.Hints = append(screen.Hints, "Press any key to close.")

	return screen.Render()
}

// Finding is one thing the doctor found wrong, in the little this package needs of it.
// It is restated here rather than taken from the doctor, so drawing a screen does not
// pull the checks and their database handles in behind it.
type Finding struct {
	Name   string
	Reason string
	Fix    string
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
