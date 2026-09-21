// Package weavelint holds the lint rules that are about weave's own conventions rather than
// about Go, and registers them with golangci-lint so they run in the one lint pass.
//
// It is a module of its own because golangci-lint builds its custom binary by importing the
// plugin by module path, which it cannot do for a package inside a module whose root is a
// command. Nothing else imports it, and `go list ./...` in the main module does not see it, so
// its own tests are run by the test task rather than by the suite.
//
// The sentinel and handler rules read syntax alone, so for them a name is only a name: a package
// aliased to errors reads as the standard library's, and one brought in by a dot import does not
// read as anything at all. The secret rule needs to know a method's receiver, so the plugin
// loads types for every package it runs over, which the other linters in the pass already pay
// for.
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

	inline, err := newSentinelInline(l.config.SentinelInline)
	if err != nil {
		return nil, err
	}

	phrases, err := newWrapPhrase(l.config.WrapPhrase)
	if err != nil {
		return nil, err
	}

	handlers, err := newHandlerShape(l.config.HandlerShape)
	if err != nil {
		return nil, err
	}

	bare, err := newBareHTTP(l.config.BareHTTP)
	if err != nil {
		return nil, err
	}

	return []*analysis.Analyzer{sentinels, inline, phrases, handlers, newSecretExpose(l.config.SecretExpose), bare}, nil
}

// GetLoadMode tells golangci-lint the analyzers need types resolved.
func (l *linter) GetLoadMode() string { return register.LoadModeTypesInfo }
