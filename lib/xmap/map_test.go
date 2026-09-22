package xmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetMakesTheMapOnTheFirstWrite pins that a nil map is written to rather than panicked on.
func TestSetMakesTheMapOnTheFirstWrite(t *testing.T) {
	t.Parallel()

	var held map[string]int
	Set(&held, "a", 1)
	Set(&held, "b", 2)
	Set(&held, "a", 3)

	assert.Equal(t, map[string]int{"a": 3, "b": 2}, held)
}
