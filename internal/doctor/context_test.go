package doctor

import (
	"io/fs"
	"syscall"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
)

// contextEnv answers the config and whatever reading the file at the configured path gives back.
func contextEnv(file string, text string, readErr error) Env {
	cfg := config.Defaults()
	cfg.Context.File = file

	env := configEnv(cfg, nil, true)
	env.ReadFile = func(string) ([]byte, error) { return []byte(text), readErr }

	return env
}

// TestContextCheck covers the standing text agents are started with, which the launcher reads and
// drops the error from, so a file weave cannot read leaves every agent without it.
func TestContextCheck(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		text    string
		readErr error
		want    Status
		code    string
	}{
		{
			name:    "a named file that is not there",
			file:    "/home/u/notes/context.md",
			readErr: fs.ErrNotExist,
			want:    StatusFail,
			code:    ErrContextFileAbsent.Code,
		},
		{
			name:    "a named path that is a directory",
			file:    "/home/u/notes",
			readErr: &fs.PathError{Op: "read", Path: "/home/u/notes", Err: syscall.EISDIR},
			want:    StatusFail,
			code:    ErrContextFileNotAFile.Code,
		},
		{
			name:    "a file that is there and cannot be read, which presence alone would pass",
			file:    "/home/u/notes/context.md",
			readErr: fs.ErrPermission,
			want:    StatusFail,
			code:    ErrContextFileUnreadable.Code,
		},
		{
			name:    "no file named and none at the default path",
			readErr: fs.ErrNotExist,
			want:    StatusOK,
		},
		{
			name: "a named file that reads",
			file: "/home/u/notes/context.md",
			text: "answer mail briefly",
			want: StatusOK,
		},
		{
			name: "a file named as spaces, which resolves to the default path",
			file: "   ",
			text: "",
			want: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newContextCheck().Run(t.Context(), contextEnv(tt.file, tt.text, tt.readErr))

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

	assert.True(t, containsCheck(checks, "context"))
}

func containsCheck(checks []Check, name string) bool {
	for _, check := range checks {
		if check.Name == name {
			return true
		}
	}

	return false
}
