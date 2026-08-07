package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActionValidate covers what Validate objects to, independent of how the Action value
// was built, since a cookbook constructs one by hand rather than through Discover.
func TestActionValidate(t *testing.T) {
	tests := []struct {
		name           string
		action         Action
		wantErr        error
		wantErrContain string
	}{
		{
			name:    "not executable",
			action:  Action{Executable: false, Text: true, Header: Header{Shebang: true}},
			wantErr: ErrNotExecutable,
		},
		{
			name:    "text with no shebang",
			action:  Action{Executable: true, Text: true, Header: Header{}},
			wantErr: ErrNotAScript,
		},
		{
			name:           "a header directive weave could not read",
			action:         Action{Executable: true, Text: true, Header: Header{Shebang: true, Problems: []string{"line 2: bogus is not a directive weave knows"}}},
			wantErrContain: "bogus is not a directive weave knows",
		},
		{
			name:   "a script with a shebang and a clean header",
			action: Action{Executable: true, Text: true, Header: Header{Shebang: true}},
		},
		{
			name:   "a compiled program needs no shebang",
			action: Action{Executable: true, Text: false, Header: Header{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.action.Validate()

			switch {
			case tt.wantErr != nil:
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			case tt.wantErrContain != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContain)
			default:
				assert.NoError(t, err)
			}
		})
	}
}

// TestDescribe covers reading one file the way Discover reads each entry in a directory,
// which is what lets a cookbook validate a script the same way without a directory of its own.
func TestDescribe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setup")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n#WEAVE description=\"sets up\"\n"), 0o755))

	action, err := Describe(path)
	require.NoError(t, err)

	assert.Equal(t, "setup", action.Name)
	assert.Equal(t, path, action.Path)
	assert.True(t, action.Executable)
	assert.True(t, action.Text)
	assert.Equal(t, "sets up", action.Header.Description)

	t.Run("a directory is not a file to describe", func(t *testing.T) {
		_, err := Describe(dir)
		assert.Error(t, err)
	})

	t.Run("a file that is not there", func(t *testing.T) {
		_, err := Describe(filepath.Join(dir, "gone"))
		assert.Error(t, err)
	})
}

// TestValidateAsInstalled covers a cookbook validating a script that has not been installed
// yet, and so is not executable on disk regardless of whether it is a real script: the
// executable bit InstallActions always sets is assumed rather than read.
func TestValidateAsInstalled(t *testing.T) {
	dir := t.TempDir()

	script := filepath.Join(dir, "setup")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"), 0o644))
	assert.NoError(t, ValidateAsInstalled(script), "not yet executable, but it will be once installed")

	note := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(note, []byte("just some prose\n"), 0o644))
	err := ValidateAsInstalled(note)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotAScript)

	info, err := os.Stat(note)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o111, "validating never touches the file on disk")
}
