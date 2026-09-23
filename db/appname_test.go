package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHostOfReadsBackTheHostAppNameWrote covers a hostname long enough that the cut takes the pid.
func TestHostOfReadsBackTheHostAppNameWrote(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "a short host", host: "box", want: "box"},
		{name: "a 70 character host", host: strings.Repeat("h", 70), want: strings.Repeat("h", maxAppName-len(appNamePrefix)-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := appName(tt.host, 12345)

			assert.LessOrEqual(t, len(name), maxAppName)
			assert.Equal(t, tt.want, hostOf(name))
		})
	}
}
