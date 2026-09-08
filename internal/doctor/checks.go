package doctor

import "weave/lib/xslice"

// Checks is every assertion doctor makes, running from the dependencies weave cannot install for
// itself to the ones it can, with each check depending only on one declared before it.
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
