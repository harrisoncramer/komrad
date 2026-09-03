package doctor

import "weave/lib/xslice"

// Checks is every assertion doctor makes, in the order it reports them; the order runs from
// the dependencies weave cannot install for itself to the ones it can, and a check can only
// depend on one declared before it, since it reads that check's conclusion.
func Checks(settings Settings) []Check {
	checks := []Check{
		newTmuxBinaryCheck(),
		newTmuxVersionCheck(),
		newTmuxShellCheck(),
		newConfigCheck(),
		newDefaultHarnessCheck(),
		newHooksCheck(),
		newWorktreesCheck(),
		newDatabaseCheck(),
		newSchemaCheck(),
		newReadOnlyLoginCheck(),
		newOrphanedWorktreesCheck(),
		newServerCheck(),
	}

	checks = append(checks, xslice.Map(settings.Harnesses, newHarnessBinaryCheck)...)

	return checks
}
