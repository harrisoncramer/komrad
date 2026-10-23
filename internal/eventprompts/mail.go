package eventprompts

import (
	mailstore "komrad/internal/store/mail"
	eventv1 "komrad/proto/gen/event/v1"
)

// Mail is what komrad writes an agent about one thing that happened.
type Mail interface {
	Kind() mailstore.Kind
	Events() []eventv1.Event
	Body() string
}
