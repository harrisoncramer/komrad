package eventprompts

import (
	"fmt"

	mailstore "komrad/internal/store/mail"
)

// ExchangeCapped tells an agent that mail written to it was refused because the pair reached the exchange limit.
type ExchangeCapped struct {
	SenderName string
	SenderID   string
	Exchanged  int64
}

// Kind is the kind the exchange cap mail is filed under.
func (ExchangeCapped) Kind() mailstore.Kind {
	return mailstore.KindExchangeCapped
}

// Events is empty, since reaching the cap starts no event.
func (ExchangeCapped) Events() []string {
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
