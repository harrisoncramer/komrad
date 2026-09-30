package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUndeclaredColumnsGiveTheirRoomToTheName pins what a column not declared is for: a name in
// a result list has the room the three columns would have taken.
func TestUndeclaredColumnsGiveTheirRoomToTheName(t *testing.T) {
	assert.Equal(t, drawnResultNameColumn, gutterColumns(NoColumns()))
	assert.Greater(t, nameColumns(canonicalPane, NoColumns()), nameColumns(canonicalPane, everyColumn()))
}
