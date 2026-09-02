package ui

import (
	"strings"
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

// TestBindPalette pins the command naming the running binary and the config this process
// resolved, since the popup is a new process that would otherwise read the defaults.
func TestBindPalette(t *testing.T) {
	tests := []struct {
		name    string
		chosen  string
		wantKey string
	}{
		{
			name:    "no chosen key falls back to the default",
			chosen:  "",
			wantKey: defaultPaletteKey,
		},
		{
			name:    "the user's own key wins",
			chosen:  "x",
			wantKey: "x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &bindingTmux{}
			if tt.chosen != "" {
				tmux.SetGlobal(paletteKeyOption, tt.chosen)
			}

			NewPopupWindow(tmux, "manager").BindPalette(BindPaletteParams{
				Self:        "/usr/local/bin/weave",
				ConfigPaths: []string{"/etc/weave.toml", "/home/dev/weave.toml"},
			})

			command, bound := tmux.bound[tt.wantKey]
			require.True(t, bound, "the palette key is not bound")

			line := strings.Join(command, " ")
			assert.Contains(t, line, "display-popup")
			assert.Contains(t, line, "-EE /usr/local/bin/weave palette",
				"the popup stays up on a refusal, and runs this binary rather than one on the path")
			assert.Contains(t, line, "--config /etc/weave.toml --config /home/dev/weave.toml",
				"the palette reads the config layers the panel was started on")
		})
	}
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
