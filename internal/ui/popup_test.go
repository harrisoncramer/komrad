package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindingTmux records the keys bound on the manager's server and answers the options a
// binding reads off the user's.
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

// panelKeyRecorder is the user's own tmux server, the one the panel key is bound on, where
// every call reports whether tmux took it.
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

// TestBindPanelKey covers the binding weave is reached by, which a tmux config asks for and
// nobody reads afterwards. It has to name this binary's own path, since a tmux server keeps
// whatever PATH it started with for life, and it has to branch on the flag the popup sets so
// the same key hides the panel rather than stacking a second popup on it.
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

			require.NoError(t, BindPanelKey(tmux, "/usr/local/bin/weave"))

			command, bound := tmux.bound[tt.wantKey]
			require.True(t, bound, "the panel key is not bound")

			// Binding by hand while the panel is up must not report it closed, since the next
			// press would then stack a second popup rather than hiding the one there.
			assert.Equal(t, tt.wantOpen, tmux.globals[managerOpenFlag])
			assert.Equal(t, []string{
				"if-shell", "-F", "#{==:#{@weave_open},1}",
				"run-shell -b '/usr/local/bin/weave hide'",
				"display-popup -w 100% -h 100% -x C -y C -T ' weave ' -EE '/usr/local/bin/weave open'",
			}, command)
		})
	}
}

// TestBindPanelKeyReportsRefusal covers a tmux that will not take the binding, which is what a
// server that is not running looks like. The panel binds keys where nothing can be done about
// a failure, but this one was asked for by somebody reading the output.
func TestBindPanelKeyReportsRefusal(t *testing.T) {
	tmux := &panelKeyRecorder{err: assert.AnError}

	assert.Error(t, BindPanelKey(tmux, "/usr/local/bin/weave"))
}

// TestBindPalette covers the binding the palette is reached by, which is the only way in.
// The command has to name the running binary and the config this process resolved, since the
// popup is a new process that would otherwise read whatever the defaults are.
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
