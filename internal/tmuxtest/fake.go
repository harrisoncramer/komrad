package tmuxtest

import (
	"errors"
	"slices"
	"sync"
	"time"

	"komrad/internal/tmux"
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
	AfterGetPanes   func()
	BeforeGetOwner  func()
	AfterSetOwner   func()
	NewWindowCalls  int
	SetOwnerErr     error
	SendKeysErr     error
	PaneOwnersErr   error
	RespawnErr      error
	Shell           string
	Respawned       map[string][]Respawn
	ClientAt        map[string]bool
	PasteErr        error
	YankErr         error
	// Edits is every cut, paste and yank made on a pane, in the order they were made.
	Edits map[string][]string
	// CutFrames is what a pane shows once its line is cut, one frame per capture, holding the last.
	CutFrames map[string][]string
	cutReads  map[string]int
	// Views is the pane each view was last opened on.
	Views map[string]string
	// SessionClients is how many clients each session answers it has attached.
	SessionClients map[string]int
	killedSessions []string
	// AttachCommand is what an attach runs in place of tmux.
	AttachCommand []string
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
		ClientAt:       map[string]bool{},
		Edits:          map[string][]string{},
		CutFrames:      map[string][]string{},
		Views:          map[string]string{},
		SessionClients: map[string]int{},
		Shell:          "/bin/zsh",
		AttachCommand:  []string{"true"},
	}
}

// OpenView records the pane a view is opened on.
func (f *Fake) OpenView(view, _, pane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Views[view] = pane

	return nil
}

// CountSessionClients answers SessionClients for the session.
func (f *Fake) CountSessionClients(session string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.SessionClients[session], nil
}

// KillSession records the session.
func (f *Fake) KillSession(session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.killedSessions = append(f.killedSessions, session)

	return nil
}

// BuildAttachArgv answers AttachCommand.
func (f *Fake) BuildAttachArgv(string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.AttachCommand
}

// ReadKilledSessions is every session killed so far.
func (f *Fake) ReadKilledSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.killedSessions)
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
	if f.BeforeGetOwner != nil {
		f.BeforeGetOwner()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Owners[pane]
}

// CapturePane answers the frame Captures holds for the pane, or the next of CutFrames once its line is cut.
func (f *Fake) CapturePane(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	frames := f.CutFrames[pane]
	if read, cut := f.cutReads[pane]; cut && len(frames) > 0 {
		f.cutReads[pane]++

		return frames[min(read, len(frames)-1)]
	}

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
	panes, err := f.listPanes()
	if err == nil && f.AfterGetPanes != nil {
		f.AfterGetPanes()
	}

	return panes, err
}

func (f *Fake) listPanes() (map[string]tmux.PaneStatus, error) {
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
	if err := f.recordOwner(pane, agentID); err != nil {
		return err
	}

	if f.AfterSetOwner != nil {
		f.AfterSetOwner()
	}

	return nil
}

func (f *Fake) recordOwner(pane, agentID string) error {
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

// IsClientAtPane answers ClientAt for the pane.
func (f *Fake) IsClientAtPane(pane string, _ time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ClientAt[pane]
}

// PasteAndSubmit records the paste, and the text as sent unless PasteErr refuses it.
func (f *Fake) PasteAndSubmit(target, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.recordEdit(target, "paste")
	if f.PasteErr != nil {
		return f.PasteErr
	}
	f.Sent[target] = append(f.Sent[target], []string{text})

	return nil
}

// CutLine records the cut, after which the pane shows CutFrames.
func (f *Fake) CutLine(target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.recordEdit(target, "cut")
	if f.cutReads == nil {
		f.cutReads = map[string]int{}
	}
	f.cutReads[target] = 0

	return nil
}

// YankLine records the yank, answering YankErr.
func (f *Fake) YankLine(target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.recordEdit(target, "yank")

	return f.YankErr
}

func (f *Fake) recordEdit(target, edit string) {
	if f.Edits == nil {
		f.Edits = map[string][]string{}
	}
	f.Edits[target] = append(f.Edits[target], edit)
}
