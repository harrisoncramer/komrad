// Package principalrole names the roles a principal may hold on an engine.
package principalrole

import (
	"slices"
	"strings"

	"komrad/internal/fail"
	"komrad/lib/xslice"
)

// Role is what a principal may do on one engine.
type Role string

const (
	Viewer     Role = "viewer"
	Operator   Role = "operator"
	Automation Role = "automation"
)

// ErrUnknown is a role that is none of Roles.
var ErrUnknown = fail.InvalidArgument("unknown principal role")

// Roles is every role a principal may hold.
var Roles = []Role{Viewer, Operator, Automation}

// Parse reads a role as the database and a token spell it.
func Parse(spelled string) (Role, error) {
	role := Role(spelled)
	if !slices.Contains(Roles, role) {
		names := xslice.Map(Roles, func(role Role) string {
			return string(role)
		})

		return "", ErrUnknown.Saying("unknown role %q, want one of %s", spelled, strings.Join(names, ", "))
	}

	return role, nil
}
