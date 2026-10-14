package doctor

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
