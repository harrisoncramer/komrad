package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bindingTmux struct {
	globals map[string]string
	bound   map[string][]string
}

func (t *bindingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *bindingTmux) ShowGlobal(name string) string           { return t.globals[name] }
func (t *bindingTmux) GetSessionClient(string) string          { return "" }
func (t *bindingTmux) SetSessionOption(string, string, string) {}
func (t *bindingTmux) DetachSession(string)                    {}

func (t *bindingTmux) BindKey(key string, command ...string) {
	if t.bound == nil {
		t.bound = map[string][]string{}
	}

	t.bound[key] = command
}

type panelKeyRecorder struct {
	bindingTmux
	err error
}

func (t *panelKeyRecorder) BindKeyChecked(key string, command ...string) error {
	t.BindKey(key, command...)

	return t.err
}

// TestBindPanelKey pins the binding naming this binary's own path, since a tmux server keeps
// whatever PATH it started with for life, and pins both branches going through it, since the
// key press decides nothing for itself.
func TestBindPanelKey(t *testing.T) {
	tests := []struct {
		name    string
		chosen  string
		wantKey string
	}{
		{
			name:    "no chosen key falls back to the default",
			chosen:  "",
			wantKey: defaultPanelKey,
		},
		{
			name:    "the user's own key wins",
			chosen:  "w",
			wantKey: "w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &panelKeyRecorder{}
			if tt.chosen != "" {
				tmux.SetGlobal(managerKeyOption, tt.chosen)
			}

			require.NoError(t, BindPanelKey(tmux, BindPanelKeyParams{
				Self:        "/usr/local/bin/weave",
				ConfigPaths: []string{"/etc/weave.toml", "/home/dev/weave.toml"},
			}))

			command, bound := tmux.bound[tt.wantKey]
			require.True(t, bound, "the panel key is not bound")

			assert.NotContains(t, tmux.globals, "@weave_open",
				"the binding writes no flag a later press could read back as stale")
			assert.Equal(t, []string{
				"if-shell",
				"/usr/local/bin/weave shown --config /etc/weave.toml --config /home/dev/weave.toml",
				"run-shell -b '/usr/local/bin/weave hide --config /etc/weave.toml --config /home/dev/weave.toml'",
				"display-popup -w 100% -h 100% -x C -y C -T ' weave ' " +
					"-EE '/usr/local/bin/weave open --config /etc/weave.toml --config /home/dev/weave.toml'",
			}, command)
		})
	}
}

// TestBindPanelKeyReportsRefusal pins the refusal reaching the person who asked for it, since
// a tmux that is not running is what a rejected binding looks like.
func TestBindPanelKeyReportsRefusal(t *testing.T) {
	tmux := &panelKeyRecorder{err: assert.AnError}

	assert.Error(t, BindPanelKey(tmux, BindPanelKeyParams{Self: "/usr/local/bin/weave"}))
}

// TestIsShown covers the panel asking whether anybody is looking at it, which is a client on
// its own session rather than anything a popup wrote down as it opened.
func TestIsShown(t *testing.T) {
	tests := []struct {
		name   string
		client string
		want   bool
	}{
		{
			name:   "a client is attached to the panel's session",
			client: "/dev/ttys004",
			want:   true,
		},
		{name: "the popup was hidden, which detached it"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{sessionClient: tt.client}

			assert.Equal(t, tt.want, NewPopupWindow(tmux, "test-session").IsShown())
		})
	}
}

// TestBindHide pins the key the popup closes on being the one handed in, since it is read off
// the user's own server and this client is bound to the manager's.
func TestBindHide(t *testing.T) {
	tests := []struct {
		name    string
		chosen  string
		wantKey string
	}{
		{
			name:    "no chosen key falls back to the default",
			wantKey: defaultPanelKey,
		},
		{
			name:    "the user's own key wins",
			chosen:  "w",
			wantKey: "w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &bindingTmux{}

			NewPopupWindow(tmux, "manager").BindHide(tt.chosen)

			assert.Equal(t, []string{"detach-client", "-s", "manager"}, tmux.bound[tt.wantKey],
				"the panel key is not bound to hiding the popup")
		})
	}
}

// TestUserPanelKey pins the key being read off the one server the user can have set it on.
func TestUserPanelKey(t *testing.T) {
	tmux := &bindingTmux{}
	tmux.SetGlobal(managerKeyOption, "w")

	assert.Equal(t, "w", UserPanelKey(tmux))
}
