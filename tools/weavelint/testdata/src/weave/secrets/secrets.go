package secrets

import (
	"fmt"

	"weave/internal/forge"
)

type other struct{}

func (other) Expose() string { return "not a secret" }

func describe(s forge.Secret) string {
	return fmt.Sprintf("token %s", s.Expose()) // want `Expose gives up the token; weave/secrets.describe is not a place allowed to send or keep it`
}

func fine(s forge.Secret) string {
	return fmt.Sprint(s)
}

func foreign(o other) string {
	return o.Expose()
}

type exposer interface{ Expose() string }

func laundered(s forge.Secret) string {
	var e exposer = s

	return e.Expose()
}
