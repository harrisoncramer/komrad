package errortextskip

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorText(t *testing.T) {
	assert.Contains(t, errors.New("boom").Error(), "bo")
}
