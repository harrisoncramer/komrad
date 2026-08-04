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

// The directive an action declares itself with, taken from the file tasks in mise. A
// comment whose text begins with this word carries one TOML key and value, and TOML rather
// than a split on the equals sign is what makes quotes and escapes in a description mean
// something definite.
const (
	directiveWord    = "WEAVE"
	directiveTimeout = "timeout"
	directiveDesc    = "description"
)

// commentLeaders are what starts a comment in the languages an action is written in, since
// an action is any executable and not only the usual sh.
var commentLeaders = []string{"#", "//"}

// headerLimit bounds how much of a file is read looking for directives, since an action can
// be a compiled program of any size.
const headerLimit = 8 << 10

// Header is what an action declares about itself in the comments at the top of its own
// file. Every field is optional, and a file that declares nothing reads as the zero value.
type Header struct {
	// Description is free text for a person choosing between actions.
	Description string
	// Timeout bounds one run of the action. Zero means the file declared none, and what that
	// falls back to is the caller's to decide.
	Timeout time.Duration
	// Problems are the directives that were not understood, each naming its line. A header
	// weave cannot read is no reason to withhold an action that still runs, so these are
	// carried for a caller to show rather than returned as an error.
	Problems []string
}

// readHeader is what the file at path declares about itself, read out of the head of the
// file, and whether that head is text at all. One that cannot be read comes back as no
// header and not text, since Discover offers it either way.
func readHeader(path string) (Header, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Header{}, false
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, headerLimit)
	read, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Header{}, false
	}

	return scanHeader(head[:read]), isText(head[:read])
}

// isText reports whether the head of a file is text, deciding on a NUL byte the way less
// itself does.
func isText(head []byte) bool {
	return bytes.IndexByte(head, 0) < 0
}

// scanHeader reads the directives out of the top of a file, passing over blank lines, a
// shebang, and ordinary comments. The first line of code ends the scan, which is where this
// parts company with mise. mise takes a directive from anywhere in a file, and a line
// further down here is a comment about the code around it.
//
// A file carrying a NUL byte is compiled and is not scanned, deciding what is text the way
// less does. The last of two directives naming the same key is the one that stands.
func scanHeader(text []byte) Header {
	var header Header
	if !isText(text) {
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

// commentTextOf is what a comment line says without the marker that started it, and whether
// the line was a comment at all.
func commentTextOf(line string) (string, bool) {
	for _, leader := range commentLeaders {
		if text, found := strings.CutPrefix(line, leader); found {
			return text, true
		}
	}

	return "", false
}

// directiveTextOf is the key and value a directive carries, and whether the comment was one.
// The word is matched case sensitively, so a comment opening with the project's name in prose
// is not a declaration.
func directiveTextOf(comment string) (string, bool) {
	rest, found := strings.CutPrefix(strings.TrimLeft(comment, " \t"), directiveWord)
	if !found {
		return "", false
	}

	// The word has to end the token, or WEAVEFOO reads as a directive named FOO.
	text := strings.TrimLeft(rest, " \t")
	if text == rest {
		return "", false
	}

	return text, true
}

// apply reads one directive into the header, recording a problem where it cannot. An unknown
// key is a problem of its own, since a misspelled directive is otherwise indistinguishable
// from one weave ignored.
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

// applyTimeout reads a declared duration. Zero or negative is refused rather than taken as
// no limit, since nobody writing one down meant an action that cannot run at all.
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
