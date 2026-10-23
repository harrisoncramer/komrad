// Package app mints short-lived GitHub tokens from a GitHub App's private key, for forge calls
// and for git on the engine through a credential helper.
//
// A personal access token or a deploy key would leave a secret on the engine that never expires
// and may reach more than one repository. An App token expires after an hour and reaches only
// the repositories the App is installed on. A [Cache] keeps the minted token so each git
// operation does not mint its own.
package app
