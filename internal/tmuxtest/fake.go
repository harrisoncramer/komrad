package tmuxtest

import (
	"errors"
	"sync"
	"time"

	"weave/internal/tmux"
)

// Fake is a tmux that answers from its fields and records every call made of it.
type Fake struct {
	mu              sync.Mutex
	Owners          map[string]string
	Captures        map[string]string
	Commands        map[string]string
	InMode          map[string]bool
	ClientDir       string
	Sent            map[string][][]string
	Globals         map[string]string
	NewWindows      map[string]string
	KilledPanes     []string
	Detached        []string
	SessionOptions  map[string]string
	NewWindowErr    error
	BeforeNewWindow func()
	NewWindowCalls  int
	SetOwnerErr     error
	SendKeysErr     error
	PaneOwnersErr   error
	RespawnErr      error
	Shell           string
	Respawned       map[string][]Respawn
}

// Respawn is one respawn of a pane: the directory it started in and the command it ran.
type Respawn struct {
	Dir  string
	Argv []string
}

// NewFake is a tmux holding no panes.
func NewFake() *Fake {
	return &Fake{
		Owners:         map[string]string{},
		Captures:       map[string]string{},
		Commands:       map[string]string{},
		InMode:         map[string]bool{},
		Sent:           map[string][][]string{},
		Globals:        map[string]string{},
		NewWindows:     map[string]string{},
		SessionOptions: map[string]string{},
		Respawned:      map[string][]Respawn{},
		Shell:          "/bin/zsh",
	}
}

// SetGlobal records a global option.
func (f *Fake) SetGlobal(name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Globals[name] = value
}

// ShowGlobal answers a global option.
func (f *Fake) ShowGlobal(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Globals[name]
}

// GetPaneOwner answers the agent Owners names for the pane.
func (f *Fake) GetPaneOwner(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Owners[pane]
}

// CapturePane answers the frame Captures holds for the pane.
func (f *Fake) CapturePane(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Captures[pane]
}

// GetHostPath answers ClientDir.
func (f *Fake) GetHostPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ClientDir
}

// DetachSession records the session.
func (f *Fake) DetachSession(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Detached = append(f.Detached, name)
}

// GetPanes answers every owned pane with its command.
func (f *Fake) GetPanes() (map[string]tmux.PaneStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.PaneOwnersErr != nil {
		return nil, f.PaneOwnersErr
	}

	panes := map[string]tmux.PaneStatus{}
	for pane, owner := range f.Owners {
		panes[pane] = tmux.PaneStatus{Owner: owner, Command: f.Commands[pane]}
	}

	return panes, nil
}

// SetSessionOption records a session option.
func (f *Fake) SetSessionOption(target, name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SessionOptions[target+"/"+name] = value
}

// SendKeys records the keys sent to a target.
func (f *Fake) SendKeys(target string, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.SendKeysErr != nil {
		return f.SendKeysErr
	}
	if target == "" {
		return errors.New("refusing to send keys to an empty target")
	}
	f.Sent[target] = append(f.Sent[target], keys)

	return nil
}

// GetDefaultShell answers Shell.
func (f *Fake) GetDefaultShell() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Shell
}

// RespawnPane records the respawn.
func (f *Fake) RespawnPane(pane, dir string, argv ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.RespawnErr != nil {
		return f.RespawnErr
	}
	if pane == "" {
		return errors.New("refusing to respawn an empty pane")
	}
	f.Respawned[pane] = append(f.Respawned[pane], Respawn{Dir: dir, Argv: argv})

	return nil
}

// LastRespawn is the latest respawn of a pane.
func (f *Fake) LastRespawn(pane string) Respawn {
	f.mu.Lock()
	defer f.mu.Unlock()

	runs := f.Respawned[pane]
	if len(runs) == 0 {
		return Respawn{}
	}

	return runs[len(runs)-1]
}

// SplitRight answers a stage pane.
func (f *Fake) SplitRight(anchorPane, dir string) string {
	return "%stage"
}

// NewWindow opens a pane named for the window.
func (f *Fake) NewWindow(session, name, dir string) (string, error) {
	if f.BeforeNewWindow != nil {
		f.BeforeNewWindow()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.NewWindowErr != nil {
		return "", f.NewWindowErr
	}

	f.NewWindowCalls++

	f.NewWindows[name] = dir
	opened := "%" + name
	f.Owners[opened] = ""

	return opened, nil
}

// WindowsOpened counts the windows opened.
func (f *Fake) WindowsOpened() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.NewWindowCalls
}

// SetPaneOwner records the pane owner.
func (f *Fake) SetPaneOwner(pane, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.SetOwnerErr != nil {
		return f.SetOwnerErr
	}

	f.Owners[pane] = agentID

	return nil
}

// KillPane records the pane and forgets its owner.
func (f *Fake) KillPane(pane string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KilledPanes = append(f.KilledPanes, pane)
	delete(f.Owners, pane)
}

// IsPaneInMode answers InMode for the pane.
func (f *Fake) IsPaneInMode(pane string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.InMode[pane]
}

// IsPaneRunningCLI is true when the pane runs something other than a shell.
func (f *Fake) IsPaneRunningCLI(pane string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	command := f.Commands[pane]

	return command != "" && !tmux.PaneStatus{Command: command}.IsRunningShell()
}

// IsClientAtPane is false.
func (f *Fake) IsClientAtPane(string, time.Duration) bool {
	return false
}

// PasteAndSubmit records the text as sent.
func (f *Fake) PasteAndSubmit(target, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent[target] = append(f.Sent[target], []string{text})

	return nil
}

// CutLine does nothing.
func (f *Fake) CutLine(string) error {
	return nil
}

// YankLine does nothing.
func (f *Fake) YankLine(string) error {
	return nil
}
