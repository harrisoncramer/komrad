// Package config loads komrad's settings from a stack of TOML files, a later file winning over an
// earlier one, with a remote engine's file layered on top when one is named. The settings cover the
// harnesses and their flags, the panel's layout, keybindings, glyphs and colours, hooks and
// worktrees, forges, mail, logging, and the database and address this komrad uses.
//
// The [Config] struct is the source of truth for the documented config.toml and for the config
// reference on the docs site, both rendered from its field comments. This package is also the
// only one that looks up an environment variable by name.
package config
