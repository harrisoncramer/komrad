package doctor

// Checks is every assertion doctor makes, in the order it reports them.
//
// The order is what a person reads down, so it runs from the dependencies weave cannot
// install for itself to the ones it can: a machine with no tmux has a problem worth reading
// before anything about a config file. It also has to be an order a dependency can be
// declared in, since a check reads the conclusion of one listed before it.
//
// The list is built from the settings rather than fixed, because which agent CLIs there
// are to check is what this build was compiled with.
func Checks(settings Settings) []Check {
	checks := []Check{
		TmuxBinary(),
		TmuxVersion(),
		TmuxShell(),
		Config(),
		DefaultHarness(),
	}

	for _, agent := range settings.Harnesses {
		checks = append(checks, HarnessBinary(agent))
	}

	return checks
}
