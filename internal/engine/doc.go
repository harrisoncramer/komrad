// Package engine says how the panel reaches the engine its agents run on, and signs the person
// in to a remote one.
//
// A local engine shares the panel's tmux server, so the panel moves an agent's pane into view
// with swap-pane. A [Remote] engine answers at a URL, and the panel streams an agent's terminal
// over its API. [Login] signs in through the engine's provider in a browser, the [SignInKeeper]
// keeps the result in the keychain or in private files, and the bearer transport refreshes the
// ID token on each request.
package engine
