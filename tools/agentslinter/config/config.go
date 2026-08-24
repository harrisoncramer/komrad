package config

type Config struct {
	CommentPrefix CommentPrefixConfig `json:"comment_prefix"`
	ForbidGetenv  ForbidGetenvConfig  `json:"forbid_getenv"`
}

type CommentPrefixConfig struct {
	Skip bool `json:"skip"`
}

type ForbidGetenvConfig struct {
	Skip bool `json:"skip"`
}
