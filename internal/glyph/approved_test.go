package glyph_test

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"komrad/internal/glyph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoApprovedCharacterIsAnEmoji covers the approved set against Unicode's own list, since a
// terminal may draw an emoji two columns wide.
func TestNoApprovedCharacterIsAnEmoji(t *testing.T) {
	emoji := readEmoji(t)
	require.NotEmpty(t, emoji)

	for _, c := range glyph.ApprovedSet() {
		assert.False(t, emoji[c.Rune], "%s U+%04X carries the Emoji property", c.Name, c.Rune)
	}
}

// TestApprovedSetCounts pins the size of the set, so a new Nerd Fonts version is noticed.
func TestApprovedSetCounts(t *testing.T) {
	assert.Equal(t, "3.5.1", glyph.NerdFontsVersion)
	assert.Len(t, glyph.ApprovedSet(), 308+19)
}

// readEmoji is every rune testdata/emoji-data.txt gives the Emoji property.
func readEmoji(t *testing.T) map[rune]bool {
	t.Helper()

	file, err := os.Open("testdata/emoji-data.txt")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	emoji := map[rune]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		points, property, ok := strings.Cut(line, ";")
		if !ok || strings.TrimSpace(property) != "Emoji" {
			continue
		}

		first, last, isRange := strings.Cut(strings.TrimSpace(points), "..")
		if !isRange {
			last = first
		}

		from, err := strconv.ParseInt(first, 16, 32)
		require.NoError(t, err)
		to, err := strconv.ParseInt(last, 16, 32)
		require.NoError(t, err)

		for r := from; r <= to; r++ {
			emoji[rune(r)] = true
		}
	}
	require.NoError(t, scanner.Err())

	return emoji
}
