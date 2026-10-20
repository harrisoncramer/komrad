package ui

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/engine"
	"komrad/internal/logging"

	tea "charm.land/bubbletea/v2"
)

const loginWait = 5 * time.Minute

// startPoll is how often the panel checks access again while the engine starts.
const startPoll = 2 * time.Second

// startChecks is how many checks the panel waits through, five minutes, before it says the engine
// did not start.
const startChecks = int(5 * time.Minute / startPoll)

var (
	errStoppedWhileStarting = errors.New("the engine stopped before it answered")
	errStartTimedOut        = errors.New("the engine did not start within five minutes")
)

// SignIn checks that the person is signed in to an engine at a url with access, and signs them in.
type SignIn interface {
	CheckAccess(ctx context.Context) error
	Login(ctx context.Context, prompt func(authURL string)) error
}

// ControlPlane starts an engine at a url and authorizes GitHub for it, through the control plane
// in front of the engine.
type ControlPlane interface {
	StartEngine(ctx context.Context) error
	IsGitHubAuthorized(ctx context.Context) (bool, error)
	StartGitHub(ctx context.Context) (*GitHubCode, error)
	PollGitHub(ctx context.Context) (*GitHubPoll, error)
}

// GitHubCode is the code GitHub's device flow gives the person to enter at its address, and how
// long to wait before the first poll.
type GitHubCode struct {
	UserCode        string
	VerificationURI string
	Interval        time.Duration
}

// GitHubPoll is whether the person entered the code, and how long to wait before the next poll
// when they have not.
type GitHubPoll struct {
	IsAuthorized bool
	Interval     time.Duration
}

type accessStage int

const (
	accessGranted accessStage = iota
	accessChecking
	accessSignInNeeded
	accessLoggingIn
	accessDenied
	accessFailed
	accessStopped
	accessStarting
	accessGitHubNeeded
	accessAuthorizingGitHub
)

type accessState struct {
	signIn       SignIn
	controlPlane ControlPlane
	deps         Deps
	stage        accessStage
	err          error
	authURL      string
	githubCode   *GitHubCode
	// checks counts the checks made while the engine starts.
	checks    int
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

type engineStartedMsg struct {
	err error
}

type startPollMsg struct{}

type githubCheckedMsg struct {
	isAuthorized bool
}

type githubCodeMsg struct {
	code *GitHubCode
	err  error
}

type githubPollMsg struct{}

type githubPolledMsg struct {
	poll *GitHubPoll
	err  error
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

func (m *model) startEngineCmd() tea.Cmd {
	controlPlane := m.access.controlPlane

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		return engineStartedMsg{err: controlPlane.StartEngine(ctx)}
	}
}

func startPollCmd() tea.Cmd {
	return startTimer(startPoll, func(time.Time) tea.Msg { return startPollMsg{} })
}

// checkGitHubCmd asks whether the engine's GitHub login works, and counts a control plane that
// cannot say as a yes, so a GitHub outage never keeps the person from their engine.
func (m *model) checkGitHubCmd() tea.Cmd {
	controlPlane := m.access.controlPlane

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		isAuthorized, err := controlPlane.IsGitHubAuthorized(ctx)
		if err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to ask whether the engine's GitHub login works", logging.Err(err)...)

			return githubCheckedMsg{isAuthorized: true}
		}

		return githubCheckedMsg{isAuthorized: isAuthorized}
	}
}

func (m *model) startGitHubCmd() tea.Cmd {
	controlPlane := m.access.controlPlane

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		code, err := controlPlane.StartGitHub(ctx)

		return githubCodeMsg{code: code, err: err}
	}
}

func githubPollCmd(interval time.Duration) tea.Cmd {
	return startTimer(interval, func(time.Time) tea.Msg { return githubPollMsg{} })
}

func (m *model) pollGitHubCmd() tea.Cmd {
	controlPlane := m.access.controlPlane

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		poll, err := controlPlane.PollGitHub(ctx)

		return githubPolledMsg{poll: poll, err: err}
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
		if m.access.stage == accessStarting {
			return m.applyStartChecked(msg.err)
		}
	case loginPromptMsg:
		if m.access.stage == accessLoggingIn {
			m.access.authURL = msg.authURL
		}
	case loggedInMsg:
		if m.access.stage == accessLoggingIn {
			return m.applyLoggedIn(msg.err)
		}
	case engineStartedMsg:
		if m.access.stage == accessStarting {
			return m.applyEngineStarted(msg.err)
		}
	case startPollMsg:
		if m.access.stage == accessStarting {
			return m.checkAccessCmd()
		}
	case githubCheckedMsg:
		if m.access.stage == accessChecking {
			return m.applyGitHubChecked(msg.isAuthorized)
		}
	case githubCodeMsg:
		if m.access.stage == accessAuthorizingGitHub {
			return m.applyGitHubCode(msg.code, msg.err)
		}
	case githubPollMsg:
		if m.access.stage == accessAuthorizingGitHub {
			return m.pollGitHubCmd()
		}
	case githubPolledMsg:
		if m.access.stage == accessAuthorizingGitHub {
			return m.applyGitHubPolled(msg.poll, msg.err)
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
	case accessStopped:
		m.access.stage, m.access.err, m.access.checks = accessStarting, nil, 0

		return m.startEngineCmd()
	case accessGitHubNeeded:
		return m.authorizeGitHub()
	case accessGranted, accessChecking, accessLoggingIn, accessStarting, accessAuthorizingGitHub:
	}

	return nil
}

func (m *model) applyAccessChecked(err error) tea.Cmd {
	switch {
	case err == nil:
		if m.access.controlPlane == nil {
			return m.grantAccess()
		}
		m.access.stage, m.access.err = accessChecking, nil

		return m.checkGitHubCmd()
	case errors.Is(err, engine.ErrSignInNeeded):
		m.access.stage, m.access.err = accessSignInNeeded, err
	case errors.Is(err, engine.ErrAccessDenied):
		m.access.stage, m.access.err = accessDenied, err
	case errors.Is(err, engine.ErrEngineStopped):
		m.access.stage, m.access.err = accessStopped, nil
	case errors.Is(err, engine.ErrEngineStarting):
		m.access.stage, m.access.err, m.access.checks = accessStarting, nil, 0

		return startPollCmd()
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

func (m *model) applyEngineStarted(err error) tea.Cmd {
	if err != nil {
		m.access.stage, m.access.err = accessStopped, err

		return nil
	}

	return startPollCmd()
}

// applyStartChecked waits on anything but the engine itself answering, a stopped engine or a
// sign-in that no longer holds.
func (m *model) applyStartChecked(err error) tea.Cmd {
	switch {
	case err == nil, errors.Is(err, engine.ErrSignInNeeded), errors.Is(err, engine.ErrAccessDenied):
		return m.applyAccessChecked(err)
	case errors.Is(err, engine.ErrEngineStopped):
		m.access.stage, m.access.err = accessStopped, errStoppedWhileStarting

		return nil
	}

	m.access.checks++
	if m.access.checks >= startChecks {
		m.access.stage, m.access.err = accessStopped, errStartTimedOut

		return nil
	}

	return startPollCmd()
}

func (m *model) applyGitHubChecked(isAuthorized bool) tea.Cmd {
	if isAuthorized {
		return m.grantAccess()
	}

	return m.authorizeGitHub()
}

func (m *model) authorizeGitHub() tea.Cmd {
	m.access.stage, m.access.err, m.access.githubCode = accessAuthorizingGitHub, nil, nil

	return m.startGitHubCmd()
}

func (m *model) applyGitHubCode(code *GitHubCode, err error) tea.Cmd {
	if err != nil {
		m.access.stage, m.access.err = accessGitHubNeeded, err

		return nil
	}

	m.access.githubCode = code

	return githubPollCmd(code.Interval)
}

func (m *model) applyGitHubPolled(poll *GitHubPoll, err error) tea.Cmd {
	switch {
	case err != nil:
		m.access.stage, m.access.err, m.access.githubCode = accessGitHubNeeded, err, nil

		return nil
	case poll.IsAuthorized:
		return m.grantAccess()
	}

	return githubPollCmd(poll.Interval)
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

func (m *model) applyAccessRechecked(err error) tea.Cmd {
	for _, lost := range []error{engine.ErrSignInNeeded, engine.ErrAccessDenied, engine.ErrProviderUnreachable, engine.ErrEngineStopped, engine.ErrEngineStarting} {
		if errors.Is(err, lost) {
			return m.loseAccess(err)
		}
	}

	return nil
}

func (m *model) loseAccess(err error) tea.Cmd {
	if m.access.stopWatch != nil {
		m.access.stopWatch()
	}

	m.frame.Vacate()

	gated := newModel(m.access.deps, m.term.isDark, m.post)
	gated.access.watch = m.access.watch
	gated.term.width, gated.term.height, gated.term.paneCols = m.term.width, m.term.height, m.term.paneCols
	cmd := gated.applyAccessChecked(err)

	*m = gated

	return cmd
}
