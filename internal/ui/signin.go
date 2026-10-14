package ui

import (
	"context"
	"errors"
	"time"

	"komrad/internal/engine"

	tea "charm.land/bubbletea/v2"
)

const loginWait = 5 * time.Minute

// SignIn checks that the person is signed in to an engine at a url with access, and signs them in.
type SignIn interface {
	CheckAccess(ctx context.Context) error
	Login(ctx context.Context, prompt func(authURL string)) error
}

type accessStage int

const (
	accessGranted accessStage = iota
	accessChecking
	accessSignInNeeded
	accessLoggingIn
	accessDenied
	accessFailed
)

type accessState struct {
	signIn    SignIn
	deps      Deps
	stage     accessStage
	err       error
	authURL   string
	watch     func() context.CancelFunc
	stopWatch context.CancelFunc
}

type accessCheckedMsg struct {
	err error
}

type loginPromptMsg struct {
	authURL string
}

type loggedInMsg struct {
	err error
}

func (m *model) isGated() bool {
	return m.access.stage != accessGranted
}

func (m *model) checkAccessCmd() tea.Cmd {
	signIn := m.access.signIn

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		return accessCheckedMsg{err: signIn.CheckAccess(ctx)}
	}
}

func (m *model) loginCmd() tea.Cmd {
	signIn, post := m.access.signIn, m.post

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loginWait)
		defer cancel()

		err := signIn.Login(ctx, func(authURL string) {
			post.send(loginPromptMsg{authURL: authURL})
		})

		return loggedInMsg{err: err}
	}
}

func (m *model) routeSignIn(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.updateSignIn(msg)
	case accessCheckedMsg:
		if m.access.stage == accessChecking {
			return m.applyAccessChecked(msg.err)
		}
	case loginPromptMsg:
		if m.access.stage == accessLoggingIn {
			m.access.authURL = msg.authURL
		}
	case loggedInMsg:
		if m.access.stage == accessLoggingIn {
			return m.applyLoggedIn(msg.err)
		}
	}

	return nil
}

func (m *model) updateSignIn(key tea.KeyPressMsg) tea.Cmd {
	pressed := key.String()

	if isBound(pressed, fixedKeys().Quit) {
		return tea.Quit
	}

	if !isBound(pressed, fixedKeys().Submit) {
		return nil
	}

	switch m.access.stage {
	case accessSignInNeeded:
		m.access.stage, m.access.err, m.access.authURL = accessLoggingIn, nil, ""

		return m.loginCmd()
	case accessDenied, accessFailed:
		m.access.stage, m.access.err = accessChecking, nil

		return m.checkAccessCmd()
	case accessGranted, accessChecking, accessLoggingIn:
	}

	return nil
}

func (m *model) applyAccessChecked(err error) tea.Cmd {
	switch {
	case err == nil:
		return m.grantAccess()
	case errors.Is(err, engine.ErrSignInNeeded):
		m.access.stage = accessSignInNeeded
	case errors.Is(err, engine.ErrAccessDenied):
		m.access.stage, m.access.err = accessDenied, err
	default:
		m.access.stage, m.access.err = accessFailed, err
	}

	return nil
}

func (m *model) applyLoggedIn(err error) tea.Cmd {
	m.access.authURL = ""

	if err != nil {
		m.access.stage, m.access.err = accessSignInNeeded, err

		return nil
	}

	m.access.stage = accessChecking

	return m.checkAccessCmd()
}

func (m *model) grantAccess() tea.Cmd {
	m.access.stage, m.access.err = accessGranted, nil

	m.attachFrame()
	m.startWatch()
	m.holdLeader()

	return tea.Batch(tick(), tea.RequestBackgroundColor)
}

func (m *model) startWatch() {
	if m.access.watch == nil {
		return
	}

	m.access.stopWatch = m.access.watch()
}

func (m *model) applyAccessRechecked(err error) {
	if !errors.Is(err, engine.ErrSignInNeeded) && !errors.Is(err, engine.ErrAccessDenied) && !errors.Is(err, engine.ErrProviderUnreachable) {
		return
	}

	m.loseAccess(err)
}

func (m *model) loseAccess(err error) {
	if m.access.stopWatch != nil {
		m.access.stopWatch()
	}

	m.frame.Vacate()

	gated := newModel(m.access.deps, m.term.isDark, m.post)
	gated.access.watch = m.access.watch
	gated.term.width, gated.term.height, gated.term.paneCols = m.term.width, m.term.height, m.term.paneCols
	gated.applyAccessChecked(err)

	*m = gated
}
