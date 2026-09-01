// Package config is the settings the analyzers take from .golangci.yaml, which is how a rule is
// turned off or pointed away from the files it does not apply to.
package config

type Config struct {
	CommentLength  CommentLengthConfig  `json:"comment_length"`
	CommentPrefix  CommentPrefixConfig  `json:"comment_prefix"`
	ContextTimeout ContextTimeoutConfig `json:"context_timeout"`
	ForbidGetenv   ForbidGetenvConfig   `json:"forbid_getenv"`
	ForbidNilNil   ForbidNilNilConfig   `json:"forbid_nil_nil"`
}

type ContextTimeoutConfig struct {
	Skip    bool     `json:"skip"`
	Include []string `json:"include"`
}

type CommentLengthConfig struct {
	Skip    bool     `json:"skip"`
	Exclude []string `json:"exclude"`
}

type CommentPrefixConfig struct {
	Skip    bool `json:"skip"`
	Require bool `json:"require"`
}

type ForbidGetenvConfig struct {
	Skip bool `json:"skip"`
}

type ForbidNilNilConfig struct {
	Skip bool `json:"skip"`
}
