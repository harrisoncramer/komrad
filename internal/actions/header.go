package actions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// The directive an action declares itself with, borrowed from the file tasks in mise, which
// this repository's own mise tasks are written as. A line is a directive when it is a
// comment whose text begins with this word, and what follows is one TOML key and value.
//
// TOML is what parses the value rather than a split on the first equals sign, so a
// description carrying quotes, escapes, or a run of spaces means what TOML says it means
// instead of whatever a hand-rolled reader happened to do with it.
const (
	directiveWord    = "WEAVE"
	directiveTimeout = "timeout"
	directiveDesc    = "description"
)

// commentLeaders are what starts a comment in the languages an action is written in. An
// action is an executable rather than a shell script, so a directive is readable from the
// Python or JavaScript one somebody writes as easily as from the usual sh.
var commentLeaders = []string{"#", "//"}

// headerLimit bounds how much of a file is read looking for directives, since an action is
// as readily a compiled program of any size as a script and the header is a handful of
// lines either way.
const headerLimit = 8 << 10

// Header is what an action declares about itself in the comments at the top of its own
// file. Every field is optional, and the zero value is what a file with no header at all,
// and what one weave could not read, both come back as.
type Header struct {
	// Description is free text for a person choosing between actions.
	Description string
	// Timeout bounds one run of the action. Zero means the action declared none, which is
	// not the same as declaring no limit: what an undeclared timeout falls back to is the
	// caller's to decide.
	Timeout time.Duration
	// Problems are the directives that were not understood, each naming its line. They are
	// carried rather than returned as an error because a header weave cannot read is not a
	// reason to withhold the action: the file is still there, still executable, and still
	// does whatever it does when run by hand. Discover lists a file missing its chmod for
	// the same reason, and a typo in a comment is a smaller thing than that.
	Problems []string
}

// readHeader is what the file at path declares about itself. Only the head of the file is
// read, and a file that cannot be opened or read comes back as no header rather than an
// error, since Discover offers it either way and the caller has nothing to do about it.
func readHeader(path string) Header {
	file, err := os.Open(path)
	if err != nil {
		return Header{}
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, headerLimit)
	read, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Header{}
	}

	return scanHeader(head[:read])
}

// scanHeader reads the directives out of the top of a file.
//
// The scan runs from the first line to the first line of code, so blank lines, a shebang,
// and ordinary comments are all passed over and a directive among them still counts. Code
// ends it, which is where this parts company with mise: mise takes a directive from
// anywhere in the file, and here a line that looks like one further down is a comment
// about the code around it rather than something the file is saying about itself.
//
// A file carrying a NUL byte is a compiled program and is not scanned at all, deciding
// what is text the way less does. Directives are read off the trimmed line, so an indented
// one is still read, and the last of two directives naming the same key is the one that
// stands.
func scanHeader(text []byte) Header {
	var header Header
	if bytes.IndexByte(text, 0) >= 0 {
		return header
	}

	for number, line := range strings.Split(string(text), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		comment, isComment := commentTextOf(trimmed)
		if !isComment {
			break
		}

		directive, isDirective := directiveTextOf(comment)
		if !isDirective {
			continue
		}

		header.apply(directive, number+1)
	}

	return header
}

// commentTextOf is what a comment line says, less the marker that started it, and whether
// the line was a comment at all.
func commentTextOf(line string) (string, bool) {
	for _, leader := range commentLeaders {
		if text, found := strings.CutPrefix(line, leader); found {
			return text, true
		}
	}

	return "", false
}

// directiveTextOf is the key and value a directive carries, and whether the comment was
// one. The word is matched case sensitively, so a comment that happens to open with the
// project's name in prose is not read as a declaration.
func directiveTextOf(comment string) (string, bool) {
	rest, found := strings.CutPrefix(strings.TrimLeft(comment, " \t"), directiveWord)
	if !found {
		return "", false
	}

	// The word has to end the token rather than merely start it, or WEAVEFOO reads as a
	// directive named FOO.
	text := strings.TrimLeft(rest, " \t")
	if text == rest {
		return "", false
	}

	return text, true
}

// apply reads one directive into the header, recording a problem instead where it cannot.
// An unknown key is a problem of its own, since a misspelled directive is otherwise
// indistinguishable from one weave silently ignored.
func (h *Header) apply(directive string, line int) {
	var fields map[string]any
	if err := toml.Unmarshal([]byte(directive), &fields); err != nil {
		h.note(line, fmt.Sprintf("%q is not one TOML key and value", directive))

		return
	}

	for key, value := range fields {
		text, isText := value.(string)

		switch {
		case key == directiveDesc && isText:
			h.Description = text
		case key == directiveDesc:
			h.note(line, "description wants a quoted string")
		case key == directiveTimeout && isText:
			h.applyTimeout(text, line)
		case key == directiveTimeout:
			h.note(line, `timeout wants a quoted duration, such as "90s" or "10m"`)
		default:
			h.note(line, fmt.Sprintf("%s is not a directive weave knows", key))
		}
	}
}

// applyTimeout reads a declared duration. A zero or negative one is refused rather than
// taken as no limit, since an action that may not run at all is not what anybody was
// asking for by writing it down.
func (h *Header) applyTimeout(text string, line int) {
	timeout, err := time.ParseDuration(text)
	if err != nil {
		h.note(line, fmt.Sprintf("%q is not a duration: %v", text, err))

		return
	}

	if timeout <= 0 {
		h.note(line, fmt.Sprintf("%q is not a duration an action can run within", text))

		return
	}

	h.Timeout = timeout
}

// note records what could not be read, naming the line so it can be found.
func (h *Header) note(line int, said string) {
	h.Problems = append(h.Problems, fmt.Sprintf("line %d: %s", line, said))
}
