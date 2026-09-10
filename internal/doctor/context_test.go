package doctor

import (
	"errors"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
)

// contextEnv answers the config and whatever the file at the configured path turns out to be.
func contextEnv(file string, state DirState, inspectErr error) Env {
	cfg := config.Defaults()
	cfg.Context.File = file

	env := configEnv(cfg, nil, true)
	env.InspectDir = func(string) (DirState, error) { return state, inspectErr }

	return env
}

// TestContextCheck covers the standing text agents are started with, which the panel resolves at
// startup and logs at debug when it cannot, so a named file that is not there says nothing.
func TestContextCheck(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		state      DirState
		inspectErr error
		want       Status
		code       string
	}{
		{
			name: "a named file that is not there",
			file: "/home/u/notes/context.md",
			want: StatusFail,
			code: ErrContextFileAbsent.Code,
		},
		{
			name:  "a named path that is a directory",
			file:  "/home/u/notes",
			state: DirState{Present: true, Directory: true},
			want:  StatusFail,
			code:  ErrContextFileNotAFile.Code,
		},
		{
			name:       "a file that cannot be read",
			file:       "/home/u/notes/context.md",
			inspectErr: errors.New("permission denied"),
			want:       StatusFail,
			code:       ErrContextFileUnreadable.Code,
		},
		{
			name: "no file named and none at the default path",
			want: StatusOK,
		},
		{
			name:  "a named file that is there",
			file:  "/home/u/notes/context.md",
			state: DirState{Present: true},
			want:  StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newContextCheck().Run(t.Context(), contextEnv(tt.file, tt.state, tt.inspectErr))

			assert.Equal(t, tt.want, result.Status)
			if tt.code != "" {
				assert.Equal(t, tt.code, codeOf(result.Err))
			}
		})
	}
}

// TestTheContextCheckIsRun pins the check being one doctor makes, a check nothing runs being
// worth nothing.
func TestTheContextCheckIsRun(t *testing.T) {
	checks := Checks(Settings{Harnesses: claudeAndCodex, TmuxSocket: "weave"})

	found := false
	for _, check := range checks {
		if check.Name == "context" {
			found = true
		}
	}

	assert.True(t, found)
}
