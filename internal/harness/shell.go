package harness

import "strings"

// SplitArgs breaks a flag string into arguments the way a shell would, honoring
// single and double quotes so a flag whose value contains a space stays one
// argument. It finds argument boundaries and expands nothing.
//
// A launch command is typed into a pane with send-keys, so the shell in that pane
// parses whatever a harness assembles. Flags arrive as one configured string and
// have to survive that trip as data. Unquoted, a flag holding $(...), a backtick,
// or a semicolon would run as a command in the user's shell, and flags can arrive
// over RPC.
func SplitArgs(flags string) []string {
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

// QuoteArg wraps an argument in single quotes so the shell takes it literally. A
// single quote inside the argument closes the quoting, escapes itself, and
// reopens, the only way to represent one inside a single-quoted string.
func QuoteArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// QuoteFlags renders a configured flag string as shell-safe arguments, ready to
// append to a command line.
func QuoteFlags(flags string) string {
	args := SplitArgs(flags)
	if len(args) == 0 {
		return ""
	}

	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = QuoteArg(arg)
	}

	return strings.Join(quoted, " ")
}
