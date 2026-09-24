package label

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPath covers the tilde standing in for the home directory and nothing else.
func TestPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		home string
		want string
	}{
		{
			name: "no home leaves the path alone",
			path: "/Users/me/src",
			home: "",
			want: "/Users/me/src",
		},
		{
			name: "no path stays empty",
			path: "",
			home: "/Users/me",
			want: "",
		},
		{
			name: "a path beside the home directory is not under it",
			path: "/Users/meta/src",
			home: "/Users/me",
			want: "/Users/meta/src",
		},
		{
			name: "a path elsewhere is spelled as given",
			path: "/opt/src",
			home: "/Users/me",
			want: "/opt/src",
		},
		{
			name: "the home directory itself is a bare tilde",
			path: "/Users/me",
			home: "/Users/me",
			want: "~",
		},
		{
			name: "a path under home starts with the tilde",
			path: "/Users/me/src/komrade",
			home: "/Users/me",
			want: "~/src/komrade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Path(tt.path, tt.home))
		})
	}
}
