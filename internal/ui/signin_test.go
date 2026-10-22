package ui_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"komrad/internal/engine"
	"komrad/internal/ui/paneltest"

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

func readProse(h *paneltest.Panel) string {
	return paneltest.JoinProse(paneltest.SplitLines(h.Frame()))
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
			harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})

			frame := readProse(harness)
			for _, want := range tt.wantContain {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			assert.False(t, harness.IsWatching(), "the watch waits for access")
			assert.Empty(t, harness.Requests(), "nothing is asked of the engine before access")
			assert.True(t, harness.Quits("ctrl+c"))
		})
	}
}

// TestSignIn walks a panel on an engine at a url through signing in and losing access.
func TestSignIn(t *testing.T) {
	t.Run("logging in checks access again before it shows the engine", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrSignInNeeded}
		harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})

		signIn.answer(nil, nil)
		harness.Press("enter")

		checks, logins := signIn.count()
		assert.Equal(t, 1, logins)
		assert.Equal(t, 2, checks, "the login is checked against the engine before the panel trusts it")
		assert.True(t, harness.IsWatching())

		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend")))
		assert.Contains(t, harness.Frame(), "backend")
	})

	t.Run("a login that fails says why and offers it again", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrSignInNeeded}
		harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})

		signIn.answer(engine.ErrSignInNeeded, errors.New("the browser came back from some other sign-in"))
		harness.Press("enter")

		frame := readProse(harness)
		assert.Contains(t, frame, "the browser came back from some other sign-in")
		assert.Contains(t, frame, "Press enter to sign in")
		assert.False(t, harness.IsWatching())

		signIn.answer(nil, nil)
		harness.Press("enter")

		_, logins := signIn.count()
		assert.Equal(t, 2, logins)
		assert.True(t, harness.IsWatching())
	})

	t.Run("a login the engine does not take leaves the panel at the gate", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrSignInNeeded}
		harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})

		signIn.answer(engine.ErrAccessDenied, nil)
		harness.Press("enter")

		assert.Contains(t, readProse(harness), "You do not have access to this engine")
		assert.False(t, harness.IsWatching())
	})

	t.Run("a failed check is tried again", func(t *testing.T) {
		signIn := &fakeSignIn{access: errors.New("failed to reach the engine")}
		harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})

		signIn.answer(nil, nil)
		harness.Press("enter")

		_, logins := signIn.count()
		assert.Zero(t, logins, "a failure that is not about the sign-in does not ask for one")
		assert.True(t, harness.IsWatching())
	})

	t.Run("a grant the owner gives later is picked up by checking again", func(t *testing.T) {
		signIn := &fakeSignIn{access: engine.ErrAccessDenied}
		harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})

		signIn.answer(nil, nil)
		harness.Press("enter")

		_, logins := signIn.count()
		assert.Zero(t, logins)
		assert.True(t, harness.IsWatching())
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
			harness := paneltest.Start(t, paneltest.Params{SignIn: signIn})
			harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend")))
			require.Contains(t, harness.Frame(), "backend")

			signIn.answer(tt.access, nil)
			harness.LoseWatch(errors.New("the stream ended"))

			frame := readProse(harness)
			assert.Contains(t, frame, tt.wantContain)
			assert.Equal(t, !tt.wantGated, harness.IsWatching())
			if !tt.wantGated {
				return
			}

			harness.Watch(paneltest.Snapshot(newLiveWorkflow("w2", "should-not-render")))
			assert.NotContains(t, readProse(harness), "should-not-render", "a batch already on its way is dropped")

			signIn.answer(nil, nil)
			harness.Press("enter")
			require.True(t, harness.IsWatching())
			assert.NotContains(t, harness.Frame(), "backend", "the panel comes back empty and waits for the engine")
		})
	}
}
