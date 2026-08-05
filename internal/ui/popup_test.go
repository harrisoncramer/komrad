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
