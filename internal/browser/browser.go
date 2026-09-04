// Package browser hands a URL to whatever the person at the panel opens links with, which is
// the desktop's own opener rather than a browser weave picks. Nothing here knows what the URL
// is for, so anything with an address to show a person can use it.
package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	openTimeout = 30 * time.Second
	detailCap   = 400
)

var ErrNotAWebURL = errors.New("not an http or https URL")

// Opener is what anything with a URL to show a person calls, declared here rather than beside
// each caller so one stand-in serves all of them in tests.
type Opener interface {
	OpenURL(ctx context.Context, target string) error
}

// Desktop opens a URL through the desktop's own opener.
type Desktop struct{}

// NewDesktop returns an opener that shells out to the desktop's own `open`, or `xdg-open` where
// there is one.
func NewDesktop() *Desktop {
	return &Desktop{}
}

// OpenURL waits for the opener rather than the browser, which is as far as anything can see.
func (*Desktop) OpenURL(ctx context.Context, target string) error {
	// An opener given a path or an app's own scheme acts on it, and a URL weave was handed is
	// not always one weave wrote, so only a web address is passed on.
	if !isWebURL(target) {
		return fmt.Errorf("%w: %q", ErrNotAWebURL, target)
	}

	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	// xdg-open is asked for first because a Linux `open` is openvt, which would take over a
	// virtual terminal rather than open anything.
	opener := "open"
	if found, err := exec.LookPath("xdg-open"); err == nil {
		opener = found
	}

	output, err := exec.CommandContext(ctx, opener, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w%s", opener, target, err, detail(output))
	}

	return nil
}

func isWebURL(target string) bool {
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func detail(output []byte) string {
	said := bytes.TrimSpace(output)
	if len(said) == 0 {
		return ""
	}

	if len(said) > detailCap {
		said = said[len(said)-detailCap:]
	}

	// The cut is by bytes, so the head of what is kept can be half a rune, which Postgres and
	// Connect both refuse further down.
	return ": " + strings.ToValidUTF8(string(said), "")
}
