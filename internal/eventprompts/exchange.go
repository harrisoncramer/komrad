package eventprompts

import (
	"fmt"

	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
)

// ExchangeCapped tells an agent that mail written to it was refused because the pair reached the exchange limit.
type ExchangeCapped struct {
	SenderName string
	SenderID   uuid.UUID
	Exchanged  int64
}

// Kind is the kind the exchange cap mail is filed under.
func (ExchangeCapped) Kind() agentv1.MailKind {
	return agentv1.MailKind_MAIL_KIND_EXCHANGE_CAPPED
}

// Events is empty, since reaching the cap starts no event.
func (ExchangeCapped) Events() []eventv1.Event {
	return nil
}

// Body tells the agent that nothing more will get through from the sender.
func (m ExchangeCapped) Body() string {
	return fmt.Sprintf("komrad: %s (%s) wrote to you and the send was refused, because the two of you have now exchanged %d pieces of mail, which is the limit. "+
		"Nothing further will get through between you, in either direction. "+
		"Whatever you were waiting on from them is not coming, so do not keep waiting: say where you got to, and tell the person what is unfinished. "+
		"A row has been marked for attention in the panel so the person knows to look.",
		m.SenderName, m.SenderID, m.Exchanged)
}
