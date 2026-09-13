// Package weavelint holds the lint rules that are about weave's own conventions rather than
// about Go, and registers them with golangci-lint so they run in the one lint pass.
//
// It is a module of its own because golangci-lint builds its custom binary by importing the
// plugin by module path, which it cannot do for a package inside a module whose root is a
// command. Nothing else imports it, and `go list ./...` in the main module does not see it, so
// its own tests are run by the test task rather than by the suite.
//
// The rules read syntax rather than types, which is enough for what they ask and keeps the lint
// pass cheap. The cost is that a name is only a name: a package aliased to errors reads as the
// standard library's, and one brought in by a dot import does not read as anything at all.
package weavelint

import (
	"weavelint/config"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("weavelint", func(settings any) (register.LinterPlugin, error) {
		decoded, err := register.DecodeSettings[config.Config](settings)
		if err != nil {
			return nil, err
		}

		return &linter{config: decoded}, nil
	})
}

// linter is the plugin golangci-lint holds while it runs.
type linter struct {
	config config.Config
}

// BuildAnalyzers returns every analyzer this plugin contributes.
func (l *linter) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	sentinels, err := newSentinelKind(l.config.SentinelKind)
	if err != nil {
		return nil, err
	}

	return []*analysis.Analyzer{sentinels}, nil
}

// GetLoadMode tells golangci-lint the analyzers read syntax alone.
func (l *linter) GetLoadMode() string { return register.LoadModeSyntax }
