package doctor

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEnv answers for tmux without one being installed, which is the whole reason the
// checks reach the machine through functions.
func fakeEnv(path string, lookErr error, reported string, outErr error) Env {
	return Env{
		LookPath: func(string) (string, error) { return path, lookErr },
		Output:   func(context.Context, string, ...string) (string, error) { return reported, outErr },
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
		{
			name:     "OpenBSD's base tmux, which reports the OS release and not tmux's",
			reported: "tmux openbsd-7.6",
			wantErr:  true,
		},
		{
			name:     "a build naming no version at all",
			reported: "tmux master",
			wantErr:  true,
		},
		{
			name:     "a major with nothing after it",
			reported: "tmux 3",
			wantErr:  true,
		},
		{
			name:     "nothing",
			reported: "",
			wantErr:  true,
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

// TestTmuxVersionOrdering covers the comparison, including the two-digit minor that a
// string comparison would order wrongly.
func TestTmuxVersionOrdering(t *testing.T) {
	tests := []struct {
		name  string
		have  tmuxVersion
		floor tmuxVersion
		want  bool
	}{
		{name: "the same version", have: tmuxVersion{3, 3}, floor: tmuxVersion{3, 3}, want: true},
		{name: "a newer minor", have: tmuxVersion{3, 4}, floor: tmuxVersion{3, 3}, want: true},
		{name: "an older minor", have: tmuxVersion{3, 2}, floor: tmuxVersion{3, 3}, want: false},
		{name: "a newer major", have: tmuxVersion{4, 0}, floor: tmuxVersion{3, 3}, want: true},
		{name: "an older major with a bigger minor", have: tmuxVersion{2, 9}, floor: tmuxVersion{3, 3}, want: false},
		{name: "a two-digit minor over a one-digit floor", have: tmuxVersion{3, 10}, floor: tmuxVersion{3, 9}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.have.atLeast(tt.floor))
		})
	}
}

// TestTmuxBinaryCheck covers the two answers the check has, and that the failure carries a
// code rather than only a sentence.
func TestTmuxBinaryCheck(t *testing.T) {
	t.Run("tmux is installed", func(t *testing.T) {
		result := TmuxBinary().Run(t.Context(), fakeEnv("/usr/bin/tmux", nil, "", nil))

		assert.Equal(t, StatusOK, result.Status)
		assert.Equal(t, "/usr/bin/tmux", result.Detail, "where it landed is reported, since two tmuxes on one machine is the confusing case")
	})

	t.Run("tmux is not installed", func(t *testing.T) {
		result := TmuxBinary().Run(t.Context(), fakeEnv("", exec.ErrNotFound, "", nil))

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
			name:     "exactly the floor",
			reported: "tmux 3.3a",
			want:     StatusOK,
		},
		{
			name:     "newer than the floor",
			reported: "tmux 3.7b",
			want:     StatusOK,
		},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TmuxVersion().Run(t.Context(), fakeEnv("/usr/bin/tmux", nil, tt.reported, tt.outErr))

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
