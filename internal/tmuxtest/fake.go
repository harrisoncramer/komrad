package tmuxtest

import (
	"errors"
	"sync"
	"time"

	"weave/internal/tmux"
)

type Fake struct {
	mu              sync.Mutex
	Owners          map[string]string
	Captures        map[string]string
	Commands        map[string]string
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

func NewFake() *Fake {
	return &Fake{
		Owners:         map[string]string{},
		Captures:       map[string]string{},
		Commands:       map[string]string{},
		Sent:           map[string][][]string{},
		Globals:        map[string]string{},
		NewWindows:     map[string]string{},
		SessionOptions: map[string]string{},
		Respawned:      map[string][]Respawn{},
		Shell:          "/bin/zsh",
	}
}

type Respawn struct {
	Dir  string
	Argv []string
}

func (f *Fake) SetGlobal(name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Globals[name] = value
}

func (f *Fake) ShowGlobal(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Globals[name]
}

func (f *Fake) GetPaneOwner(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Owners[pane]
}

func (f *Fake) CapturePane(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Captures[pane]
}

func (f *Fake) GetHostPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ClientDir
}

func (f *Fake) DetachSession(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Detached = append(f.Detached, name)
}

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

func (f *Fake) SetSessionOption(target, name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SessionOptions[target+"/"+name] = value
}

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

func (f *Fake) GetDefaultShell() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.Shell
}

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

func (f *Fake) LastRespawn(pane string) Respawn {
	f.mu.Lock()
	defer f.mu.Unlock()

	runs := f.Respawned[pane]
	if len(runs) == 0 {
		return Respawn{}
	}

	return runs[len(runs)-1]
}

func (f *Fake) SplitRight(anchorPane, dir string) string { return "%stage" }

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

func (f *Fake) WindowsOpened() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.NewWindowCalls
}

func (f *Fake) SetPaneOwner(pane, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.SetOwnerErr != nil {
		return f.SetOwnerErr
	}

	f.Owners[pane] = agentID

	return nil
}

func (f *Fake) KillPane(pane string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KilledPanes = append(f.KilledPanes, pane)
	delete(f.Owners, pane)
}

func (f *Fake) IsPaneInMode(string) bool { return false }

func (f *Fake) IsPaneRunningCLI(pane string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	command := f.Commands[pane]

	return command != "" && !tmux.PaneStatus{Command: command}.IsRunningShell()
}

func (f *Fake) IsClientAtPane(string, time.Duration) bool { return false }

func (f *Fake) PasteAndSubmit(target, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent[target] = append(f.Sent[target], []string{text})

	return nil
}

func (f *Fake) CutLine(string) error { return nil }

func (f *Fake) YankLine(string) error { return nil }
