package ui_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"komrad/internal/engine"
	"komrad/internal/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSignIn struct {
	mu     sync.Mutex
	access error
	login  error
	checks int
	logins int
}

func (f *fakeSignIn) CheckAccess(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.checks++

	return f.access
}

func (f *fakeSignIn) Login(_ context.Context, prompt func(authURL string)) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.logins++
	prompt("https://provider.example/auth")

	return f.login
}

func (f *fakeSignIn) answer(access, login error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.access, f.login = access, login
}

func (f *fakeSignIn) count() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.checks, f.logins
}

func readProse(h *panelHarness) string {
	return joinProse(splitLines(h.readFrame()))
}

// TestSignInStartup pins what a panel on an engine at a url shows before the engine says the
// person is signed in with access: one screen, and nothing read from the engine.
func TestSignInStartup(t *testing.T) {
	type signInStartupCase struct {
		name        string
		access      error
		wantContain []string
		wantAbsent  []string
	}

	tests := []signInStartupCase{
		{
			name:        "a machine that never signed in",
			access:      engine.ErrSignInNeeded,
			wantContain: []string{"Sign in to the engine", "Press enter to sign in"},
		},
		{
			name:        "a sign-in the provider would not renew",
			access:      fmt.Errorf("renew: %w", engine.ErrSignInNeeded),
			wantContain: []string{"Sign in to the engine", "Press enter to sign in"},
		},
		{
			name:        "a sign-in the engine did not take",
			access:      engine.ErrSignInNeeded.Saying("the engine did not take this sign-in: the email is not verified"),
			wantContain: []string{"Sign in to the engine", "the email is not verified", "Press enter to sign in"},
		},
		{
			name:        "a person nobody granted access",
			access:      engine.ErrAccessDenied,
			wantContain: []string{"You do not have access to this engine", "Ask the engine's owner"},
			wantAbsent:  []string{"Press enter to sign in"},
		},
		{
			name:        "a provider that cannot be reached",
			access:      errors.New("failed to reach the identity provider at https://provider.example"),
			wantContain: []string{"The panel could not check your sign-in", "failed to reach the identity provider", "Press enter to try again"},
			wantAbsent:  []string{"Press enter to sign in"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signIn := &fakeSignIn{access: tt.access}
			harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})

			harness.watch(buildSnapshot(newLiveWorkflow("w1", "should-not-render")))

			frame := readProse(harness)
			for _, want := range tt.wantContain {
				assert.Contains(t, frame, want)
			}
			for _, absent := range append(tt.wantAbsent, "should-not-render") {
				assert.NotContains(t, frame, absent)
			}
			assert.False(t, harness.panel.IsWatching(), "the watch waits for access")
			assert.Empty(t, harness.readRequests(), "nothing is asked of the engine before access")
			assert.True(t, doesQuit(harness.panel.Update(ui.NewKeyPress("ctrl+c"))))
		})
	}
}

// TestSignIn walks a panel on an engine at a url through signing in and losing access.
func TestSignIn(t *testing.T) {
	t.Run("logging in checks access again before it shows the engine", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrSignInNeeded}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})

		signIn.answer(nil, nil)
		harness.press("enter")

		checks, logins := signIn.count()
		assert.Equal(t, 1, logins)
		assert.Equal(t, 2, checks, "the login is checked against the engine before the panel trusts it")
		assert.True(t, harness.panel.IsWatching())

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
		assert.Contains(t, harness.readFrame(), "backend")
	})

	t.Run("a login that fails says why and offers it again", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrSignInNeeded}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})

		signIn.answer(engine.ErrSignInNeeded, errors.New("the browser came back from some other sign-in"))
		harness.press("enter")

		frame := readProse(harness)
		assert.Contains(t, frame, "the browser came back from some other sign-in")
		assert.Contains(t, frame, "Press enter to sign in")
		assert.False(t, harness.panel.IsWatching())

		signIn.answer(nil, nil)
		harness.press("enter")

		_, logins := signIn.count()
		assert.Equal(t, 2, logins)
		assert.True(t, harness.panel.IsWatching())
	})

	t.Run("a login the engine does not take leaves the panel at the gate", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrSignInNeeded}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})

		signIn.answer(engine.ErrAccessDenied, nil)
		harness.press("enter")

		assert.Contains(t, readProse(harness), "You do not have access to this engine")
		assert.False(t, harness.panel.IsWatching())
	})

	t.Run("a failed check is tried again", func(t *testing.T) {
		signIn := &fakeSignIn{access: errors.New("failed to reach the engine")}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})

		signIn.answer(nil, nil)
		harness.press("enter")

		_, logins := signIn.count()
		assert.Zero(t, logins, "a failure that is not about the sign-in does not ask for one")
		assert.True(t, harness.panel.IsWatching())
	})

	t.Run("a grant the owner gives later is picked up by checking again", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrAccessDenied}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})

		signIn.answer(nil, nil)
		harness.press("enter")

		_, logins := signIn.count()
		assert.Zero(t, logins)
		assert.True(t, harness.panel.IsWatching())
	})
}

// TestSignInLost pins that a panel that loses its sign-in or its grant shows nothing it read
// from the engine while it had them.
func TestSignInLost(t *testing.T) {
	type signInLostCase struct {
		name        string
		access      error
		wantContain string
		wantGated   bool
	}

	tests := []signInLostCase{
		{
			name:        "a grant the owner revoked",
			access:      engine.ErrAccessDenied,
			wantContain: "You do not have access to this engine",
			wantGated:   true,
		},
		{
			name:        "a sign-in the provider would not renew",
			access:      engine.ErrSignInNeeded,
			wantContain: "Sign in to the engine",
			wantGated:   true,
		},
		{
			name:        "a provider that cannot be reached to renew the sign-in",
			access:      fmt.Errorf("renew: %w", engine.ErrProviderUnreachable),
			wantContain: "The panel could not check your sign-in",
			wantGated:   true,
		},
		{
			name:        "an engine that stopped answering",
			access:      errors.New("failed to reach the engine"),
			wantContain: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signIn := &fakeSignIn{}
			harness := newPanelHarness(t, panelHarnessParams{signIn: signIn})
			harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
			require.Contains(t, harness.readFrame(), "backend")

			signIn.answer(tt.access, nil)
			harness.send(ui.NewWatchLost(errors.New("the stream ended")))

			frame := readProse(harness)
			assert.Contains(t, frame, tt.wantContain)
			assert.Equal(t, !tt.wantGated, harness.panel.IsWatching())
			if !tt.wantGated {
				return
			}

			harness.watch(buildSnapshot(newLiveWorkflow("w2", "should-not-render")))
			assert.NotContains(t, readProse(harness), "should-not-render", "a batch already on its way is dropped")

			signIn.answer(nil, nil)
			harness.press("enter")
			require.True(t, harness.panel.IsWatching())
			assert.NotContains(t, harness.readFrame(), "backend", "the panel comes back empty and waits for the engine")
		})
	}
}

// githubPoll is one answer the control plane gives a poll of GitHub's device flow.
type githubPoll struct {
	isAuthorized bool
	err          error
}

type fakeControlPlane struct {
	mu           sync.Mutex
	startErr     error
	isAuthorized bool
	githubErr    error
	codeErr      error
	polls        []githubPoll
	starts       int
	codes        int
}

func (f *fakeControlPlane) StartEngine(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.starts++

	return f.startErr
}

func (f *fakeControlPlane) IsGitHubAuthorized(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.isAuthorized, f.githubErr
}

func (f *fakeControlPlane) StartGitHub(context.Context) (*ui.GitHubCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codes++
	if f.codeErr != nil {
		return nil, f.codeErr
	}

	return &ui.GitHubCode{UserCode: "WDJB-MJHT", VerificationURI: "https://github.com/login/device", Interval: 5 * time.Second}, nil
}

// PollGitHub answers each of polls in turn, holding on the last.
func (f *fakeControlPlane) PollGitHub(context.Context) (*ui.GitHubPoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	next := f.polls[0]
	if len(f.polls) > 1 {
		f.polls = f.polls[1:]
	}
	if next.err != nil {
		return nil, next.err
	}

	return &ui.GitHubPoll{IsAuthorized: next.isAuthorized, Interval: 5 * time.Second}, nil
}

func (f *fakeControlPlane) answerGitHub(codeErr error, polls ...githubPoll) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeErr, f.polls = codeErr, polls
}

func (f *fakeControlPlane) count() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.starts, f.codes
}

// TestStoppedEngine walks a panel on an engine behind a control plane through starting the engine.
func TestStoppedEngine(t *testing.T) {
	t.Run("enter starts a stopped engine, and the panel opens once the engine itself answers", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrEngineStopped}
		plane := &fakeControlPlane{isAuthorized: true}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn, controlPlane: plane})

		frame := readProse(harness)
		assert.Contains(t, frame, "Your engine is stopped")
		assert.Contains(t, frame, "Press enter to start it")

		signIn.answer(engine.ErrEngineStarting, nil)
		harness.press("enter")

		starts, _ := plane.count()
		assert.Equal(t, 1, starts)
		assert.Contains(t, readProse(harness), "Your engine is starting")

		harness.fireTimers()
		assert.Contains(t, readProse(harness), "Your engine is starting")
		assert.False(t, harness.panel.IsWatching())

		signIn.answer(nil, nil)
		harness.fireTimers()
		assert.True(t, harness.panel.IsWatching())
	})

	t.Run("an engine already starting is waited for", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrEngineStarting}
		plane := &fakeControlPlane{isAuthorized: true}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn, controlPlane: plane})

		assert.Contains(t, readProse(harness), "Your engine is starting")

		signIn.answer(nil, nil)
		harness.fireTimers()

		starts, _ := plane.count()
		assert.Zero(t, starts)
		assert.True(t, harness.panel.IsWatching())
	})

	t.Run("a start the control plane refuses says why", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrEngineStopped}
		plane := &fakeControlPlane{startErr: errors.New("the instance is out of capacity")}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn, controlPlane: plane})

		harness.press("enter")

		frame := readProse(harness)
		assert.Contains(t, frame, "Your engine is stopped")
		assert.Contains(t, frame, "the instance is out of capacity")
	})

	t.Run("an engine that stops while it starts says so", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrEngineStopped}
		plane := &fakeControlPlane{}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn, controlPlane: plane})

		harness.press("enter")
		harness.fireTimers()

		frame := readProse(harness)
		assert.Contains(t, frame, "Your engine is stopped")
		assert.Contains(t, frame, "the engine stopped before it answered")
	})

	t.Run("an engine that never answers is given five minutes, then offered again", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrEngineStopped}
		plane := &fakeControlPlane{}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn, controlPlane: plane})

		signIn.answer(engine.ErrEngineStarting, nil)
		harness.press("enter")
		for range int(5 * time.Minute / (2 * time.Second)) {
			require.Contains(t, readProse(harness), "Your engine is starting")
			harness.fireTimers()
		}

		frame := readProse(harness)
		assert.Contains(t, frame, "the engine did not start within five minutes")
		assert.Contains(t, frame, "Press enter to start it")

		harness.press("enter")
		starts, _ := plane.count()
		assert.Equal(t, 2, starts)
	})

	t.Run("a running panel whose engine stops shows the stopped screen", func(t *testing.T) {
		signIn := &fakeSignIn{}
		plane := &fakeControlPlane{isAuthorized: true}
		harness := newPanelHarness(t, panelHarnessParams{signIn: signIn, controlPlane: plane})
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
		require.Contains(t, harness.readFrame(), "backend")

		signIn.answer(engine.ErrEngineStopped, nil)
		harness.send(ui.NewWatchLost(errors.New("the stream ended")))

		frame := readProse(harness)
		assert.Contains(t, frame, "Your engine is stopped")
		assert.NotContains(t, frame, "backend")
		assert.False(t, harness.panel.IsWatching())
	})
}

// TestGitHubAuthorization walks a panel on an engine without a working GitHub login through
// GitHub's device flow.
func TestGitHubAuthorization(t *testing.T) {
	t.Run("the panel shows GitHub's code until the person enters it", func(t *testing.T) {
		plane := &fakeControlPlane{polls: []githubPoll{{}, {isAuthorized: true}}}
		harness := newPanelHarness(t, panelHarnessParams{signIn: &fakeSignIn{}, controlPlane: plane})

		frame := readProse(harness)
		assert.Contains(t, frame, "Authorize GitHub for your engine")
		assert.Contains(t, frame, "Open https://github.com/login/device and enter WDJB-MJHT.")
		assert.Contains(t, frame, "Waiting for GitHub...")
		assert.False(t, harness.panel.IsWatching())

		harness.fireTimers()
		assert.Contains(t, readProse(harness), "Waiting for GitHub...")

		harness.fireTimers()
		assert.True(t, harness.panel.IsWatching())
	})

	t.Run("an engine whose GitHub login works opens at once", func(t *testing.T) {
		plane := &fakeControlPlane{isAuthorized: true}
		harness := newPanelHarness(t, panelHarnessParams{signIn: &fakeSignIn{}, controlPlane: plane})

		_, codes := plane.count()
		assert.Zero(t, codes)
		assert.True(t, harness.panel.IsWatching())
	})

	t.Run("a control plane that cannot ask GitHub lets the person in", func(t *testing.T) {
		plane := &fakeControlPlane{githubErr: errors.New("the control plane answered 502 Bad Gateway")}
		harness := newPanelHarness(t, panelHarnessParams{signIn: &fakeSignIn{}, controlPlane: plane})

		assert.True(t, harness.panel.IsWatching())
	})

	tests := []struct {
		name    string
		codeErr error
		polls   []githubPoll
		wantErr string
	}{
		{
			name:    "a code that runs out offers a new code",
			polls:   []githubPoll{{err: errors.New("the code ran out before it was entered")}},
			wantErr: "the code ran out before it was entered",
		},
		{
			name:    "an authorization turned down at GitHub offers a new code",
			polls:   []githubPoll{{err: errors.New("the authorization was turned down at GitHub")}},
			wantErr: "the authorization was turned down at GitHub",
		},
		{
			name:    "a code GitHub would not give offers a new code",
			codeErr: errors.New("the control plane answered 502 Bad Gateway"),
			wantErr: "the control plane answered 502 Bad Gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plane := &fakeControlPlane{codeErr: tt.codeErr, polls: tt.polls}
			harness := newPanelHarness(t, panelHarnessParams{signIn: &fakeSignIn{}, controlPlane: plane})
			harness.fireTimers()

			frame := readProse(harness)
			assert.Contains(t, frame, tt.wantErr)
			assert.Contains(t, frame, "Press enter for a new code")
			assert.False(t, harness.panel.IsWatching())

			plane.answerGitHub(nil, githubPoll{isAuthorized: true})
			harness.press("enter")
			harness.fireTimers()

			_, codes := plane.count()
			assert.Equal(t, 2, codes)
			assert.True(t, harness.panel.IsWatching())
		})
	}
}
