package transcripts

import (
	"unicode/utf8"

	"komrad/internal/fail"
	"komrad/lib/xstr"

	transcriptstore "komrad/internal/store/transcript"
)

// maxWindow is the most content one read returns.
const maxWindow = 32 * 1024

// errCursorMidRune is a cursor pointing inside a character.
var errCursorMidRune = fail.InvalidArgument(
	"cursor no longer names the start of a character, since the transcript was rewritten; read it again from the first window")

// errWindowTooSmall is a window too small to hold even one character.
var errWindowTooSmall = fail.InvalidArgument("the window is too small to hold the next character")

// Window is one cut of a transcript, with the cursor for the cut after it.
type Window struct {
	Content string
	Next    *transcriptstore.ContentCursor
}

// clampWindow is the window a request asked for, bounded above by maxWindow.
func clampWindow(requested int32) int {
	if requested > 0 && int(requested) < maxWindow {
		return int(requested)
	}

	return maxWindow
}

// cutWindow cuts the window asked for out of content, between runes, and names the cursor for
// the one after it.
func cutWindow(content string, cursor *transcriptstore.ContentCursor, requested int32) (*Window, error) {
	limit := clampWindow(requested)

	var offset int
	if cursor != nil {
		offset = int(cursor.Offset)
	}

	// An offset past the end is the empty last page of a transcript still being written.
	if offset >= len(content) {
		return &Window{}, nil
	}

	if !utf8.RuneStart(content[offset]) {
		return nil, errCursorMidRune
	}

	window, truncated := xstr.Head(content[offset:], limit)
	if !truncated {
		return &Window{Content: window}, nil
	}

	// A limit landing inside the first rune would hand back a cursor asking the same question again.
	if window == "" {
		return nil, errWindowTooSmall.Saying("max_bytes of %d is too small to hold the next character", limit)
	}

	return &Window{
		Content: window,
		Next:    &transcriptstore.ContentCursor{Offset: int64(offset + len(window))},
	}, nil
}
