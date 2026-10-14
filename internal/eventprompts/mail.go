package eventprompts

import mailstore "komrad/internal/store/mail"

// Mail is what komrad writes an agent about one thing that happened.
type Mail interface {
	Kind() mailstore.Kind
	Events() []string
	Body() string
}
