package paint

import (
	"strconv"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestDocRowsFitTheColumn covers a message carrying an indented code block, whose tabs the
// terminal draws at a width and the panel's own measuring does not, and one carrying a token
// too long to break on a word, which the word wrap leaves wider than the column.
func TestDocRowsFitTheColumn(t *testing.T) {
	bodies := map[string]string{
		"a code block held with tabs": "Here is the queue:\n\n    125:\t\t\tqueues.SupervisorReconciliation: {\n    135:\t\t\t\t\tGlobalLimit: 1,\n\nSo it is there.\n",
		"a link nothing will break":   "The run is at https://github.com/acme/gadgets/actions/runs/1234567890/job/9876543210 and it failed.\n",
	}

	for name, body := range bodies {
		for _, width := range []int{40, 50, 60, 80} {
			t.Run(name+" at "+strconv.Itoa(width), func(t *testing.T) {
				for _, row := range Panel.DocRows(body, width) {
					plain := ansi.Strip(row)

					assert.NotContains(t, plain, "\t", "a row holds no tab for the terminal to widen")
					assert.LessOrEqual(t, ansi.StringWidth(row), width, "the row fits its column: %q", plain)
				}
			})
		}
	}
}

// TestExpandTabs covers the stops the tabs are taken to, counted from the start of each line.
func TestExpandTabs(t *testing.T) {
	assert.Equal(t, "    ", expandTabs("\t"))
	assert.Equal(t, "ab  cd", expandTabs("ab\tcd"))
	assert.Equal(t, "abc d", expandTabs("abc\td"))
	assert.Equal(t, "a   \nbc  d", expandTabs("a\t\nbc\td"))
	assert.Equal(t, "no tabs here", expandTabs("no tabs here"))
}
