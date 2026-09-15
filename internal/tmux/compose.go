package tmux

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// pasteBufferPrefix opens the name of every buffer a paste is loaded through.
const pasteBufferPrefix = "weave-paste"

// pastes counts the pastes this process has sent, which a buffer name carries so two pastes at
// once cannot overwrite each other's text.
var pastes atomic.Int64

// Paste inserts text into a pane as a bracketed paste.
func (c *Client) Paste(target, text string) error {
	return c.paste(target, text)
}

// pasteSettleDelay is the pause between a bracketed paste and the Enter that submits it.
// Claude Code needs time to process the paste-end marker (\e[201~) before it will accept
// Enter as a submission; an Enter that lands in the same PTY write is swallowed.
const pasteSettleDelay = 100 * time.Millisecond

// PasteAndSubmit inserts text as a bracketed paste, waits for the program to leave paste
// mode, then sends Enter.
func (c *Client) PasteAndSubmit(target, text string) error {
	if err := c.paste(target, text); err != nil {
		return err
	}

	time.Sleep(pasteSettleDelay)

	if _, err := c.query("send-keys", "-t", target, "Enter"); err != nil {
		return fmt.Errorf("failed to submit at %q: %w", target, err)
	}

	return nil
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

// paste loads the text into a buffer of weave's own and pastes it.
func (c *Client) paste(target, text string) error {
	if target == "" {
		return errors.New("refusing to paste into an empty target")
	}
	if text == "" {
		return errors.New("refusing to paste nothing")
	}

	if _, err := c.query(pasteArgs(pasteBufferName(), target, text)...); err != nil {
		return fmt.Errorf("failed to paste into %q: %w", target, err)
	}

	return nil
}

// pasteBufferName is a buffer name no other paste on this server holds.
func pasteBufferName() string {
	return fmt.Sprintf("%s-%d-%d", pasteBufferPrefix, os.Getpid(), pastes.Add(1))
}

// pasteArgs is the one invocation a paste is sent as.
func pasteArgs(buffer, target, text string) []string {
	// -d drops the buffer once it has been pasted, and -p wraps it in the markers that say the
	// text was pasted rather than typed.
	return []string{
		"set-buffer", "-b", buffer, "--", text,
		";", "paste-buffer", "-p", "-d", "-b", buffer, "-t", target,
	}
}
