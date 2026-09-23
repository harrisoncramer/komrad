package doctor

import "weave/lib/xslice"

// HereChecks is what the doctor makes on the machine it runs on: what the panel and the CLI
// there need, and the service as it is reached from there.
func HereChecks() []Check {
	return []Check{
		newTmuxBinaryCheck(),
		newTmuxVersionCheck(),
		newConfigCheck(),
		newKeybindingsCheck(),
		newServerCheck(),
		newForgesCheck(),
		newTerminalGlyphCheck(),
		newTerminalEmphasisCheck(),
		newTerminalColorCheck(),
	}
}

// EngineChecks is what the service makes on the machine the engine runs on, and what the doctor
// asks an engine for: what the agents there need.
func EngineChecks(settings Settings) []Check {
	checks := []Check{
		newTmuxBinaryCheck(),
		newTmuxVersionCheck(),
		newTmuxShellCheck(),
		newConfigCheck(),
		newDefaultHarnessCheck(),
		newHooksCheck(),
		newWorktreesCheck(),
		newContextCheck(),
		newDatabaseCheck(),
		newBasesCheck(),
		newSchemaCheck(),
		newTenantCheck(),
		newReadOnlyLoginCheck(),
		newOrphanedWorktreesCheck(),
	}

	return append(checks, xslice.Map(settings.Harnesses, newHarnessBinaryCheck)...)
}

// Checks is every check when the engine is the machine the doctor runs on: the ones about
// here, then the ones about the engine that here did not already make.
func Checks(settings Settings) []Check {
	checks := HereChecks()
	made := make(map[string]bool, len(checks))
	for _, check := range checks {
		made[check.Name] = true
	}

	for _, check := range EngineChecks(settings) {
		if !made[check.Name] {
			checks = append(checks, check)
		}
	}

	return checks
}
