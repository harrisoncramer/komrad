package harness

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"weave/lib/xslice"
)

var ErrShellUnsupported = errors.New("unsupported pane shell")

var posixShells = map[string]bool{
	"ash":   true,
	"bash":  true,
	"dash":  true,
	"ksh":   true,
	"ksh93": true,
	"mksh":  true,
	"sh":    true,
	"zsh":   true,
}

// splitArgs breaks a flag string into arguments the way a shell would, honoring single
// and double quotes so a flag whose value contains a space stays one argument.
func splitArgs(flags string) []string {
	var (
		args    []string
		current strings.Builder
		quote   rune
		started bool
	)

	for _, r := range flags {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				break
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if started {
				args = append(args, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}

	if started {
		args = append(args, current.String())
	}

	return args
}

// QuoteArg wraps an argument in single quotes so the pane's shell takes it literally.
func QuoteArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// QuoteFlags renders a configured flag string as shell-safe arguments, ready to
// append to a command line.
func QuoteFlags(flags string) string {
	args := splitArgs(flags)
	if len(args) == 0 {
		return ""
	}

	return strings.Join(xslice.Map(args, QuoteArg), " ")
}

// RequirePOSIXShell refuses a shell that would leave a launch as a parse error in the pane.
func RequirePOSIXShell(shell string) error {
	// An empty shell is accepted, tmux declining to report one being no evidence of a bad one.
	if shell == "" || posixShells[filepath.Base(shell)] {
		return nil
	}

	return fmt.Errorf(
		"%w: tmux default-shell is %s, which cannot parse the command that starts an agent; "+
			"set default-shell to a POSIX shell such as /bin/sh",
		ErrShellUnsupported, shell,
	)
}
