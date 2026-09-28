package logview_test

import (
	"fmt"
	"testing"

	"komrad/internal/ui/logview"

	"github.com/stretchr/testify/assert"
)

func numbered(from, to int) []string {
	var lines []string
	for i := from; i <= to; i++ {
		lines = append(lines, fmt.Sprint("line ", i))
	}

	return lines
}

// TestTailFollows covers new lines arriving while following and after moving away.
func TestTailFollows(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(100)
	viewer.Append(numbered(1, 5))
	assert.Equal(t, []string{" line 3", " line 4", "›line 5"}, viewer.Rows(20, 3))

	viewer.Append(numbered(6, 6))
	assert.True(t, viewer.IsFollowing())
	assert.Equal(t, []string{" line 4", " line 5", "›line 6"}, viewer.Rows(20, 3))

	viewer.Move(-1)
	assert.False(t, viewer.IsFollowing())
	viewer.Append(numbered(7, 8))
	assert.Equal(t, []string{" line 4", "›line 5", " line 6"}, viewer.Rows(20, 3))

	viewer.Bottom()
	assert.True(t, viewer.IsFollowing())
	assert.Equal(t, []string{" line 6", " line 7", "›line 8"}, viewer.Rows(20, 3))
}

// TestTailMoves covers the cursor moving by log line, however many rows a line wraps to.
func TestTailMoves(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(100)
	viewer.Append([]string{"short", "a line that wraps", "last"})
	assert.Equal(t, []string{" a line t", " hat wrap", " s", "›last"}, viewer.Rows(9, 4))

	viewer.Move(-1)
	assert.Equal(t, []string{"›a line t", " hat wrap", " s", " last"}, viewer.Rows(9, 4))

	viewer.Top()
	assert.False(t, viewer.IsFollowing())
	assert.Equal(t, []string{"›short", " a line t", " hat wrap", " s"}, viewer.Rows(9, 4))

	viewer.Move(-5)
	assert.Equal(t, "›short", viewer.Rows(9, 4)[0], "the cursor stops at the first line")

	viewer.Move(10)
	assert.Equal(t, []string{" a line t", " hat wrap", " s", "›last"}, viewer.Rows(9, 4), "and at the last")
	assert.False(t, viewer.IsFollowing(), "moving down does not follow again")
}

// TestTailCeiling covers the oldest lines dropping once the viewer holds more than it keeps.
func TestTailCeiling(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(3)
	viewer.Append(numbered(1, 3))
	viewer.Top()
	viewer.Move(1)
	viewer.Append(numbered(4, 5))

	assert.Equal(t, 3, viewer.Len())
	assert.Equal(t, []string{"›line 3", " line 4", " line 5"}, viewer.Rows(20, 3), "the cursor stays on the oldest line kept")
}

// TestTailReset covers starting over, which empties the viewer and follows again.
func TestTailReset(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(100)
	viewer.Append(numbered(1, 3))
	viewer.Top()
	viewer.Reset()

	assert.Zero(t, viewer.Len())
	assert.True(t, viewer.IsFollowing())
	assert.Empty(t, viewer.Rows(20, 3))

	viewer.Append(numbered(9, 9))
	assert.Equal(t, []string{"›line 9"}, viewer.Rows(20, 3))
}

// TestTailSanitizes covers a line carrying escape sequences and control characters.
func TestTailSanitizes(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(100)
	viewer.Append([]string{"\x1b[31mred\x1b[0m\ttab\rbell\a"})

	assert.Equal(t, []string{"›red tabbell"}, viewer.Rows(40, 3))
}
