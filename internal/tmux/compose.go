package tmux

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"unicode"
)

// pasteBufferPrefix opens the name of every buffer a paste is loaded through.
const pasteBufferPrefix = "weave-paste"

// pastes counts the pastes this process has sent, which a buffer name carries so two pastes at
// once cannot overwrite each other's text.
var pastes atomic.Int64

// PasteAndSubmit inserts text into a pane as a bracketed paste and sends Enter in a single
// invocation.
func (c *Client) PasteAndSubmit(target, text string) error {
	return c.paste(target, text, ";", "send-keys", "-t", target, "Enter")
}

// CutLine takes the line a pane's cursor sits on into the kill ring of the program reading it.
func (c *Client) CutLine(target string) error {
	if target == "" {
		return errors.New("refusing to cut a line at an empty target")
	}

	// A kill takes only what is behind the cursor.
	if _, err := c.query("send-keys", "-t", target, "C-e", ";", "send-keys", "-t", target, "C-u"); err != nil {
		return fmt.Errorf("failed to cut the line at %q: %w", target, err)
	}

	return nil
}

// YankLine puts back the newest line in the kill ring of the program reading the pane.
func (c *Client) YankLine(target string) error {
	if target == "" {
		return errors.New("refusing to yank at an empty target")
	}

	if _, err := c.query("send-keys", "-t", target, "C-y"); err != nil {
		return fmt.Errorf("failed to yank at %q: %w", target, err)
	}

	return nil
}

// paste loads text into a buffer and pastes it, running whatever commands follow in the
// same invocation.
func (c *Client) paste(target, text string, then ...string) error {
	if target == "" {
		return errors.New("refusing to paste into an empty target")
	}
	if text == "" {
		return errors.New("refusing to paste nothing")
	}

	text = stripControls(text)
	if text == "" {
		return errors.New("refusing to paste nothing but control characters")
	}

	if _, err := c.query(pasteArgs(pasteBufferName(), target, text, then...)...); err != nil {
		return fmt.Errorf("failed to paste into %q: %w", target, err)
	}

	return nil
}

func stripControls(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}

		return -1
	}, text)
}

// pasteBufferName is a buffer name no other paste on this server holds.
func pasteBufferName() string {
	return fmt.Sprintf("%s-%d-%d", pasteBufferPrefix, os.Getpid(), pastes.Add(1))
}

// pasteArgs is the one invocation a paste is sent as.
func pasteArgs(buffer, target, text string, then ...string) []string {
	args := []string{
		"set-buffer", "-b", buffer, "--", text,
		";", "paste-buffer", "-d", "-p", "-b", buffer, "-t", target,
	}

	return append(args, then...)
}
