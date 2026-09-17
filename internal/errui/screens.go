package errui

import "errors"

// ErrEngineBuild marks a startup failure that is this binary rather than the engine it dialled.
var ErrEngineBuild = errors.New("the engine is running code this panel was not built from")

type Finding struct {
	Name   string
	Reason string
	Fix    string
}

// RenderConfigError is shown instead of the panel when the config file could not be read.
func RenderConfigError(width int, err error) string {
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

// RenderStartupError is shown instead of the panel when it has no server it can talk to.
func RenderStartupError(width int, err error, findings []Finding) string {
	screen := Screen{
		Width: width,
		Title: "The agent server could not be started",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and none of them were stopped: the server is only how the panel reads and changes them.",
		},
	}

	if errors.Is(err, ErrEngineBuild) {
		screen.Title = "The engine is running different code"
		screen.Body = []string{
			"The panel did not start. The engine is up and every agent on it is still running, so there is nothing to restart over there: what does not match is the weave on this machine.",
		}
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
