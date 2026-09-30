package ui

import (
	"komrad/internal/panes"
	"komrad/internal/ui/paint"
)

// modeHooks are the tmux hooks that tell the panel whether the active pane in its window is in
// copy-mode.
var modeHooks = []string{"pane-mode-changed", "window-pane-changed"}

type sessionTmux interface {
	SetSessionOption(target, name, value string)
	BindRootKey(key string, command ...string)
	UnbindRootKey(key string)
	BindTableKey(table, key string, command ...string)
	UnbindTable(table string)
	GetWindowID(pane string) string
	DetachSession(name string)
	TuneCopyMode(selection string)
	SetWindowHook(window, hook, command string)
	UnsetWindowHook(window, hook string)
	OpenCopyStandIn(params panes.CopyStandIn) error
}

// NewSessionParams is what NewSession builds a Session from.
type NewSessionParams struct {
	Tmux        sessionTmux
	Session     string
	LeaderKey   string
	LeaderTable string
}

// Session is the tmux session the panel draws in.
type Session struct {
	tmux    sessionTmux
	session string
	// leaderKey is the key, spelled as tmux spells it, that opens the leader table.
	leaderKey   string
	leaderTable string
	// leaderHeld is whether the leader is bound.
	leaderHeld bool
	// window is the window the leader is held in.
	window string
	// binary is the komrad binary a stand-in runs visual mode with.
	binary string
}

// NewSession builds the named tmux session the panel runs in over a tmux client, with the leader
// key opening the leader table.
func NewSession(params NewSessionParams) *Session {
	return &Session{
		tmux:        params.Tmux,
		session:     params.Session,
		leaderKey:   params.LeaderKey,
		leaderTable: params.LeaderTable,
	}
}

// HoldLeader binds the leader in every pane of sidebar's window, leaving it with the panes of every
// other window.
func (s *Session) HoldLeader(sidebar string) {
	if s.leaderHeld || sidebar == "" {
		return
	}

	window := s.tmux.GetWindowID(sidebar)
	if window == "" {
		return
	}

	s.tmux.BindRootKey(s.leaderKey, "if-shell", "-F", "#{==:#{window_id},"+window+"}",
		"switch-client -T "+s.leaderTable, "send-keys "+s.leaderKey)

	for _, key := range leaderPanelKeys {
		s.tmux.BindTableKey(s.leaderTable, key, "send-keys", "-t", sidebar, s.leaderKey, key)
	}

	s.tmux.BindTableKey(s.leaderTable, copyKey, "if-shell", "-F", "#{==:#{pane_id},"+sidebar+"}",
		"send-keys -t "+sidebar+" "+s.leaderKey+" "+copyKey, "copy-mode")

	for _, key := range []string{s.leaderKey, "Space"} {
		s.tmux.BindTableKey(s.leaderTable, key, "if-shell", "-F", "#{window_zoomed_flag}",
			"send-keys -t "+sidebar+" "+s.leaderKey+" "+s.leaderKey, "last-pane")
	}

	tell := func(key string) string { return "send-keys -t " + sidebar + " " + s.leaderKey + " " + key }
	for _, hook := range modeHooks {
		s.tmux.SetWindowHook(window, hook, "if-shell -F -t "+window+" \"#{==:#{pane_mode},copy-mode}\" \""+
			tell(paneCopyKey)+"\" \""+tell(paneNormalKey)+"\"")
	}

	s.window = window
	s.leaderHeld = true
}

// ReleaseLeader gives the leader back to the panes on the panel's way out.
func (s *Session) ReleaseLeader() {
	if !s.leaderHeld {
		return
	}

	s.tmux.UnbindRootKey(s.leaderKey)
	s.tmux.UnbindTable(s.leaderTable)
	for _, hook := range modeHooks {
		s.tmux.UnsetWindowHook(s.window, hook)
	}
	s.leaderHeld = false
}

// Hide detaches every client from the panel's session, leaving the session alive for the next attach.
func (s *Session) Hide() {
	s.tmux.DetachSession(s.session)
}

// SilenceStatus turns off the panel session's tmux status bar.
func (s *Session) SilenceStatus() {
	s.tmux.SetSessionOption(s.session, "status", "off")
}

// TuneCopyMode makes tmux's copy-mode the panel's visual mode, run in a stand-in by binary.
func (s *Session) TuneCopyMode(binary string) {
	s.binary = binary
	s.tmux.TuneCopyMode(paint.Panel.TmuxSelection())
}

// OpenCopyStandIn puts standIn in copy-mode where sidebar sits, and has it send the sidebar the
// leader's copy key once copy-mode ends.
func (s *Session) OpenCopyStandIn(sidebar string, standIn panes.CopyStandIn) error {
	standIn.Pane = sidebar
	standIn.Binary = s.binary
	standIn.BorderStyle = paint.Panel.TmuxBorder()
	standIn.Keys = []string{s.leaderKey, copyKey}

	return s.tmux.OpenCopyStandIn(standIn)
}
