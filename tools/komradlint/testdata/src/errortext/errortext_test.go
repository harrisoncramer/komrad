package errortext

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type text struct{}

func (text) Error() int { return 0 }

func TestErrorText(t *testing.T) {
	err := errors.New("boom")
	want := errors.New("boom")

	assert.Contains(t, err.Error(), "bo")           // want `a test hands ErrorContains the error itself, not the text of Contains`
	assert.Containsf(t, err.Error(), "bo", "%d", 1) // want `a test hands ErrorContainsf the error itself, not the text of Containsf`
	require.Contains(t, err.Error(), "bo")          // want `a test hands ErrorContains the error itself, not the text of Contains`
	assert.Equal(t, "boom", err.Error())            // want `a test hands EqualError the error itself, not the text of Equal`
	assert.Equalf(t, "boom", err.Error(), "%d", 1)  // want `a test hands EqualErrorf the error itself, not the text of Equalf`
	require.Equal(t, want.Error(), err.Error())     // want `a test hands EqualError the error itself, not the text of Equal`
	assert.Equal(t, err.Error(), "boom")            // want `a test hands EqualError the error itself, not the text of Equal`

	assert.Contains(t, "boom", err.Error())
	assert.NotContains(t, err.Error(), "x")
	assert.Equal(t, 0, text{}.Error())
	(&assert.Assertions{}).Contains(err.Error(), "bo")
}
