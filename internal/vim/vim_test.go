package vim

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buffer reads a buffer written with | where the cursor is.
func buffer(t *testing.T, s string) Buffer {
	t.Helper()

	at := strings.Index(s, "|")
	require.GreaterOrEqual(t, at, 0, "the buffer %q marks no cursor", s)

	text := s[:at] + s[at+1:]

	return Buffer{Text: text, Cursor: len([]rune(s[:at]))}
}

// written is a buffer written back with | where the cursor is.
func written(b Buffer) string {
	r := []rune(b.Text)

	return string(r[:b.Cursor]) + "|" + string(r[b.Cursor:])
}

// keys reads a key sequence in vim's own notation, such as "ciwfoo<esc>".
func keys(s string) []string {
	named := map[string]string{
		"esc": "esc", "c-[": "ctrl+[", "cr": "enter", "space": "space", "bs": "backspace",
		"c-r": "ctrl+r", "tab": "tab", "c-s": "ctrl+s", "c-e": "ctrl+e", "up": "up",
		"down": "down", "left": "left", "right": "right", "del": "delete",
	}

	var out []string
	for s != "" {
		if s[0] == '<' {
			if end := strings.IndexByte(s, '>'); end > 0 {
				if key, ok := named[s[1:end]]; ok {
					out = append(out, key)
					s = s[end+1:]

					continue
				}
			}
		}

		r := []rune(s)[0]
		out = append(out, string(r))
		s = s[len(string(r)):]
	}

	return out
}

// field stands in for the text field the engine sits in front of, typing what the engine passes on in insert mode.
type field struct {
	engine *Engine
	buf    Buffer
	single bool
	last   Outcome
}

func (f *field) press(sequence string) {
	for _, key := range keys(sequence) {
		result := f.engine.Key(key, f.buf)
		f.buf, f.last = result.Buffer, result.Outcome
		if result.Outcome == Pass && f.engine.Mode() == Insert {
			f.typeKey(key)
		}
	}
}

func (f *field) typeKey(key string) {
	r := []rune(f.buf.Text)
	p := f.buf.Cursor

	switch key {
	case "backspace":
		if p > 0 {
			f.buf = Buffer{Text: string(splice(r, p-1, p, nil)), Cursor: p - 1}
		}

		return
	case "enter":
		if !f.single {
			f.buf = Buffer{Text: string(splice(r, p, p, []rune{'\n'})), Cursor: p + 1}
		}

		return
	}

	if c, ok := keyChar(key); ok {
		f.buf = Buffer{Text: string(splice(r, p, p, []rune{c})), Cursor: p + 1}
	}
}

// inNormal is a field on start with its engine already in normal mode.
func inNormal(t *testing.T, start string, single bool) *field {
	t.Helper()

	b := buffer(t, start)
	e := New(b, single, nil)
	e.mode = Normal

	return &field{engine: e, buf: e.Normal(b), single: single}
}

type editCase struct {
	name  string
	start string
	keys  string
	want  string
	mode  Mode
}

func runEdits(t *testing.T, tests []editCase, single bool) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := inNormal(t, tt.start, single)
			f.press(tt.keys)

			assert.Equal(t, tt.want, written(f.buf))
			assert.Equal(t, tt.mode, f.engine.Mode())
		})
	}
}

// TestMotions moves the cursor and leaves the text alone.
func TestMotions(t *testing.T) {
	runEdits(t, []editCase{
		{name: "h moves left", start: "ab|c", keys: "h", want: "a|bc", mode: Normal},
		{name: "h stops at the start of the line", start: "x\n|abc", keys: "h", want: "x\n|abc", mode: Normal},
		{name: "l moves right", start: "|abc", keys: "l", want: "a|bc", mode: Normal},
		{name: "l stops on the last character", start: "ab|c", keys: "l", want: "ab|c", mode: Normal},
		{name: "the arrow keys move like hjkl", start: "a|bc", keys: "<right><left><left>", want: "|abc", mode: Normal},
		{name: "space moves right", start: "|abc", keys: "<space>", want: "a|bc", mode: Normal},
		{name: "j keeps the column", start: "ab|c\ndef", keys: "j", want: "abc\nde|f", mode: Normal},
		{name: "j holds the column inside a shorter line", start: "abc|d\nef", keys: "j", want: "abcd\ne|f", mode: Normal},
		{name: "k moves up", start: "abc\nd|ef", keys: "k", want: "a|bc\ndef", mode: Normal},
		{name: "enter moves to the first non-blank of the next line", start: "a|bc\n  def", keys: "<cr>", want: "abc\n  |def", mode: Normal},
		{name: "w moves to the next word", start: "|foo bar", keys: "w", want: "foo |bar", mode: Normal},
		{name: "w stops at punctuation", start: "|foo.bar", keys: "w", want: "foo|.bar", mode: Normal},
		{name: "W skips punctuation", start: "|foo.bar baz", keys: "W", want: "foo.bar |baz", mode: Normal},
		{name: "w crosses a line", start: "|foo\nbar", keys: "w", want: "foo\n|bar", mode: Normal},
		{name: "w at the last word stays on the last character", start: "foo |bar", keys: "w", want: "foo ba|r", mode: Normal},
		{name: "b moves to the start of the word", start: "foo ba|r", keys: "b", want: "foo |bar", mode: Normal},
		{name: "b moves to the previous word", start: "foo |bar", keys: "b", want: "|foo bar", mode: Normal},
		{name: "B skips punctuation", start: "foo.bar |baz", keys: "B", want: "|foo.bar baz", mode: Normal},
		{name: "e moves to the end of the word", start: "|foo bar", keys: "e", want: "fo|o bar", mode: Normal},
		{name: "e from the end moves to the next word's end", start: "fo|o bar", keys: "e", want: "foo ba|r", mode: Normal},
		{name: "E skips punctuation", start: "|foo.bar baz", keys: "E", want: "foo.ba|r baz", mode: Normal},
		{name: "0 moves to the start of the line", start: "  ab|c", keys: "0", want: "|  abc", mode: Normal},
		{name: "^ moves to the first non-blank", start: "  ab|c", keys: "^", want: "  |abc", mode: Normal},
		{name: "$ moves to the last character", start: "|abc", keys: "$", want: "ab|c", mode: Normal},
		{name: "gg moves to the first line", start: "abc\n d|ef", keys: "gg", want: "|abc\n def", mode: Normal},
		{name: "G moves to the first non-blank of the last line", start: "a|bc\n def", keys: "G", want: "abc\n |def", mode: Normal},
		{name: "f finds a character", start: "|a,b,c", keys: "f,", want: "a|,b,c", mode: Normal},
		{name: "t stops before the character", start: "|a,b,c", keys: "t,", want: "|a,b,c", mode: Normal},
		{name: "t from further back stops before it", start: "|ab,c", keys: "t,", want: "a|b,c", mode: Normal},
		{name: "F finds back", start: "a,b,|c", keys: "F,", want: "a,b|,c", mode: Normal},
		{name: "T stops after the character back", start: "a,b,|c", keys: "T,", want: "a,b,|c", mode: Normal},
		{name: "f with space finds a blank", start: "|ab cd", keys: "f<space>", want: "ab| cd", mode: Normal},
		{name: "f that finds nothing stays", start: "|abc", keys: "fz", want: "|abc", mode: Normal},
		{name: "f stays on its line", start: "|abc\nz", keys: "fz", want: "|abc\nz", mode: Normal},
		{name: "; repeats the find", start: "|a,b,c", keys: "f,;", want: "a,b|,c", mode: Normal},
		{name: ", repeats the find backward", start: "|a,b,c", keys: "f,;,", want: "a|,b,c", mode: Normal},
		{name: "a motion over wide runes counts runes", start: "|日本 語", keys: "w", want: "日本 |語", mode: Normal},
	}, false)
}

// TestCounts runs motions and edits the number of times typed before them.
func TestCounts(t *testing.T) {
	runEdits(t, []editCase{
		{name: "a count repeats a motion", start: "|a b c d", keys: "3w", want: "a b c |d", mode: Normal},
		{name: "a count past the line stops at its end", start: "|abc", keys: "9l", want: "ab|c", mode: Normal},
		{name: "two digits make one count", start: "|abcdefghijklmnop", keys: "12l", want: "abcdefghijkl|mnop", mode: Normal},
		{name: "0 after a digit is part of the count", start: "|abcdefghijklmnop", keys: "10l", want: "abcdefghij|klmnop", mode: Normal},
		{name: "a count finds the nth character", start: "|a,b,c", keys: "2f,", want: "a,b|,c", mode: Normal},
		{name: "a count on G goes to that line", start: "|a\nb\nc", keys: "2G", want: "a\n|b\nc", mode: Normal},
		{name: "a count on gg goes to that line", start: "a\nb\n|c", keys: "2gg", want: "a\n|b\nc", mode: Normal},
		{name: "a count on j moves that many lines", start: "|a\nb\nc", keys: "2j", want: "a\nb\n|c", mode: Normal},
		{name: "a count on x deletes that many", start: "|abcd", keys: "2x", want: "|cd", mode: Normal},
		{name: "counts on the operator and motion multiply", start: "|a b c d e f g", keys: "2d2w", want: "|e f g", mode: Normal},
		{name: "a count on dd deletes that many lines", start: "|a\nb\nc", keys: "2dd", want: "|c", mode: Normal},
		{name: "a count on p puts that many times", start: "|ab", keys: "yl3p", want: "aaa|ab", mode: Normal},
		{name: "a count on r replaces that many", start: "|abcd", keys: "3rx", want: "xx|xd", mode: Normal},
		{name: "a count on r past the line does nothing", start: "|ab", keys: "3rx", want: "|ab", mode: Normal},
		{name: "a count on ~ toggles that many", start: "|abcd", keys: "3~", want: "ABC|d", mode: Normal},
	}, false)
}

// TestOperators runs d, c and y over motions, and the edits spelled with one key.
func TestOperators(t *testing.T) {
	runEdits(t, []editCase{
		{name: "dw deletes to the next word", start: "|foo bar", keys: "dw", want: "|bar", mode: Normal},
		{name: "dw on the last word of a line keeps the newline", start: "foo |bar\nbaz", keys: "dw", want: "foo| \nbaz", mode: Normal},
		{name: "de deletes to the end of the word", start: "|foo bar", keys: "de", want: "| bar", mode: Normal},
		{name: "db deletes back to the start of the word", start: "foo |bar", keys: "db", want: "|bar", mode: Normal},
		{name: "d$ deletes to the end of the line", start: "a|bc\nd", keys: "d$", want: "|a\nd", mode: Normal},
		{name: "d0 deletes to the start of the line", start: "ab|c", keys: "d0", want: "|c", mode: Normal},
		{name: "dh deletes the character before", start: "ab|c", keys: "dh", want: "a|c", mode: Normal},
		{name: "dl on the last character deletes it", start: "ab|c", keys: "dl", want: "a|b", mode: Normal},
		{name: "dj deletes this line and the next", start: "|a\nb\nc", keys: "dj", want: "|c", mode: Normal},
		{name: "dk deletes this line and the one above", start: "a\nb\n|c", keys: "dk", want: "|a", mode: Normal},
		{name: "dj on the last line does nothing", start: "a\n|b", keys: "dj", want: "a\n|b", mode: Normal},
		{name: "dG deletes to the last line", start: "a\n|b\nc", keys: "dG", want: "|a", mode: Normal},
		{name: "dgg deletes to the first line", start: "a\n|b\nc", keys: "dgg", want: "|c", mode: Normal},
		{name: "df deletes through the character", start: "|a,b", keys: "df,", want: "|b", mode: Normal},
		{name: "dt deletes up to the character", start: "|ab,c", keys: "dt,", want: "|,c", mode: Normal},
		{name: "dF deletes back to the character", start: "a,b|c", keys: "dF,", want: "a|c", mode: Normal},
		{name: "dd deletes the line", start: "a\n|b\nc", keys: "dd", want: "a\n|c", mode: Normal},
		{name: "dd on the last line deletes it and its newline", start: "a\n|b", keys: "dd", want: "|a", mode: Normal},
		{name: "dd on the only line empties it", start: "|abc", keys: "dd", want: "|", mode: Normal},
		{name: "dd lands on the first non-blank", start: "|a\n  b", keys: "dd", want: "  |b", mode: Normal},
		{name: "cw changes to the end of the word", start: "|foo bar", keys: "cwbaz<esc>", want: "ba|z bar", mode: Normal},
		{name: "cw on a one-letter word changes only it", start: "|a bar", keys: "cwxy<esc>", want: "x|y bar", mode: Normal},
		{name: "cw on blanks changes the blanks", start: "foo|  bar", keys: "cw-<esc>", want: "foo|-bar", mode: Normal},
		{name: "c$ changes to the end of the line", start: "a|bc", keys: "c$X<esc>", want: "a|X", mode: Normal},
		{name: "cc changes the whole line", start: "a\n|bcd\ne", keys: "ccX<esc>", want: "a\n|X\ne", mode: Normal},
		{name: "a change stays in insert until esc", start: "|foo", keys: "cw", want: "|", mode: Insert},
		{name: "yw then P puts the word before the cursor", start: "|foo bar", keys: "ywP", want: "foo| foo bar", mode: Normal},
		{name: "yy then p puts the line below", start: "|ab\ncd", keys: "yyp", want: "ab\n|ab\ncd", mode: Normal},
		{name: "yy then P puts the line above", start: "ab\n|cd", keys: "yyP", want: "ab\n|cd\ncd", mode: Normal},
		{name: "yy then p on the last line puts a line after it", start: "ab\n|cd", keys: "yyp", want: "ab\ncd\n|cd", mode: Normal},
		{name: "y leaves the text and moves to the start", start: "foo |bar", keys: "yb", want: "|foo bar", mode: Normal},
		{name: "dd then p moves a line down", start: "|a\nb", keys: "ddp", want: "b\n|a", mode: Normal},
		{name: "x deletes the character", start: "a|bc", keys: "x", want: "a|c", mode: Normal},
		{name: "x on the last character moves back", start: "ab|c", keys: "x", want: "a|b", mode: Normal},
		{name: "x on an empty line does nothing", start: "a\n|\nb", keys: "x", want: "a\n|\nb", mode: Normal},
		{name: "the delete key deletes like x", start: "a|bc", keys: "<del>", want: "a|c", mode: Normal},
		{name: "X deletes the character before", start: "ab|c", keys: "X", want: "a|c", mode: Normal},
		{name: "s substitutes the character", start: "a|bc", keys: "sX<esc>", want: "a|Xc", mode: Normal},
		{name: "S substitutes the line", start: "|abc", keys: "SX<esc>", want: "|X", mode: Normal},
		{name: "D deletes to the end of the line", start: "a|bc", keys: "D", want: "|a", mode: Normal},
		{name: "C changes to the end of the line", start: "a|bc", keys: "CX<esc>", want: "a|X", mode: Normal},
		{name: "Y then p puts the line below", start: "|ab", keys: "Yp", want: "ab\n|ab", mode: Normal},
		{name: "r replaces the character", start: "a|bc", keys: "rX", want: "a|Xc", mode: Normal},
		{name: "~ toggles case and moves on", start: "|aBc", keys: "~~", want: "Ab|c", mode: Normal},
		{name: "p puts a character after the cursor", start: "|ab", keys: "xp", want: "b|a", mode: Normal},
		{name: "p with nothing yanked does nothing", start: "|ab", keys: "p", want: "|ab", mode: Normal},
		{name: "J joins the next line with a space", start: "|ab\n  cd", keys: "J", want: "ab| cd", mode: Normal},
		{name: "J onto an empty line adds no space", start: "|ab\n\ncd", keys: "J", want: "a|b\ncd", mode: Normal},
		{name: "J after a trailing blank adds no second space", start: "|ab \ncd", keys: "J", want: "ab |cd", mode: Normal},
		{name: "J on the last line does nothing", start: "|ab", keys: "J", want: "|ab", mode: Normal},
		{name: "i inserts before the cursor", start: "a|c", keys: "ib<esc>", want: "a|bc", mode: Normal},
		{name: "a inserts after the cursor", start: "|ac", keys: "ab<esc>", want: "a|bc", mode: Normal},
		{name: "I inserts at the first non-blank", start: "  ab|c", keys: "Ix<esc>", want: "  |xabc", mode: Normal},
		{name: "A appends to the line", start: "|ab\ncd", keys: "Ax<esc>", want: "ab|x\ncd", mode: Normal},
		{name: "o opens a line below", start: "|ab\ncd", keys: "ox<esc>", want: "ab\n|x\ncd", mode: Normal},
		{name: "O opens a line above", start: "ab\n|cd", keys: "Ox<esc>", want: "ab\n|x\ncd", mode: Normal},
		{name: "enter in insert types a new line", start: "a|b", keys: "i<cr><esc>", want: "a\n|b", mode: Normal},
	}, false)
}

// TestTextObjects changes and deletes the text objects around the cursor.
func TestTextObjects(t *testing.T) {
	runEdits(t, []editCase{
		{name: "diw deletes the word", start: "foo b|ar baz", keys: "diw", want: "foo | baz", mode: Normal},
		{name: "daw deletes the word and the blank after it", start: "foo b|ar baz", keys: "daw", want: "foo |baz", mode: Normal},
		{name: "daw on the last word takes the blank before it", start: "foo b|ar", keys: "daw", want: "fo|o", mode: Normal},
		{name: "diw on blanks deletes the blanks", start: "foo | bar", keys: "diw", want: "foo|bar", mode: Normal},
		{name: "ciw changes the word", start: "foo b|ar baz", keys: "ciwqux<esc>", want: "foo qu|x baz", mode: Normal},
		{name: "diW takes punctuation with the word", start: "a fo|o.bar b", keys: "diW", want: "a | b", mode: Normal},
		{name: "di\" deletes inside the quotes", start: `say "he|llo" now`, keys: `di"`, want: `say "|" now`, mode: Normal},
		{name: "da\" deletes the quotes and the blank after", start: `say "he|llo" now`, keys: `da"`, want: `say |now`, mode: Normal},
		{name: "di\" before the quotes finds the next pair", start: `|say "hello"`, keys: `di"`, want: `say "|"`, mode: Normal},
		{name: "di' deletes inside single quotes", start: "a 'b|c' d", keys: "di'", want: "a '|' d", mode: Normal},
		{name: "di` deletes inside backticks", start: "a `b|c` d", keys: "di`", want: "a `|` d", mode: Normal},
		{name: "an escaped quote is not a quote", start: `"a\"|b"`, keys: `di"`, want: `"|"`, mode: Normal},
		{name: "di\" with no quotes does nothing", start: "ab|c", keys: `di"`, want: "ab|c", mode: Normal},
		{name: "di( deletes inside the brackets", start: "f(a, |b)", keys: "di(", want: "f(|)", mode: Normal},
		{name: "di) is di(", start: "f(a, |b)", keys: "di)", want: "f(|)", mode: Normal},
		{name: "dib is di(", start: "f(a, |b)", keys: "dib", want: "f(|)", mode: Normal},
		{name: "da( deletes the brackets too", start: "f(a, |b) x", keys: "da(", want: "f| x", mode: Normal},
		{name: "di( on the opening bracket", start: "f|(ab)", keys: "di(", want: "f(|)", mode: Normal},
		{name: "di( on the closing bracket", start: "f(ab|)", keys: "di(", want: "f(|)", mode: Normal},
		{name: "di( skips a nested pair", start: "f(a, (b), |c)", keys: "di(", want: "f(|)", mode: Normal},
		{name: "di( inside a nested pair takes the inner one", start: "f(a, (|b), c)", keys: "di(", want: "f(a, (|), c)", mode: Normal},
		{name: "di( across lines", start: "f(\n  |a\n)", keys: "di(", want: "f(|)", mode: Normal},
		{name: "ci( on an empty pair inserts between", start: "f|()", keys: "ci(x<esc>", want: "f(|x)", mode: Normal},
		{name: "di[ deletes inside square brackets", start: "[a|b]", keys: "di[", want: "[|]", mode: Normal},
		{name: "di{ deletes inside braces", start: "{a|b}", keys: "di{", want: "{|}", mode: Normal},
		{name: "diB is di{", start: "{a|b}", keys: "diB", want: "{|}", mode: Normal},
		{name: "di( with no brackets does nothing", start: "ab|c", keys: "di(", want: "ab|c", mode: Normal},
		{name: "an unknown text object is dropped", start: "ab|c", keys: "diqx", want: "a|b", mode: Normal},
	}, false)
}

// TestUndo steps back and forward through changes, including ones made outside the engine.
func TestUndo(t *testing.T) {
	runEdits(t, []editCase{
		{name: "u undoes a delete", start: "a|bc", keys: "xu", want: "a|bc", mode: Normal},
		{name: "u undoes an insert as one step", start: "|c", keys: "iab<esc>u", want: "|c", mode: Normal},
		{name: "u undoes a change and its insert as one step", start: "|foo bar", keys: "cwbaz<esc>u", want: "|foo bar", mode: Normal},
		{name: "u with a count undoes that many", start: "|abc", keys: "xxx2u", want: "|bc", mode: Normal},
		{name: "ctrl+r redoes", start: "a|bc", keys: "xu<c-r>", want: "a|c", mode: Normal},
		{name: "a new change drops the redo", start: "|abc", keys: "xux<c-r>", want: "|bc", mode: Normal},
		{name: "u with nothing to undo does nothing", start: "a|bc", keys: "u", want: "a|bc", mode: Normal},
		{name: "an insert that typed nothing is no step", start: "a|bc", keys: "xi<esc>u", want: "a|bc", mode: Normal},
		{name: "a motion is no step", start: "a|bc", keys: "xlu", want: "a|bc", mode: Normal},
	}, false)

	t.Run("a change made outside the engine is a step of its own", func(t *testing.T) {
		f := inNormal(t, "a|bc", false)
		f.press("x")
		f.buf = Buffer{Text: "written by the editor", Cursor: 0}
		f.press("u")

		assert.Equal(t, "a|c", written(f.buf))

		f.press("u")

		assert.Equal(t, "a|bc", written(f.buf))
	})
}

// TestRepeat runs the last change again with dot.
func TestRepeat(t *testing.T) {
	runEdits(t, []editCase{
		{name: ". repeats x", start: "|abcd", keys: "x.", want: "|cd", mode: Normal},
		{name: ". repeats dw", start: "|a b c", keys: "dw.", want: "|c", mode: Normal},
		{name: ". repeats ciw with what was typed", start: "|foo bar", keys: "ciwX<esc>w.", want: "X |X", mode: Normal},
		{name: ". repeats an append", start: "|a\nb", keys: "A!<esc>j.", want: "a!\nb|!", mode: Normal},
		{name: ". repeats an insert", start: "|ab", keys: "i-<esc>.", want: "|--ab", mode: Normal},
		{name: ". repeats o", start: "|a", keys: "ox<esc>.", want: "a\nx\n|x", mode: Normal},
		{name: ". with a count replaces the count", start: "|abcdef", keys: "x3.", want: "|ef", mode: Normal},
		{name: ". after an insert that backspaced repeats what was left", start: "|", keys: "iab<bs>c<esc>.", want: "aa|cc", mode: Normal},
		{name: ". is undone by u in one step", start: "|abc", keys: "x.u", want: "|bc", mode: Normal},
		{name: ". with nothing to repeat does nothing", start: "a|bc", keys: ".", want: "a|bc", mode: Normal},
		{name: "a yank is not repeated", start: "|abc", keys: "xyl.", want: "|c", mode: Normal},
		{name: "a motion is not repeated", start: "|abc", keys: "xl.", want: "|b", mode: Normal},
	}, false)
}

// TestModes covers how the engine moves between insert and normal and what it hands back.
func TestModes(t *testing.T) {
	t.Run("a new engine starts in insert", func(t *testing.T) {
		e := New(Buffer{}, true, nil)

		assert.Equal(t, Insert, e.Mode())
	})

	t.Run("insert passes typed keys to the field", func(t *testing.T) {
		e := New(Buffer{Text: "ab", Cursor: 2}, true, nil)

		result := e.Key("j", Buffer{Text: "ab", Cursor: 2})

		assert.Equal(t, Pass, result.Outcome)
		assert.Equal(t, Insert, e.Mode())
	})

	tests := []struct {
		name    string
		start   string
		single  bool
		keys    string
		want    string
		outcome Outcome
		mode    Mode
	}{
		{name: "esc leaves insert and steps back", start: "a|b", keys: "a<esc>", want: "a|b", outcome: Consumed, mode: Normal},
		{name: "esc at the start of a line stays", start: "|ab", keys: "i<esc>", want: "|ab", outcome: Consumed, mode: Normal},
		{name: "ctrl+[ is esc", start: "a|b", keys: "a<c-[>", want: "a|b", outcome: Consumed, mode: Normal},
		{name: "esc in normal leaves", start: "a|b", keys: "<esc>", want: "a|b", outcome: Leave, mode: Normal},
		{name: "ctrl+[ in normal leaves", start: "a|b", keys: "<c-[>", want: "a|b", outcome: Leave, mode: Normal},
		{name: "esc drops a pending command without leaving", start: "a|b", keys: "d<esc>", want: "a|b", outcome: Consumed, mode: Normal},
		{name: "esc drops a pending count", start: "|abcd", keys: "3<esc>x", want: "|bcd", outcome: Consumed, mode: Normal},
		{name: "j on a one-line field is an edge", start: "a|b", single: true, keys: "j", want: "a|b", outcome: EdgeDown, mode: Normal},
		{name: "k on a one-line field is an edge", start: "a|b", single: true, keys: "k", want: "a|b", outcome: EdgeUp, mode: Normal},
		{name: "the down arrow is an edge too", start: "a|b", single: true, keys: "<down>", want: "a|b", outcome: EdgeDown, mode: Normal},
		{name: "j on the last line is an edge", start: "a\n|b", keys: "j", want: "a\n|b", outcome: EdgeDown, mode: Normal},
		{name: "k on the first line is an edge", start: "|a\nb", keys: "k", want: "|a\nb", outcome: EdgeUp, mode: Normal},
		{name: "j with lines below moves", start: "|a\nb", keys: "j", want: "a\n|b", outcome: Consumed, mode: Normal},
		{name: "tab passes to the field", start: "a|b", keys: "<tab>", want: "a|b", outcome: Pass, mode: Normal},
		{name: "the submit key passes to the field", start: "a|b", keys: "<c-s>", want: "a|b", outcome: Pass, mode: Normal},
		{name: "the editor key passes to the field", start: "a|b", keys: "<c-e>", want: "a|b", outcome: Pass, mode: Normal},
		{name: "a pass drops a pending command", start: "a|b", keys: "d<tab>x", want: "|a", outcome: Consumed, mode: Normal},
		{name: "enter passes on a one-line field", start: "a|b", single: true, keys: "<cr>", want: "a|b", outcome: Pass, mode: Normal},
		{name: "enter on the last line of a multi-line field does nothing", start: "a\n|b", keys: "<cr>", want: "a\n|b", outcome: Consumed, mode: Normal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := inNormal(t, tt.start, tt.single)
			f.press(tt.keys)

			assert.Equal(t, tt.want, written(f.buf))
			assert.Equal(t, tt.outcome, f.last)
			assert.Equal(t, tt.mode, f.engine.Mode())
		})
	}

	t.Run("Normal from insert settles like esc", func(t *testing.T) {
		e := New(Buffer{Text: "ab", Cursor: 2}, true, nil)

		b := e.Normal(Buffer{Text: "abc", Cursor: 3})

		assert.Equal(t, "ab|c", written(b))
		assert.Equal(t, Normal, e.Mode())
	})

	t.Run("Insert from normal keeps the cursor where it is", func(t *testing.T) {
		f := inNormal(t, "a|bc", true)
		f.engine.Insert(f.buf)
		f.press("x<esc>")

		assert.Equal(t, "a|xbc", written(f.buf))
	})

	t.Run("the register is shared between engines", func(t *testing.T) {
		register := &Register{}
		from := New(Buffer{Text: "word"}, true, register)
		to := New(Buffer{Text: "x"}, true, register)

		source := &field{engine: from, buf: from.Normal(Buffer{Text: "word"}), single: true}
		source.press("yiw")

		target := &field{engine: to, buf: to.Normal(Buffer{Text: "x"}), single: true}
		target.press("p")

		assert.Equal(t, "xwor|d", written(target.buf))
	})
}

// TestSwallowed holds that normal mode never types into the field.
func TestSwallowed(t *testing.T) {
	runEdits(t, []editCase{
		{name: "a key normal mode does not own types nothing", start: "a|b", keys: "vqmZ/", want: "a|b", mode: Normal},
		{name: "a g that is not gg is dropped", start: "a|b", keys: "gzx", want: "|a", mode: Normal},
		{name: "enter on a multi-line field moves rather than types", start: "|a\nb", keys: "<cr>", want: "a\n|b", mode: Normal},
		{name: "backspace moves left rather than deletes", start: "a|b", keys: "<bs>", want: "|ab", mode: Normal},
	}, false)

	t.Run("an out-of-scope key reports it was taken", func(t *testing.T) {
		f := inNormal(t, "a|b", false)
		f.press("v")

		assert.Equal(t, Consumed, f.last)
	})
}

// TestSingleLine holds a one-line field to one line.
func TestSingleLine(t *testing.T) {
	runEdits(t, []editCase{
		{name: "o does nothing", start: "a|b", keys: "o", want: "a|b", mode: Normal},
		{name: "O does nothing", start: "a|b", keys: "O", want: "a|b", mode: Normal},
		{name: "J does nothing", start: "a|b", keys: "J", want: "a|b", mode: Normal},
		{name: "dd empties the field", start: "a|b", keys: "dd", want: "|", mode: Normal},
		{name: "a yanked line is put inline", start: "|ab", keys: "yyp", want: "aa|bb", mode: Normal},
		{name: "gg and G stay on the line", start: "a|bc", keys: "G", want: "|abc", mode: Normal},
		{name: "cc changes the line", start: "a|b", keys: "ccx<esc>", want: "|x", mode: Normal},
	}, true)

	t.Run("a put of text holding a newline is joined with spaces", func(t *testing.T) {
		register := &Register{text: "a\nb"}
		e := New(Buffer{Text: "x"}, true, register)
		f := &field{engine: e, buf: e.Normal(Buffer{Text: "x"}), single: true}
		f.press("p")

		assert.Equal(t, "xa |b", written(f.buf))
	})
}

// TestTyped measures the text an insert added.
func TestTyped(t *testing.T) {
	tests := []struct {
		from, to, want string
	}{
		{from: "ab", to: "axb", want: "x"},
		{from: "", to: "abc", want: "abc"},
		{from: "aa", to: "aaa", want: "a"},
		{from: "abc", to: "abc", want: ""},
		{from: "abc", to: "ac", want: ""},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, typed(tt.from, tt.to), "%q to %q", tt.from, tt.to)
	}
}
