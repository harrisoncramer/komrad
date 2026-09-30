package ui

import (
	"komrad/lib/xptr"
)

type sessionTmux interface {
	GetSessionClient(session string) string
	SetSessionOption(target, name, value string)
	BindRootKey(key string, command ...string)
	UnbindRootKey(key string)
	GetWindowID(pane string) string
	DetachSession(name string)
}

// Session is the tmux session the panel draws in.
type Session struct {
	tmux       sessionTmux
	session    string
	cycleKeys  []string
	visualKeys []string
	// visualHeld is whether the visual keys are bound.
	visualHeld bool
	// cycling is whether the cycle keys are bound, nil until the panel has settled it once.
	cycling *bool
}

// NewSession builds the named tmux session the panel runs in over a tmux client, binding the
// cycle keys, spelled as tmux spells them, while a pane beside the sidebar is a stop.
func NewSession(tmux sessionTmux, session string, cycleKeys, visualKeys []string) *Session {
	return &Session{
		tmux:       tmux,
		session:    session,
		cycleKeys:  cycleKeys,
		visualKeys: visualKeys,
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

// HoldVisualKeys takes the visual keys for sidebar from every pane in its window, leaving them with
// the panes of every other window.
func (s *Session) HoldVisualKeys(sidebar string) {
	if s.visualHeld || len(s.visualKeys) == 0 || sidebar == "" {
		return
	}

	window := s.tmux.GetWindowID(sidebar)
	if window == "" {
		return
	}

	for _, key := range s.visualKeys {
		quoted := "'" + key + "'"
		s.tmux.BindRootKey(key, "if-shell", "-F", "#{==:#{window_id},"+window+"}",
			"send-keys -t "+sidebar+" "+quoted, "send-keys "+quoted)
	}

	s.visualHeld = true
}

// ReleaseVisualKeys gives the visual keys back to the panes on the panel's way out.
func (s *Session) ReleaseVisualKeys() {
	if !s.visualHeld {
		return
	}

	for _, key := range s.visualKeys {
		s.tmux.UnbindRootKey(key)
	}

	s.visualHeld = false
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
