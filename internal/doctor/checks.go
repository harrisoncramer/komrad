package doctor

import "weave/lib/xslice"

// Checks is every assertion doctor makes, in order, each depending only on a check declared
// before it.
func Checks(settings Settings) []Check {
	checks := []Check{
		newTmuxBinaryCheck(),
		newTmuxVersionCheck(),
		newTmuxShellCheck(),
		newConfigCheck(),
		newKeybindingsCheck(),
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
