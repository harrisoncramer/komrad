package glyph_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"komrad/internal/glyph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scanExempt is the source allowed to spell a glyph itself: this package, which is where they
// are spelled, and the harness frame readers, which match what other programs draw.
var scanExempt = []string{"internal/glyph", "internal/harness"}

// TestNoGlyphIsSpelledOutsideThisPackage covers the one place a glyph is written, so a new icon
// cannot be typed where it is drawn and escape the config.
func TestNoGlyphIsSpelledOutsideThisPackage(t *testing.T) {
	root := findModuleRoot(t)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		if entry.IsDir() {
			if isSkippedDir(relative, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(relative, ".go") || strings.HasSuffix(relative, "_test.go") {
			return nil
		}

		for _, found := range scanFile(t, path) {
			t.Errorf("%s:%d spells U+%04X %q, which belongs in internal/glyph", relative, found.line, found.r, string(found.r))
		}

		return nil
	})
	require.NoError(t, err)
}

// TestScanReadsEscapes covers a glyph written as an escape, which the scan unquotes before it
// looks.
func TestScanReadsEscapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "escaped.go")
	source := "package escaped\n\nconst a = \"\\uf4dd\"\nconst b = '·'\nconst c = \"…!\"\n"
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))

	found := scanFile(t, path)
	require.Len(t, found, 2)
	assert.Equal(t, rune(0xf4dd), found[0].r)
	assert.Equal(t, '·', found[1].r)
}

// spelled is a glyph a literal spells and the line it is on.
type spelled struct {
	line int
	r    rune
}

// scanFile is every approved or private-use character the file's literals spell.
func scanFile(t *testing.T, path string) []spelled {
	t.Helper()

	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "%s does not parse", path)

	var found []spelled
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || (literal.Kind != token.STRING && literal.Kind != token.CHAR) {
			return true
		}

		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}

		for _, r := range value {
			if isForbidden(r) {
				found = append(found, spelled{line: files.Position(literal.Pos()).Line, r: r})
			}
		}

		return true
	})

	return found
}

// isForbidden reports whether a character may only come from this package, which is every
// approved one save ordinary punctuation and anything in a Private Use Area.
func isForbidden(r rune) bool {
	if r == '!' {
		return false
	}

	return glyph.IsApproved(r) || isPrivateUse(r)
}

// isPrivateUse reports whether a character sits in one of Unicode's Private Use Areas, where
// every icon font puts its icons.
func isPrivateUse(r rune) bool {
	return (r >= 0xe000 && r <= 0xf8ff) || (r >= 0xf0000 && r <= 0xffffd) || (r >= 0x100000 && r <= 0x10fffd)
}

// isSkippedDir reports whether the scan leaves a directory out.
func isSkippedDir(relative, name string) bool {
	if name == ".git" || name == "node_modules" {
		return true
	}

	for _, exempt := range scanExempt {
		if relative == exempt {
			return true
		}
	}

	return false
}

// findModuleRoot is the directory holding go.mod, above this package.
func findModuleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above the test")
		dir = parent
	}
}
