package errui

type Finding struct {
	Name   string
	Reason string
	Fix    string
}

// Config is shown instead of the panel when the config file could not be read, since
// printing to stderr would write to a popup tmux destroys in the same instant.
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

// Startup is shown instead of the panel when the server could not be started, as a takeover
// rather than the unreachable screen since nothing retries a failed first attempt.
func Startup(width int, err error, findings []Finding) string {
	screen := Screen{
		Width: width,
		Title: "The agent server could not be started",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and none of them were stopped: the server is only how the panel reads and changes them.",
		},
	}

	for _, finding := range findings {
		screen.Details = append(screen.Details, Detail{Label: finding.Name, Value: finding.Reason})
		if finding.Fix != "" {
			screen.Hints = append(screen.Hints, finding.Name+": "+finding.Fix)
		}
	}

	screen.Details = append(screen.Details, Detail{Label: "error", Value: err.Error()})

	if len(findings) == 0 {
		screen.Hints = append(screen.Hints, "Run weave doctor for what this machine is missing.")
	}

	screen.Hints = append(screen.Hints, "Press any key to close.")

	return screen.Render()
}

// Unreachable takes over every other screen while the service is not answering, since a
// stale agent list left on screen would read as an empty store.
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
