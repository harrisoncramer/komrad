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

func (t *panelKeyRecorder) SetGlobalChecked(name, value string) error {
	t.SetGlobal(name, value)

	return t.err
}

func (t *panelKeyRecorder) BindKeyChecked(key string, command ...string) error {
	t.BindKey(key, command...)

	return t.err
}

// TestBindPanelKey pins the binding naming this binary's own path, since a tmux server keeps
// whatever PATH it started with for life.
func TestBindPanelKey(t *testing.T) {
	tests := []struct {
		name     string
		chosen   string
		open     string
		wantKey  string
		wantOpen string
	}{
		{
			name:     "no chosen key falls back to the default",
			chosen:   "",
			wantKey:  defaultPanelKey,
			wantOpen: "0",
		},
		{
			name:     "the user's own key wins",
			chosen:   "w",
			wantKey:  "w",
			wantOpen: "0",
		},
		{
			name:     "a popup that is already up keeps saying so",
			open:     "1",
			wantKey:  defaultPanelKey,
			wantOpen: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &panelKeyRecorder{}
			if tt.chosen != "" {
				tmux.SetGlobal(managerKeyOption, tt.chosen)
			}
			if tt.open != "" {
				tmux.SetGlobal(managerOpenFlag, tt.open)
			}

			require.NoError(t, BindPanelKey(tmux, BindPanelKeyParams{
				Self:        "/usr/local/bin/weave",
				ConfigPaths: []string{"/etc/weave.toml", "/home/dev/weave.toml"},
			}))

			command, bound := tmux.bound[tt.wantKey]
			require.True(t, bound, "the panel key is not bound")

			assert.Equal(t, tt.wantOpen, tmux.globals[managerOpenFlag])
			assert.Equal(t, []string{
				"if-shell", "-F", "#{==:#{@weave_open},1}",
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

// TestIsShownTreatsAnUnreadableFlagAsShown covers the panel attached in a terminal by hand,
// where no user server was ever recorded and so the flag can be neither written nor read, which
// read as hidden would leave every unviewed mark on screen with no way to clear one.
func TestIsShownTreatsAnUnreadableFlagAsShown(t *testing.T) {
	tests := []struct {
		name string
		flag string
		want bool
	}{
		{name: "no user server has ever opened a popup", want: true},
		{
			name: "a popup is attached",
			flag: "1",
			want: true,
		},
		{name: "the popup is hidden", flag: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{}
			if tt.flag != "" {
				tmux.SetGlobal(managerOpenFlag, tt.flag)
			}

			assert.Equal(t, tt.want, NewPopupWindow(tmux, "test-session").IsShown())
		})
	}
}
