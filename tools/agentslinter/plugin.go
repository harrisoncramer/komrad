// Package plugin is the entry point golangci-lint builds into a custom binary.
package plugin

import (
	forbidgetenv "agentslinter/analyzers/forbidgetenv"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("agentslinter", func(any) (register.LinterPlugin, error) {
		return &agentsLinter{}, nil
	})
}

type agentsLinter struct{}

// BuildAnalyzers returns every analyzer this plugin contributes.
func (a *agentsLinter) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{forbidgetenv.NewAnalyzer()}, nil
}

// GetLoadMode tells golangci-lint the analyzers need full type information.
func (a *agentsLinter) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
