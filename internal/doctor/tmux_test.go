package doctor

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tmuxOnPath answers for whether tmux resolves, which is all the binary check asks.
func tmuxOnPath(path string, err error) Env {
	return Env{
		LookPath: func(string) (string, error) { return path, err },
	}
}

// tmuxReporting answers for what "tmux -V" prints, for the version check.
func tmuxReporting(reported string, err error) Env {
	return Env{
		LookPath: func(string) (string, error) { return "/usr/bin/tmux", nil },
		Output:   func(context.Context, string, ...string) (string, error) { return reported, err },
	}
}

// TestParseTmuxVersion covers the spellings "tmux -V" comes back with. The version gates
// whether the panel's key binding works at all, so reading one wrong either refuses a
// machine that would have worked or accepts one that will not.
func TestParseTmuxVersion(t *testing.T) {
	tests := []struct {
		name     string
		reported string
		want     tmuxVersion
		wantErr  bool
	}{
		{
			name:     "nothing",
			reported: "",
			wantErr:  true,
		},
		{
			name:     "a major with nothing after it",
			reported: "tmux 3",
			wantErr:  true,
		},
		{
			name:     "a build naming no version at all",
			reported: "tmux master",
			wantErr:  true,
		},
		{
			name:     "OpenBSD's base tmux, which reports the OS release and not tmux's",
			reported: "tmux openbsd-7.6",
			wantErr:  true,
		},
		{
			name:     "an ordinary release",
			reported: "tmux 3.4",
			want:     tmuxVersion{major: 3, minor: 4},
		},
		{
			name:     "a point release, whose letter is not part of the number",
			reported: "tmux 3.3a",
			want:     tmuxVersion{major: 3, minor: 3},
		},
		{
			name:     "the version this was written against",
			reported: "tmux 3.7b",
			want:     tmuxVersion{major: 3, minor: 7},
		},
		{
			name:     "a build from master, named for the release it is heading for",
			reported: "tmux next-3.8",
			want:     tmuxVersion{major: 3, minor: 8},
		},
		{
			name:     "a release candidate",
			reported: "tmux 3.5-rc",
			want:     tmuxVersion{major: 3, minor: 5},
		},
		{
			name:     "a two-digit minor, which must not be read as one",
			reported: "tmux 3.10",
			want:     tmuxVersion{major: 3, minor: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTmuxVersion(tt.reported)
			if tt.wantErr {
				require.Error(t, err, "an unreadable version is refused rather than guessed at")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTmuxVersionOrdering covers the comparison, including the two-digit minor a string
// comparison would order wrongly.
func TestTmuxVersionOrdering(t *testing.T) {
	tests := []struct {
		name  string
		have  tmuxVersion
		floor tmuxVersion
		want  bool
	}{
		{
			name:  "an older minor",
			have:  tmuxVersion{major: 3, minor: 2},
			floor: tmuxVersion{major: 3, minor: 3},
			want:  false,
		},
		{
			name:  "an older major with a bigger minor",
			have:  tmuxVersion{major: 2, minor: 9},
			floor: tmuxVersion{major: 3, minor: 3},
			want:  false,
		},
		{
			name:  "the same version",
			have:  tmuxVersion{major: 3, minor: 3},
			floor: tmuxVersion{major: 3, minor: 3},
			want:  true,
		},
		{
			name:  "a newer minor",
			have:  tmuxVersion{major: 3, minor: 4},
			floor: tmuxVersion{major: 3, minor: 3},
			want:  true,
		},
		{
			name:  "a newer major",
			have:  tmuxVersion{major: 4, minor: 0},
			floor: tmuxVersion{major: 3, minor: 3},
			want:  true,
		},
		{
			name:  "a two-digit minor over a one-digit floor",
			have:  tmuxVersion{major: 3, minor: 10},
			floor: tmuxVersion{major: 3, minor: 9},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.have.isAtLeast(tt.floor))
		})
	}
}

// TestTmuxBinaryCheck covers the two answers the check has, and that the failure carries a
// code rather than only a sentence.
func TestTmuxBinaryCheck(t *testing.T) {
	t.Run("tmux is installed", func(t *testing.T) {
		result := TmuxBinary().Run(t.Context(), tmuxOnPath("/usr/bin/tmux", nil))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "/usr/bin/tmux", result.Detail, "where it landed is reported, since two tmuxes on one machine is the confusing case")
	})

	t.Run("tmux is not installed", func(t *testing.T) {
		result := TmuxBinary().Run(t.Context(), tmuxOnPath("", exec.ErrNotFound))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "tmux_missing", CodeOf(result.Err))
		assert.NotEmpty(t, result.Fix, "a failure a person can act on carries the command that acts on it")
	})
}

// TestTmuxVersionCheck covers each way the version can land, including the fork whose
// version cannot be read, which warns rather than failing.
func TestTmuxVersionCheck(t *testing.T) {
	tests := []struct {
		name     string
		reported string
		outErr   error
		want     Status
		wantCode string
	}{
		{
			name:     "below the floor",
			reported: "tmux 3.1c",
			want:     StatusFail,
			wantCode: "tmux_too_old",
		},
		{
			name:     "one minor below the floor, the off-by-one",
			reported: "tmux 3.2",
			want:     StatusFail,
			wantCode: "tmux_too_old",
		},
		{
			name:     "a version that cannot be read is not a refusal",
			reported: "tmux openbsd-7.6",
			want:     StatusWarn,
			wantCode: "tmux_version_unreadable",
		},
		{
			name:     "a tmux that will not run",
			outErr:   errors.New("permission denied"),
			want:     StatusFail,
			wantCode: "tmux_not_runnable",
		},
		{
			name:     "exactly the floor",
			reported: "tmux 3.3a",
			want:     StatusOK,
		},
		{
			name:     "newer than the floor",
			reported: "tmux 3.7b",
			want:     StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TmuxVersion().Run(t.Context(), tmuxReporting(tt.reported, tt.outErr))

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, CodeOf(result.Err))
		})
	}
}

// TestTmuxVersionNeedsTheBinary covers that the version check does not repeat the missing
// binary as a second failure, which is what the declared dependency is for.
func TestTmuxVersionNeedsTheBinary(t *testing.T) {
	assert.Contains(t, TmuxVersion().Needs, TmuxBinary().Name)
}

// tmuxServers fakes the tmux servers that are up. Keys are the socket flag a call carries,
// "-L weave" for weave's own and "" for the user's, so a test says which servers answer
// and what each one reports.
func tmuxServers(answers map[string]string) Env {
	return Env{
		Settings: Settings{TmuxSocket: "weave", InsideTmux: "/tmp/tmux-501/default,1,0"},
		LookPath: func(string) (string, error) { return "/usr/bin/tmux", nil },
		Output: func(_ context.Context, _ string, args ...string) (string, error) {
			socket := ""
			if len(args) > 1 && args[0] == "-L" {
				socket = args[0] + " " + args[1]
			}

			answer, running := answers[socket]
			if !running {
				return "", errors.New("no server running on that socket")
			}

			return answer, nil
		},
	}
}

// TestTmuxShellCheck covers what the option can say and which server was asked. The shell
// decides whether an agent launches at all, and the service refuses to start over it, so a
// wrong answer here sends someone looking at the wrong thing.
func TestTmuxShellCheck(t *testing.T) {
	tests := []struct {
		name       string
		answers    map[string]string
		want       Status
		wantCode   string
		wantDetail string
	}{
		{
			name:     "a shell that cannot parse the launch",
			answers:  map[string]string{"-L weave": "/usr/bin/fish"},
			want:     StatusFail,
			wantCode: "tmux_shell_unsupported",
		},
		{
			name:     "the same shell under a path tmux spelled differently",
			answers:  map[string]string{"-L weave": "/opt/homebrew/bin/fish"},
			want:     StatusFail,
			wantCode: "tmux_shell_unsupported",
		},
		{
			name:       "an option tmux does not have set",
			answers:    map[string]string{"-L weave": ""},
			want:       StatusOK,
			wantDetail: "tmux names no default-shell, so panes take the system one",
		},
		{
			name:       "weave's server is not up, so the user's answers",
			answers:    map[string]string{"": "/bin/bash"},
			want:       StatusOK,
			wantDetail: "/bin/bash (your tmux server)",
		},
		{
			name:     "no server is up at all",
			answers:  map[string]string{},
			want:     StatusWarn,
			wantCode: "tmux_no_server",
		},
		{
			name:       "weave's own server names a shell an agent can launch from",
			answers:    map[string]string{"-L weave": "/bin/zsh"},
			want:       StatusOK,
			wantDetail: "/bin/zsh (weave's tmux server)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TmuxShell().Run(t.Context(), tmuxServers(tt.answers))

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, CodeOf(result.Err))
			if tt.wantDetail != "" {
				assert.Equal(t, tt.wantDetail, result.Detail)
			}
		})
	}
}

// TestTmuxShellStartsNoServer covers the promise doctor makes about itself. Asking tmux for
// an option starts a server when none is running, so the call has to be one that cannot,
// or a diagnostic would be creating the thing it reports on.
func TestTmuxShellStartsNoServer(t *testing.T) {
	var calls [][]string

	env := Env{
		Settings: Settings{TmuxSocket: "weave"},
		LookPath: func(string) (string, error) { return "/usr/bin/tmux", nil },
		Output: func(_ context.Context, _ string, args ...string) (string, error) {
			calls = append(calls, args)

			return "", errors.New("no server running on /tmp/tmux-501/weave")
		},
	}

	TmuxShell().Run(t.Context(), env)

	require.NotEmpty(t, calls)
	for _, args := range calls {
		assert.NotContains(t, args, "start-server", "doctor never starts a tmux server")
		assert.NotContains(t, args, "new-session")
		assert.Contains(t, args, "show-option", "reading an option is all it does")
	}

	assert.Len(t, calls, 1, "the user's server is not addressed when this is not running inside one")
}
