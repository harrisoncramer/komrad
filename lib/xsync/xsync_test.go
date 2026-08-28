package xsync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestWithinTimeoutGivesUp pins that a hang is left behind rather than holding the process
// open for good.
func TestWithinTimeoutGivesUp(t *testing.T) {
	assert.True(t, WithinTimeout(5*time.Second, func() {}), "work that finishes is waited for")

	blocked := make(chan struct{})
	defer close(blocked)

	assert.False(t, WithinTimeout(10*time.Millisecond, func() { <-blocked }), "work that hangs is left behind")
}
