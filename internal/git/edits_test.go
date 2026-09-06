package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEditsAreOneRunApiece is what keeps two changes to one file from being read as one span over
// everything between them, which could then be pinned nowhere.
func TestEditsAreOneRunApiece(t *testing.T) {
	root := repo(t)
	write(t, root, "wide.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "wide")

	write(t, root, "wide.txt", "ONE\ntwo\nthree\nfour\nfive\nSIX\n")

	edits, err := Edits(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, edits, 2)

	assert.Equal(t, Edit{
		Path:  "wide.txt",
		Start: 1,
		End:   1,
		Lines: []string{"ONE"},
	}, edits[0])
	assert.Equal(t, Edit{
		Path:  "wide.txt",
		Start: 6,
		End:   6,
		Lines: []string{"SIX"},
	}, edits[1])
}

// TestEditsSpellAnInsertionAsAnEmptyRange covers lines put in without any taken out, which name the
// place they go after rather than a range, since a comment cannot stand on nothing.
func TestEditsSpellAnInsertionAsAnEmptyRange(t *testing.T) {
	root := repo(t)
	write(t, root, "wide.txt", "one\ntwo\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "wide")

	write(t, root, "wide.txt", "one\nextra\ntwo\n")

	edits, err := Edits(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, Edit{
		Path:  "wide.txt",
		Start: 2,
		End:   1,
		Lines: []string{"extra"},
	}, edits[0])
}

// TestEditsSpellAnInsertionAtTheTop covers the other edge, where there is no line above the run for
// it to be hung off.
func TestEditsSpellAnInsertionAtTheTop(t *testing.T) {
	root := repo(t)
	write(t, root, "wide.txt", "one\ntwo\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "wide")

	write(t, root, "wide.txt", "zero\none\ntwo\n")

	edits, err := Edits(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, edits, 1)

	assert.Equal(t, Edit{
		Path:  "wide.txt",
		Start: 1,
		End:   0,
		Lines: []string{"zero"},
	}, edits[0])
}

// TestEditsSaysNothingAboutATreeNobodyTouched covers the editor closing on a file nobody saved.
func TestEditsSaysNothingAboutATreeNobodyTouched(t *testing.T) {
	root := repo(t)

	edits, err := Edits(t.Context(), root)
	require.NoError(t, err)

	assert.Empty(t, edits)
}
