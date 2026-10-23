package eventprompts

import (
	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"
)

// Mail is what komrad writes an agent about one thing that happened.
type Mail interface {
	Kind() agentv1.MailKind
	Events() []eventv1.Event
	Body() string
}
