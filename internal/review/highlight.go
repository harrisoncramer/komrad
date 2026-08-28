package review

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const (
	darkStyleName  = "catppuccin-mocha"
	lightStyleName = "catppuccin-latte"
)

// token is one run of source text that shares a colour, with no background, since the same
// line is drawn on a different one depending on the diff and the cursor.
type token struct {
	text string

	foreground string
}

// styledLine is one line of a file, already lexed.
type styledLine []token

// highlighter lexes whole files, which is what keeps a multi-line string or block comment
// coloured correctly where per-line lexing cannot.
type highlighter struct {
	style *chroma.Style

	lexers map[string]chroma.Lexer
}

func newHighlighter(isDark bool) *highlighter {
	name := lightStyleName
	if isDark {
		name = darkStyleName
	}

	style := styles.Get(name)
	if style == nil {
		style = styles.Fallback
	}

	return &highlighter{style: style, lexers: map[string]chroma.Lexer{}}
}

// splitLines lexes a file into one entry per line, so a diff line's number indexes straight
// into the result.
func (h *highlighter) splitLines(path, source string) []styledLine {
	if source == "" {
		return nil
	}

	// A file it cannot lex comes back nil, and the caller draws the patch's own text instead.
	iterator, err := h.getLexer(path).Tokenise(nil, source)
	if err != nil {
		return nil
	}

	lines := []styledLine{{}}
	for _, current := range iterator.Tokens() {
		foreground := ""
		if entry := h.style.Get(current.Type); entry.Colour.IsSet() {
			foreground = entry.Colour.String()
		}

		for index, part := range strings.Split(current.Value, "\n") {
			if index > 0 {
				lines = append(lines, styledLine{})
			}
			if part == "" {
				continue
			}

			lines[len(lines)-1] = append(lines[len(lines)-1], token{text: part, foreground: foreground})
		}
	}

	if last := len(lines) - 1; last >= 0 && len(lines[last]) == 0 {
		lines = lines[:last]
	}

	return lines
}

func (h *highlighter) getLexer(path string) chroma.Lexer {
	if cached, ok := h.lexers[path]; ok {
		return cached
	}

	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	h.lexers[path] = lexer

	return lexer
}

// lineAt is the lexed line at a one-based file line number, or nil when the number is outside
// the file, which happens for a source that could not be read.
func lineAt(lines []styledLine, number int) styledLine {
	if number < 1 || number > len(lines) {
		return nil
	}

	return lines[number-1]
}
