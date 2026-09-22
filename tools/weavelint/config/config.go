// Package config is the shape of the settings block weave's .golangci.yaml hands the plugin.
package config

// Config is every rule's settings.
type Config struct {
	SentinelKind     SentinelKindConfig     `json:"sentinel_kind"`
	SentinelInline   SentinelInlineConfig   `json:"sentinel_inline"`
	WrapPhrase       WrapPhraseConfig       `json:"wrap_phrase"`
	HandlerShape     HandlerShapeConfig     `json:"handler_shape"`
	SecretExpose     SecretExposeConfig     `json:"secret_expose"`
	BareHTTP         BareHTTPConfig         `json:"bare_http"`
	OnelineFunc      OnelineFuncConfig      `json:"oneline_func"`
	ConversionName   ConversionNameConfig   `json:"conversion_name"`
	CommentClause    CommentClauseConfig    `json:"comment_clause"`
	SingleAppend     SingleAppendConfig     `json:"single_append"`
	ConstructorShape ConstructorShapeConfig `json:"constructor_shape"`
	StoreTouch       StoreTouchConfig       `json:"store_touch"`
}

// ConstructorShapeConfig settles where the constructor rule applies.
type ConstructorShapeConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path.
	Exclude []string `json:"exclude"`
}

// StoreTouchConfig settles which stores the touch rule reads and what it counts as a write.
type StoreTouchConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Stores is unanchored regexps over the file path, for the packages that write rows.
	Stores []string `json:"stores"`
	// Queries is the directory holding the sqlc query files, relative to where lint runs.
	Queries string `json:"queries"`
	// Listings is each listing the panel watches, keyed by its table, naming the sqlc queries
	// that read it.
	Listings map[string][]string `json:"listings"`
}

// SingleAppendConfig settles where the mapping loop rule applies.
type SingleAppendConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path, for the package that defines Map and
	// the generated code nobody edits.
	Exclude []string `json:"exclude"`
}

// CommentClauseConfig settles where the comment clause rule applies.
type CommentClauseConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path, for the one file whose doc comments
	// are the prose of the generated config.
	Exclude []string `json:"exclude"`
}

// OnelineFuncConfig settles whether the one-line declaration rule runs.
type OnelineFuncConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
}

// ConversionNameConfig settles which packages each naming of a proto converter holds in.
type ConversionNameConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Render is unanchored regexps over the file path, for the packages whose functions build
	// proto messages and so are named render.
	Render []string `json:"render"`
	// From is unanchored regexps over the file path, for the packages whose functions read
	// proto messages and so are named xFrom rather than protoToX, xFromProto or renderX.
	From []string `json:"from"`
}

// BareHTTPConfig settles where net/http's own clients may be built.
type BareHTTPConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path, for the one package that builds the
	// configured client everything else uses.
	Exclude []string `json:"exclude"`
}

// SecretExposeConfig settles where a forge Secret may give up its token.
type SecretExposeConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Allow is the functions Expose may be called in, spelled as types.Func.FullName spells
	// them, such as (weave/internal/forge.Credential).HeaderValue.
	Allow []string `json:"allow"`
}

// HandlerShapeConfig settles where the handler rule applies.
type HandlerShapeConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the path lint was run from, for the adapter itself and
	// for the stand-ins that serve canned data rather than a domain's failures.
	Exclude []string `json:"exclude"`
}

// SentinelKindConfig settles where the sentinel rule applies.
type SentinelKindConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path, for the packages no caller reaches
	// through the RPC layer.
	Exclude []string `json:"exclude"`
}

// SentinelInlineConfig settles where the inline sentinel rule applies.
type SentinelInlineConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path.
	Exclude []string `json:"exclude"`
}

// WrapPhraseConfig settles where the wrapping message rule applies.
type WrapPhraseConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path, for the packages whose messages open
	// with the config key they are about.
	Exclude []string `json:"exclude"`
}
