package tmux

import (
	"errors"
	"fmt"
)

// pasteBuffer is the buffer a paste is loaded through.
const pasteBuffer = "weave-paste"

// Paste inserts text into a pane as a bracketed paste.
func (c *Client) Paste(target, text string) error {
	return c.paste(target, text)
}

// PasteAndSubmit inserts text as a bracketed paste and sends Enter in the same invocation.
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

// YankLine puts back what CutLine took.
func (c *Client) YankLine(target string) error {
	if target == "" {
		return errors.New("refusing to yank at an empty target")
	}

	if _, err := c.query("send-keys", "-t", target, "C-y"); err != nil {
		return fmt.Errorf("failed to yank at %q: %w", target, err)
	}

	return nil
}

// paste loads the text into a buffer of weave's own and pastes it, running whatever commands
// follow in the same invocation.
func (c *Client) paste(target, text string, then ...string) error {
	if target == "" {
		return errors.New("refusing to paste into an empty target")
	}
	if text == "" {
		return errors.New("refusing to paste nothing")
	}

	if _, err := c.query(pasteArgs(target, text, then...)...); err != nil {
		return fmt.Errorf("failed to paste into %q: %w", target, err)
	}

	return nil
}

// pasteArgs is the one invocation a paste is sent as.
func pasteArgs(target, text string, then ...string) []string {
	// -d drops the buffer once it has been pasted, and -p wraps it in the markers that say the
	// text was pasted rather than typed.
	args := []string{
		"set-buffer", "-b", pasteBuffer, "--", text,
		";", "paste-buffer", "-p", "-d", "-b", pasteBuffer, "-t", target,
	}

	return append(args, then...)
}
