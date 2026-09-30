package doctor

import (
	"context"
	"slices"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/ui"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rootKeysEnv answers tmux with a root table holding the given bindings and a prefix of C-b.
func rootKeysEnv(cfg *config.Config, rootKeys ...string) Env {
	return rootKeysEnvWithPrefix(cfg, "C-b", rootKeys...)
}

func rootKeysEnvWithPrefix(cfg *config.Config, prefix string, rootKeys ...string) Env {
	env := configEnv(cfg, nil, true)
	env.TmuxSocket = "komrad"
	env.ValidateKeybindings = ui.ValidateKeybindings
	env.IsPanelRunning = func(context.Context) bool { return true }
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		switch {
		case slices.Contains(args, "list-keys"):
			lines := xslice.Map(rootKeys, func(key string) string {
				return "bind-key  -T root " + key + "    copy-mode -u"
			})

			return strings.Join(lines, "\n"), nil
		case slices.Contains(args, "prefix"):
			return prefix + "\n", nil
		}

		return "", nil
	}

	return env
}

// TestKeybindingsCheckFindsAKeyTmuxHolds covers a panel binding on a key the user's own tmux takes
// in its root table, where the press never reaches the panel at all.
func TestKeybindingsCheckFindsAKeyTmuxHolds(t *testing.T) {
	cfg := config.Defaults()
	cfg.Keybindings.Views.Next = []string{"ctrl+y"}

	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(cfg, "C-y"))

	assert.Equal(t, StatusFail, result.Status,
		"tmux answers C-y before the panel does, so the view switcher is unreachable")
	assert.Equal(t, "ctrl+y", result.Detail)
	assert.Contains(t, result.Fix, "unbind -n C-y")
}

// TestKeybindingsCheckPassesWhenTmuxHoldsNothing covers the ordinary machine, where the root table
// holds only mouse bindings.
func TestKeybindingsCheckPassesWhenTmuxHoldsNothing(t *testing.T) {
	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(config.Defaults(), "MouseDown1Pane"))

	assert.Equal(t, StatusOK, result.Status)
}

// TestKeybindingsCheckIgnoresThePanelsOwnLeader covers the leader the panel binds itself.
func TestKeybindingsCheckIgnoresThePanelsOwnLeader(t *testing.T) {
	env := configEnv(config.Defaults(), nil, true)
	env.TmuxSocket = "komrad"
	env.ValidateKeybindings = ui.ValidateKeybindings
	env.IsPanelRunning = func(context.Context) bool { return true }
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "list-keys") {
			return `bind-key  -T root C-Space    if-shell -F "#{==:#{window_id},@1}" "switch-client -T komrad-leader" "send-keys C-Space"`, nil
		}

		return "C-b\n", nil
	}

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
}

// TestKeybindingsCheckIgnoresAKeyThePanelHasTaken covers a root binding the panel made itself,
// which hands the key through on its own pane and runs the user's command elsewhere.
func TestKeybindingsCheckIgnoresAKeyThePanelHasTaken(t *testing.T) {
	env := configEnv(config.Defaults(), nil, true)
	env.TmuxSocket = "komrad"
	env.ValidateKeybindings = ui.ValidateKeybindings
	env.IsPanelRunning = func(context.Context) bool { return true }
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "list-keys") {
			return `bind-key  -T root C-d    if-shell -F "#{==:#{pane_id},#{@komrad_panel_pane}}" send-keys "copy-mode -u"`, nil
		}

		return "C-b\n", nil
	}

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "no panel key is held by tmux", result.Detail)
}

// TestKeybindingsCheckWaitsForThePanelToTakeAKey covers the user's binding standing on a panel
// key while no panel is running, which the next panel rebinds as it starts.
func TestKeybindingsCheckWaitsForThePanelToTakeAKey(t *testing.T) {
	env := rootKeysEnv(config.Defaults(), "C-d", "C-u")
	env.IsPanelRunning = func(context.Context) bool { return false }

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "the panel takes C-d, C-u for its own pane when it opens", result.Detail)
}

// TestKeybindingsCheckFindsAKeyThePanelCouldNotTake covers the user's binding still standing on a
// panel key with the panel up.
func TestKeybindingsCheckFindsAKeyThePanelCouldNotTake(t *testing.T) {
	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(config.Defaults(), "C-d"))

	assert.Equal(t, StatusFail, result.Status)
	assert.Equal(t, "ctrl+d", result.Detail)
	assert.Contains(t, result.Fix, "reopen the panel")
	assert.Contains(t, result.Fix, "unbind -n C-d")
}

// TestKeybindingsCheckFindsTheLeaderUnavailable covers the leader as the prefix, which tmux reads
// before the root table, and the leader bound in the root table by the user's own tmux config.
func TestKeybindingsCheckFindsTheLeaderUnavailable(t *testing.T) {
	t.Run("as the prefix", func(t *testing.T) {
		result := newKeybindingsCheck().Run(t.Context(), rootKeysEnvWithPrefix(config.Defaults(), "C-Space"))

		assert.Equal(t, StatusFail, result.Status, "the prefix is read first, so the leader is dead")
		assert.Contains(t, result.Fix, "prefix")
	})

	t.Run("bound in the root table", func(t *testing.T) {
		result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(config.Defaults(), "C-Space"))

		assert.Equal(t, StatusFail, result.Status)
		assert.Contains(t, result.Fix, "unbind -n C-Space")
	})
}

// TestTheKeybindingsCheckIsRun pins the check being one doctor makes.
func TestTheKeybindingsCheckIsRun(t *testing.T) {
	checks := Checks(Settings{Harnesses: claudeAndCodex, TmuxSocket: "komrad"})

	assert.True(t, slices.ContainsFunc(checks, func(c Check) bool { return c.Name == "keybindings" }))
}

// TestYourTmuxKeysCheck covers the outer tmux komrad is opened inside, whose root table answers a
// key before the nested panel sees it.
func TestYourTmuxKeysCheck(t *testing.T) {
	passThrough := `run-shell "if ps -o comm= -t '#{pane_tty}' | grep -iqE '^(\\S+\\/)?tmux$'; then tmux send-keys C-d; else tmux copy-mode -u; fi"`

	tests := []struct {
		name       string
		insideTmux string
		listed     string
		prefix     string
		want       Status
		wantDetail string
	}{
		{
			name: "outside tmux there is nothing to check",
			want: StatusSkip,
		},
		{
			name:       "komrad's own server is the keybindings check's to read",
			insideTmux: "/tmp/tmux-501/komrad,1,0",
			want:       StatusSkip,
		},
		{
			name:       "a root table of mouse bindings lets everything through",
			insideTmux: "/tmp/tmux-501/default,1,0",
			listed:     "bind-key  -T root MouseDown1Pane    select-pane -t =",
			want:       StatusOK,
		},
		{
			name:       "a panel key bound for scrolling is lost",
			insideTmux: "/tmp/tmux-501/default,1,0",
			listed:     "bind-key  -T root C-d    copy-mode -u\nbind-key  -T root C-u    copy-mode -u",
			want:       StatusWarn,
			wantDetail: "C-d, C-u",
		},
		{
			name:       "the leader bound by the user is lost",
			insideTmux: "/tmp/tmux-501/default,1,0",
			listed:     "bind-key  -T root C-Space    last-window",
			want:       StatusWarn,
			wantDetail: "C-Space",
		},
		{
			name:       "a prefix on the leader is lost",
			insideTmux: "/tmp/tmux-501/default,1,0",
			prefix:     "C-Space",
			want:       StatusWarn,
			wantDetail: "prefix C-Space",
		},
		{
			name:       "a prefix on no panel key lets everything through",
			insideTmux: "/tmp/tmux-501/default,1,0",
			prefix:     "C-]",
			want:       StatusOK,
		},
		{
			name:       "a binding that hands the key to a pane running tmux lets it through",
			insideTmux: "/tmp/tmux-501/default,1,0",
			listed:     "bind-key  -T root C-d    " + passThrough,
			want:       StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := configEnv(config.Defaults(), nil, true)
			env.TmuxSocket = "komrad"
			env.InsideTmux = tt.insideTmux
			env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
				require.Equal(t, []string{"-S", "/tmp/tmux-501/default"}, args[:2])
				if slices.Contains(args, "prefix") {
					return tt.prefix, nil
				}
				if slices.Contains(args, "list-keys") {
					return tt.listed, nil
				}

				return "", nil
			}

			result := newYourTmuxKeysCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			if tt.want == StatusWarn {
				assert.Equal(t, tt.wantDetail, result.Detail)
			}
		})
	}
}
