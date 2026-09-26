package ui

import (
	"komrad/lib/xptr"
)

type sessionTmux interface {
	GetSessionClient(session string) string
	SetSessionOption(target, name, value string)
	BindRootKey(key string, command ...string)
	UnbindRootKey(key string)
	DetachSession(name string)
}

// Session is the tmux session the panel draws in.
type Session struct {
	tmux      sessionTmux
	session   string
	cycleKeys []string
	// cycling is whether the cycle keys are bound, nil until the panel has settled it once.
	cycling *bool
}

// NewSession builds the named tmux session the panel runs in over a tmux client, binding the
// cycle keys, spelled as tmux spells them, while an agent is staged.
func NewSession(tmux sessionTmux, session string, cycleKeys []string) *Session {
	return &Session{tmux: tmux, session: session, cycleKeys: cycleKeys}
}

// HoldCycleKeys takes the cycle keys for sidebar while an agent's pane is on the stage beside it,
// or gives them back to the panes when there is none.
func (s *Session) HoldCycleKeys(sidebar string, staged bool) {
	switch {
	case sidebar == "":
	case s.cycling != nil && *s.cycling == staged:
	case staged:
		for _, key := range s.cycleKeys {
			s.tmux.BindRootKey(key, "send-keys", "-t", sidebar, key)
		}
		s.cycling = xptr.Ptr(staged)
	default:
		s.unbindCycleKeys()
		s.cycling = xptr.Ptr(staged)
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

func (s *Session) unbindCycleKeys() {
	for _, key := range s.cycleKeys {
		s.tmux.UnbindRootKey(key)
	}
}

// IsShown reports whether anything is attached to the panel's session, which the panel cannot
// see for itself since hiding only detaches the client and leaves this program running.
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
