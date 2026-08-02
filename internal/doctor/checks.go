package doctor

// Checks is every assertion doctor makes, in the order it reports them.
//
// The order is what a person reads down, so it runs from the dependencies weave cannot
// install for itself to the ones it can: a machine with no tmux has a problem worth
// reading before anything about a database.
func Checks() []Check {
	return []Check{
		TmuxBinary(),
		TmuxVersion(),
	}
}
