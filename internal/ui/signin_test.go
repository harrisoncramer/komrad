package ui_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

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
