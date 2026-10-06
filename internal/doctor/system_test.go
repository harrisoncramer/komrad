package doctor

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKomradCheck covers the build a report quotes, so a bug filed against it names the binary
// that misbehaved rather than the one the reader has.
func TestKomradCheck(t *testing.T) {
	tests := []struct {
		name       string
		build      string
		want       Status
		wantDetail string
	}{
		{
			name:       "a binary that names no build",
			build:      "",
			want:       StatusSkip,
			wantDetail: "not checked, this binary did not say which build it is",
		},
		{
			name:       "a released build",
			build:      "v1.2.3 (abc1234)",
			want:       StatusOK,
			wantDetail: "v1.2.3 (abc1234)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newKomradCheck().Run(t.Context(), Env{Settings: Settings{Build: tt.build}})

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
		})
	}
}

// TestSystemCheck covers that the machine is always named, a release that cannot be read only
// shortening the line rather than turning it into a finding.
func TestSystemCheck(t *testing.T) {
	tests := []struct {
		name       string
		os         string
		arch       string
		swVers     string
		swVersErr  error
		osRelease  string
		readErr    error
		wantDetail string
	}{
		{
			name:       "macOS when sw_vers will not run",
			os:         "darwin",
			arch:       "arm64",
			swVersErr:  errors.New("exit status 1"),
			wantDetail: "darwin arm64",
		},
		{
			name:       "Linux with no os-release",
			os:         "linux",
			arch:       "amd64",
			readErr:    fs.ErrNotExist,
			wantDetail: "linux amd64",
		},
		{
			name:       "Linux whose os-release names no pretty name",
			os:         "linux",
			arch:       "amd64",
			osRelease:  "ID=alpine\nVERSION_ID=3.20.0\n",
			wantDetail: "linux amd64",
		},
		{
			name:       "a system with no release to read",
			os:         "freebsd",
			arch:       "amd64",
			wantDetail: "freebsd amd64",
		},
		{
			name:       "macOS",
			os:         "darwin",
			arch:       "arm64",
			swVers:     "15.6",
			wantDetail: "darwin arm64, macOS 15.6",
		},
		{
			name:       "Linux",
			os:         "linux",
			arch:       "amd64",
			osRelease:  "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nID=ubuntu\n",
			wantDetail: "linux amd64, Ubuntu 24.04.1 LTS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				OS:   tt.os,
				Arch: tt.arch,
				Output: func(context.Context, string, ...string) (string, error) {
					return tt.swVers, tt.swVersErr
				},
				ReadFile: func(string) ([]byte, error) {
					return []byte(tt.osRelease), tt.readErr
				},
			}

			result := newSystemCheck().Run(t.Context(), env)

			assert.Equal(t, StatusOK, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
		})
	}
}

// TestTerminalCheck covers that inside tmux the terminal is asked of tmux, since tmux sets
// TERM_PROGRAM to its own name in every pane it runs.
func TestTerminalCheck(t *testing.T) {
	tests := []struct {
		name       string
		settings   Settings
		tmuxNamed  string
		tmuxErr    error
		want       Status
		wantDetail string
	}{
		{
			name:       "outside tmux, in a terminal that names nothing",
			settings:   Settings{},
			want:       StatusSkip,
			wantDetail: "not checked, the terminal did not name itself",
		},
		{
			name:       "inside tmux, when tmux will not answer",
			settings:   Settings{InsideTmux: "/tmp/tmux-501/default,1,0"},
			tmuxErr:    errors.New("no server running"),
			want:       StatusSkip,
			wantDetail: "not checked, the terminal did not name itself",
		},
		{
			name:       "inside tmux, with no client attached to name either",
			settings:   Settings{InsideTmux: "/tmp/tmux-501/default,1,0"},
			tmuxNamed:  "\t",
			want:       StatusSkip,
			wantDetail: "not checked, the terminal did not name itself",
		},
		{
			name:       "inside tmux, under a terminal that never told tmux its version",
			settings:   Settings{InsideTmux: "/tmp/tmux-501/default,1,0"},
			tmuxNamed:  "\talacritty",
			want:       StatusOK,
			wantDetail: "alacritty",
		},
		{
			name:       "outside tmux, in a terminal naming itself but no version",
			settings:   Settings{TermProgram: "Apple_Terminal"},
			want:       StatusOK,
			wantDetail: "Apple_Terminal",
		},
		{
			name: "outside tmux",
			settings: Settings{
				TermProgram:        "ghostty",
				TermProgramVersion: "1.2.0",
			},
			want:       StatusOK,
			wantDetail: "ghostty 1.2.0",
		},
		{
			name: "inside tmux, which names itself and not the terminal",
			settings: Settings{
				InsideTmux:         "/tmp/tmux-501/default,1,0",
				TermProgram:        "tmux",
				TermProgramVersion: "3.5a",
			},
			tmuxNamed:  "ghostty 1.2.0\txterm-ghostty",
			want:       StatusOK,
			wantDetail: "ghostty 1.2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				Settings: tt.settings,
				Output: func(context.Context, string, ...string) (string, error) {
					return tt.tmuxNamed, tt.tmuxErr
				},
			}

			result := newTerminalCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantDetail, result.Detail)
		})
	}
}
