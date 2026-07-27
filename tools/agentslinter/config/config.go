// Package config holds the settings for all agents linters. Configuration should be scoped per linter.
// This data is set as key/value pairs in the .golangci.yaml configuration file.
package config

type Config struct {
	CommentPrefix CommentPrefixConfig `json:"comment_prefix"`
	ForbidGetenv  ForbidGetenvConfig  `json:"forbid_getenv"`
}

type CommentPrefixConfig struct {
	// Whether to skip running the comment prefix linter.
	Skip bool `json:"skip"`
}

type ForbidGetenvConfig struct {
	// Whether to skip running the forbid getenv linter.
	Skip bool `json:"skip"`
}
