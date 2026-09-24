package scripts

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

const (
	directiveWord    = "KOMRADE"
	directiveTimeout = "timeout"
	directiveDesc    = "description"
)

var commentLeaders = []string{"#", "//"}

const headerLimit = 8 << 10

const noTimeoutWord = "none"

const unlimitedTimeout time.Duration = -1

type Header struct {
	Description string
	Timeout     time.Duration
	Problems    []string
}

// TimeoutOr is the header's declared timeout, the fallback where the file declared none, or
// 0 where the file declared no limit at all.
func (h Header) TimeoutOr(fallback time.Duration) time.Duration {
	switch {
	case h.Timeout < 0:
		return 0
	case h.Timeout > 0:
		return h.Timeout
	default:
		return fallback
	}
}

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

func isText(head []byte) bool {
	return bytes.IndexByte(head, 0) < 0
}

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

func commentTextOf(line string) (string, bool) {
	for _, leader := range commentLeaders {
		if text, found := strings.CutPrefix(line, leader); found {
			return text, true
		}
	}

	return "", false
}

func directiveTextOf(comment string) (string, bool) {
	rest, found := strings.CutPrefix(strings.TrimLeft(comment, " \t"), directiveWord)
	if !found {
		return "", false
	}

	text := strings.TrimLeft(rest, " \t")
	if text == rest {
		return "", false
	}

	return text, true
}

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
			h.note(line, `timeout wants a quoted duration such as "90s" or "10m", or "none"`)
		default:
			h.note(line, fmt.Sprintf("%s is not a directive komrade knows", key))
		}
	}
}

func (h *Header) applyTimeout(text string, line int) {
	if strings.EqualFold(text, noTimeoutWord) {
		h.Timeout = unlimitedTimeout

		return
	}

	timeout, err := time.ParseDuration(text)
	if err != nil {
		h.note(line, fmt.Sprintf("%q is not a duration: %v", text, err))

		return
	}

	if timeout <= 0 {
		h.note(line, fmt.Sprintf("%q is not a duration a script can run within", text))

		return
	}

	h.Timeout = timeout
}

func (h *Header) note(line int, said string) {
	h.Problems = append(h.Problems, fmt.Sprintf("line %d: %s", line, said))
}
