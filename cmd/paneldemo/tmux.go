package main

import (
	"os"
	"syscall"

	"weave/internal/panes"

	"golang.org/x/term"
)

// demoPane is the pane the panel itself draws in, which is the terminal the demo was started in.
const demoPane = "%0"

// Explainer: the panel draws itself from the size bubbletea reports and asks tmux only about the
// panes beside it, so answering that there are none puts every screen into the branch that draws
// both columns itself. An agent's own terminal on the stage is another process and cannot be
// faked, so the screens that put one there draw their sidebar and nothing beside it.

// noPanes answers the panel's questions about tmux as though it holds the window alone.
type noPanes struct{}

func (noPanes) MovePanes([]panes.Move)       {}
func (noPanes) IsPaneAlive(pane string) bool { return pane == demoPane }

// GetPaneWidth and GetPaneHeight answer for the panel's own pane and for nothing else.
func (noPanes) GetPaneWidth(pane string) int {
	if pane != demoPane {
		return 0
	}

	width, _ := terminalSize()

	return width
}

func (noPanes) GetPaneHeight(pane string) int {
	if pane != demoPane {
		return 0
	}

	_, height := terminalSize()

	return height
}

func (noPanes) SplitRight(string, string, string) string        { return "" }
func (noPanes) SplitLeft(string, string, string) string         { return "" }
func (noPanes) SplitBelow(string, string, string) string        { return "" }
func (noPanes) SplitAbove(string, string, string) string        { return "" }
func (noPanes) ResizePane(string, string)                       {}
func (noPanes) GetPaneFocus(pane string) (focused, zoomed bool) { return pane == demoPane, false }
func (noPanes) GetSiblingPanes(string) []string                 { return nil }
func (noPanes) BreakPane(string)                                {}
func (noPanes) KillPane(string)                                 {}
func (noPanes) GetPaneOwner(string) string                      { return "" }
func (noPanes) ResizeWindow(string, int, int)                   {}
func (noPanes) IsPaneRunningCLI(string) bool                    { return false }
func (noPanes) SetSessionOption(string, string, string)         {}
func (noPanes) GetHostPath() string                             { return "" }
func (noPanes) GetDefaultShell() string                         { return "/bin/sh" }
func (noPanes) SetInfoPane(string)                              {}
func (noPanes) SelectPane(string)                               {}
func (noPanes) SendKeys(string, ...string) error                { return nil }
func (noPanes) SetGlobal(string, string)                        {}
func (noPanes) GetSessionClient(string) string                  { return "" }
func (noPanes) BindKey(string, ...string)                       {}
func (noPanes) BindRootKey(string, ...string)                   {}
func (noPanes) UnbindRootKey(string)                            {}

// Explainer: the panel's quit key detaches the tmux client and leaves the panel running behind
// it, which is why quitting a real weave does not stop it. There is no client here to detach and
// nothing to leave running, so the same key has to end the process instead.

// DetachSession is what the panel's quit key reaches.
func (noPanes) DetachSession(string) {
	// Signalled rather than exited so bubbletea puts the terminal back as it found it.
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		os.Exit(1)
	}
}

// terminalSize is the terminal the demo is drawn in, falling back to a wide one where stdout is
// not one.
func terminalSize() (width, height int) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 || height <= 0 {
		return 120, 36
	}

	return width, height
}

// noFrames says no pane has drawn a harness, which follows from there being no panes.
type noFrames struct{}

func (noFrames) DoesFrameShowHarness(string, string) bool { return false }
