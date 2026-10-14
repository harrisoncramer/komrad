package errui

import "errors"

// ErrRebuilt marks a panel whose binary was replaced while it was running.
var ErrRebuilt = errors.New("komrad was rebuilt")

type Finding struct {
	Name   string
	Reason string
	Fix    string
}

// RenderConfigError is shown instead of the panel when the config file could not be read.
func RenderConfigError(width int, err error) string {
	return screen{
		Width: width,
		Title: "Your configuration could not be read",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and the file is only read at startup, so nothing else has acted on it.",
		},
		Details: []detail{{Label: "error", Value: err.Error()}},
		Hints: []string{
			"Fix the file, then reopen the panel.",
			"Press any key to close.",
		},
	}.Render()
}

// RenderStartupError is shown instead of the panel when it has no server it can talk to.
func RenderStartupError(width int, err error, findings []Finding) string {
	screen := screen{
		Width: width,
		Title: "The server could not be started",
		Body: []string{
			"The panel did not start. Nothing about your agents has changed, and none of them were stopped: the server is only how the panel reads and changes them.",
		},
	}

	if errors.Is(err, ErrRebuilt) {
		screen.Title = "komrad was rebuilt"
		screen.Body = []string{
			"The binary this panel was started from has changed, so this panel has stopped rendering. Nothing about your agents has changed, and none of them were stopped. Run komrad open to start a panel from the new build.",
		}
	}

	for _, finding := range findings {
		screen.Details = append(screen.Details, detail{Label: finding.Name, Value: finding.Reason})
		if finding.Fix != "" {
			screen.Hints = append(screen.Hints, finding.Name+": "+finding.Fix)
		}
	}

	screen.Details = append(screen.Details, detail{Label: "error", Value: err.Error()})

	if len(findings) == 0 && !errors.Is(err, ErrRebuilt) {
		screen.Hints = append(screen.Hints, "Run komrad doctor for what this machine is missing.")
	}

	screen.Hints = append(screen.Hints, "Press any key to close.")

	return screen.Render()
}

// RenderSignInChecking is shown instead of the panel while it asks an engine at a url whether the
// person is signed in with access.
func RenderSignInChecking(width int, url string) string {
	return screen{
		Width: width,
		Title: "Checking your sign-in",
		Body: []string{
			"The panel is asking the engine at " + url + " whether you are signed in and have access. It shows nothing from the engine until the engine says yes.",
		},
		Hints: []string{"Press ctrl+c to close."},
	}.Render()
}

// RenderSignInNeededParams is the engine to sign in to and how far the person has got.
type RenderSignInNeededParams struct {
	Width       int
	URL         string
	AuthURL     string
	Err         error
	IsLoggingIn bool
}

// RenderSignInNeeded is shown instead of the panel when the person has to sign in to an engine at
// a url before it shows anything.
func RenderSignInNeeded(params RenderSignInNeededParams) string {
	screen := screen{
		Width: params.Width,
		Title: "Sign in to the engine",
		Body: []string{
			"The engine at " + params.URL + " needs you to sign in before the panel shows anything from it.",
		},
	}

	if params.IsLoggingIn {
		screen.Body = append(screen.Body, "Finish signing in with your browser. If it did not open, visit the address below.")
		if params.AuthURL != "" {
			screen.Details = append(screen.Details, detail{Label: "sign in at", Value: params.AuthURL})
		}
		screen.Hints = append(screen.Hints, "Press ctrl+c to close.")

		return screen.Render()
	}

	if params.Err != nil {
		screen.Details = append(screen.Details, detail{Label: "error", Value: params.Err.Error()})
	}
	screen.Hints = append(screen.Hints, "Press enter to sign in with your browser.", "Press ctrl+c to close.")

	return screen.Render()
}

// RenderAccessDenied is shown instead of the panel when the person is signed in to an engine at a
// url that has not granted them access.
func RenderAccessDenied(width int, url string, err error) string {
	return screen{
		Width: width,
		Title: "You do not have access to this engine",
		Body: []string{
			"You are signed in, but nobody has granted your account access to the engine at " + url + ". Ask the engine's owner to grant your email access.",
		},
		Details: []detail{{Label: "error", Value: err.Error()}},
		Hints: []string{
			"Press enter to check again once the owner grants you access.",
			"Press ctrl+c to close.",
		},
	}.Render()
}

// RenderAccessFailure is shown instead of the panel when it cannot tell whether the person has
// access to an engine at a url, because the engine or the provider did not answer.
func RenderAccessFailure(width int, url string, err error) string {
	return screen{
		Width: width,
		Title: "The panel could not check your sign-in",
		Body: []string{
			"The engine at " + url + " or the provider you sign in with did not answer, so the panel cannot tell whether you have access. It shows nothing from the engine until it can.",
		},
		Details: []detail{{Label: "error", Value: err.Error()}},
		Hints: []string{
			"Press enter to try again.",
			"Press ctrl+c to close.",
		},
	}.Render()
}
