// Package eventmail writes the mail komrad sends an agent about each event, from the fields of what happened.
package eventmail

import mailstore "komrad/internal/store/mail"

// Mail is what komrad writes an agent about one thing that happened.
type Mail interface {
	Kind() mailstore.Kind
	Events() []string
	Body() string
}
