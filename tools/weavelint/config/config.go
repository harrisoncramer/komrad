// Package config is the shape of the settings block weave's .golangci.yaml hands the plugin.
package config

// Config is every rule's settings.
type Config struct {
	SentinelKind SentinelKindConfig `json:"sentinel_kind"`
}

// SentinelKindConfig settles where the sentinel rule applies.
type SentinelKindConfig struct {
	// Skip turns the rule off.
	Skip bool `json:"skip"`
	// Exclude is unanchored regexps over the file path, for the packages no caller reaches
	// through the RPC layer.
	Exclude []string `json:"exclude"`
}
