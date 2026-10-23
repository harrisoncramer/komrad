// Package komradlint holds the lint rules about komrad's own conventions, as opposed to Go's, and
// registers them with golangci-lint as a plugin so they run in the same lint pass.
//
// The package is a module of its own because golangci-lint builds its custom binary by importing
// the plugin by module path. The go:test task runs its tests, since `go list ./...` in the main
// module does not see it. Some rules need types, so the plugin asks golangci-lint to load them
// for every package it checks.
package komradlint
