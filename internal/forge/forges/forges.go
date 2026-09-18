// Package forges is every forge this build can speak to, by kind.
package forges

import (
	"weave/internal/forge"
	"weave/internal/forge/github"
)

// Builders makes the forge for each kind this build knows.
var Builders = map[string]forge.Builder{
	forge.KindGitHub: github.Build,
}
