package main

import (
	"go/format"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func run(t *testing.T, src string) (string, int) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "in.go", src, parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err)

	out, count, err := rewrite(fset, file, []byte(src))
	require.NoError(t, err)
	if count == 0 {
		return src, 0
	}

	formatted, err := format.Source(out)
	require.NoError(t, err)

	return string(formatted), count
}

// TestRewriteClosesGapsBetweenFields pins the case the tool exists for, a declaration whose
// fields are spread apart by blank lines nobody meant as grouping.
func TestRewriteClosesGapsBetweenFields(t *testing.T) {
	src := `package p

type Params struct {
	AgentID string

	Author ReviewAuthor
}
`

	out, count := run(t, src)
	assert.Equal(t, 1, count)
	assert.Equal(t, `package p

type Params struct {
	AgentID string
	Author  ReviewAuthor
}
`, out)
}

// TestRewriteClosesGapsAgainstTheBraces pins that a blank line under the opening brace and
// one above the closing brace are gaps like any other.
func TestRewriteClosesGapsAgainstTheBraces(t *testing.T) {
	src := `package p

type Params struct {

	AgentID string

}
`

	out, count := run(t, src)
	assert.Equal(t, 2, count)
	assert.Equal(t, `package p

type Params struct {
	AgentID string
}
`, out)
}

// TestRewriteKeepsTheLineBeforeADocumentedField pins the exception, since running the doc
// comments together loses the break that makes them readable.
func TestRewriteKeepsTheLineBeforeADocumentedField(t *testing.T) {
	src := `package p

type Params struct {
	// AgentID is whose review it is.
	AgentID string

	// Author is who wrote the line.
	Author ReviewAuthor
}
`

	out, count := run(t, src)
	assert.Equal(t, 0, count)
	assert.Equal(t, src, out)
}

// TestRewriteLeavesAGapHoldingAComment pins that a comment standing on its own between two
// fields keeps the blank lines that set it apart.
func TestRewriteLeavesAGapHoldingAComment(t *testing.T) {
	src := `package p

type Params struct {
	AgentID string

	// everything below is the author's

	Author ReviewAuthor
}
`

	out, count := run(t, src)
	assert.Equal(t, 0, count)
	assert.Equal(t, src, out)
}

// TestRewriteClosesGapsInKeyedLiterals pins that a literal reads under the same rule as the
// declaration it fills.
func TestRewriteClosesGapsInKeyedLiterals(t *testing.T) {
	src := `package p

var x = Params{
	AgentID: "a",

	Author: "b",
}
`

	out, count := run(t, src)
	assert.Equal(t, 1, count)
	assert.Equal(t, `package p

var x = Params{
	AgentID: "a",
	Author:  "b",
}
`, out)
}

// TestRewriteLeavesMapLiteralsAlone pins that grouping inside a map is left to whoever wrote
// it, since a map is a table rather than a record.
func TestRewriteLeavesMapLiteralsAlone(t *testing.T) {
	sources := map[string]string{
		"written out": `package p

var x = map[string]int{
	"a": 1,

	"b": 2,
}
`,
		"named type": `package p

type M map[string]int

var x = M{
	"a": 1,

	"b": 2,
}
`,
		"nested": `package p

var x = map[string]map[string]int{
	"outer": {
		"a": 1,

		"b": 2,
	},
}
`,
	}

	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			out, count := run(t, src)
			assert.Equal(t, 0, count)
			assert.Equal(t, src, out)
		})
	}
}

// TestRewriteClosesGapsInAnonymousStructLiterals pins that an inline struct is read the same
// way under its brace as above it.
func TestRewriteClosesGapsInAnonymousStructLiterals(t *testing.T) {
	src := `package p

var x = struct {
	Fn func()
	N  int
}{
	Fn: nil,

	N: 2,
}
`

	out, count := run(t, src)
	assert.Equal(t, 1, count)
	assert.Equal(t, `package p

var x = struct {
	Fn func()
	N  int
}{
	Fn: nil,
	N:  2,
}
`, out)
}

// TestRewriteReadsPhysicalLinesUnderALineDirective pins that a directive renumbering the file
// does not send the rewrite off the end of the source it is editing.
func TestRewriteReadsPhysicalLinesUnderALineDirective(t *testing.T) {
	src := `package p

//line fake.go:1000
type Params struct {
	AgentID string

	Author ReviewAuthor
}
`

	out, count := run(t, src)
	assert.Equal(t, 1, count)
	assert.Equal(t, `package p

//line fake.go:1000
type Params struct {
	AgentID string
	Author  ReviewAuthor
}
`, out)
}

// TestRewriteKeepsBlankLinesOutsideStructs pins that the tool touches nothing but the fields,
// leaving the blank lines a function body is spaced with.
func TestRewriteKeepsBlankLinesOutsideStructs(t *testing.T) {
	src := `package p

func f() {
	a := 1

	_ = a
}
`

	out, count := run(t, src)
	assert.Equal(t, 0, count)
	assert.Equal(t, src, out)
}
