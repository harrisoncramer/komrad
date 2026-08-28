package config

type Config struct {
	CommentLength CommentLengthConfig `json:"comment_length"`
	CommentPrefix CommentPrefixConfig `json:"comment_prefix"`
	ForbidGetenv  ForbidGetenvConfig  `json:"forbid_getenv"`
	ForbidNilNil  ForbidNilNilConfig  `json:"forbid_nil_nil"`
}

type CommentLengthConfig struct {
	Skip    bool     `json:"skip"`
	Exclude []string `json:"exclude"`
}

type CommentPrefixConfig struct {
	Skip bool `json:"skip"`
}

type ForbidGetenvConfig struct {
	Skip bool `json:"skip"`
}

type ForbidNilNilConfig struct {
	Skip bool `json:"skip"`
}
