package plugin

import (
	commentprefix "agentslinter/analyzers/comment_prefix"
	forbidgetenv "agentslinter/analyzers/forbidgetenv"
	"agentslinter/config"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("agentslinter", func(settings any) (register.LinterPlugin, error) {
		s, err := register.DecodeSettings[config.Config](settings)
		if err != nil {
			return nil, err
		}

		return &agentsLinter{config: s}, nil
	})
}

type agentsLinter struct {
	config config.Config
}

// BuildAnalyzers returns every analyzer this plugin contributes.
func (a *agentsLinter) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	var analyzers []*analysis.Analyzer

	if !a.config.CommentPrefix.Skip {
		analyzers = append(analyzers, commentprefix.NewAnalyzer(a.config.CommentPrefix))
	}

	if !a.config.ForbidGetenv.Skip {
		analyzers = append(analyzers, forbidgetenv.NewAnalyzer(a.config.ForbidGetenv))
	}

	return analyzers, nil
}

// GetLoadMode tells golangci-lint the analyzers need full type information.
func (a *agentsLinter) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
