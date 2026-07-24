package agents

import "strings"

// A launch command is typed into a pane with send-keys, so the shell in that
// pane parses whatever we assemble. Flags reach us as one configured string and
// must survive that trip as data: without quoting, a flag containing $(...), a
// backtick, or a semicolon would run as a command in the user's shell, and those
// flags can arrive over RPC.
//
// splitArgs breaks a flag string into arguments the way a shell would, honoring
// single and double quotes so a flag whose value contains a space stays one
// argument. Quoting rules inside the quotes are not interpreted any further:
// these are argument boundaries, not an expansion pass.
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

// quoteArg wraps an argument in single quotes so the shell takes it literally. A
// single quote inside the argument closes the quoting, escapes itself, and
// reopens, which is the only way to represent one inside a single-quoted string.
func quoteArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// quoteFlags renders a configured flag string as shell-safe arguments, ready to
// append to a command line.
func quoteFlags(flags string) string {
	args := splitArgs(flags)
	if len(args) == 0 {
		return ""
	}

	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteArg(arg)
	}

	return strings.Join(quoted, " ")
}
