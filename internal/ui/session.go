package ui

import (
	"komrad/lib/xptr"
)

type sessionTmux interface {
	GetSessionClient(session string) string
	SetSessionOption(target, name, value string)
	BindRootKey(key string, command ...string)
	UnbindRootKey(key string)
	BindTableKey(table, key string, command ...string)
	UnbindTable(table string)
	GetWindowID(pane string) string
	DetachSession(name string)
}

// LeaderKey is the key, spelled as tmux spells it, that opens the panel's leader table.
const LeaderKey = "C-Space"

// leaderTable is the tmux key table the leader opens.
const leaderTable = "komrad-leader"

// leaderPanelKeys are the leader's keys the panel answers itself.
var leaderPanelKeys = []string{"v", "j"}

// leaderLastKeys are the leader's keys that send the keyboard back to the pane it last left.
var leaderLastKeys = []string{LeaderKey, "Space"}

// Session is the tmux session the panel draws in.
type Session struct {
	tmux      sessionTmux
	session   string
	cycleKeys []string
	// leaderHeld is whether the leader is bound.
	leaderHeld bool
	// cycling is whether the cycle keys are bound, nil until the panel has settled it once.
	cycling *bool
}

// NewSession builds the named tmux session the panel runs in over a tmux client, binding the
// cycle keys, spelled as tmux spells them, while a pane beside the sidebar is a stop.
func NewSession(tmux sessionTmux, session string, cycleKeys []string) *Session {
	return &Session{
		tmux:      tmux,
		session:   session,
		cycleKeys: cycleKeys,
	}
}

// HoldCycleKeys takes the cycle keys for sidebar while a pane beside it is a stop, or gives them
// back to the panes when there is none.
func (s *Session) HoldCycleKeys(sidebar string, held bool) {
	switch {
	case sidebar == "":
	case s.cycling != nil && *s.cycling == held:
	case held:
		for _, key := range s.cycleKeys {
			s.tmux.BindRootKey(key, "send-keys", "-t", sidebar, key)
		}
		s.cycling = xptr.Ptr(held)
	default:
		s.unbindCycleKeys()
		s.cycling = xptr.Ptr(held)
	}
}

// ReleaseCycleKeys gives the cycle keys back to the panes on the panel's way out.
func (s *Session) ReleaseCycleKeys() {
	if s.cycling == nil || !*s.cycling {
		return
	}

	s.unbindCycleKeys()
	s.cycling = nil
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

	s.tmux.BindRootKey(LeaderKey, "if-shell", "-F", "#{==:#{window_id},"+window+"}",
		"switch-client -T "+leaderTable, "send-keys "+LeaderKey)

	for _, key := range leaderPanelKeys {
		s.tmux.BindTableKey(leaderTable, key, "send-keys", "-t", sidebar, LeaderKey, key)
	}

	for _, key := range leaderLastKeys {
		s.tmux.BindTableKey(leaderTable, key, "if-shell", "-F", "#{window_zoomed_flag}", "", "last-pane")
	}

	s.leaderHeld = true
}

// ReleaseLeader gives the leader back to the panes on the panel's way out.
func (s *Session) ReleaseLeader() {
	if !s.leaderHeld {
		return
	}

	s.tmux.UnbindRootKey(LeaderKey)
	s.tmux.UnbindTable(leaderTable)
	s.leaderHeld = false
}

func (s *Session) unbindCycleKeys() {
	for _, key := range s.cycleKeys {
		s.tmux.UnbindRootKey(key)
	}
}

// IsShown reports whether anything is attached to the panel's session.
func (s *Session) IsShown() bool {
	return s.tmux.GetSessionClient(s.session) != ""
}

// Hide detaches every client from the panel's session, leaving the session alive for the next attach.
func (s *Session) Hide() {
	s.tmux.DetachSession(s.session)
}

// SilenceStatus turns off a session's tmux status bar.
func (s *Session) SilenceStatus(session string) {
	s.tmux.SetSessionOption(session, "status", "off")
}
