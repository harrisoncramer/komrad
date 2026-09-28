package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	documentstore "komrad/internal/store/document"
	mailstore "komrad/internal/store/mail"

	"github.com/riverqueue/river"
)

const personWroteTopic = "document_written_by_person"

// PersonWrote is a person writing a document an agent owns.
type PersonWrote struct {
	DocumentID string `json:"document_id"`
	Created    bool   `json:"created"`
}

// Topic names the handler that tells the document's owner.
func (PersonWrote) Topic() string {
	return personWroteTopic
}

// EventHandler keeps one unread mail per document telling its owner a person wrote it.
type EventHandler struct{}

// NewEventHandler builds the handler the router is given for a person's writes.
func NewEventHandler() *EventHandler {
	return &EventHandler{}
}

// Topic is the topic of PersonWrote.
func (*EventHandler) Topic() string {
	return personWroteTopic
}

// Handle tells the owner of the document a person wrote.
func (*EventHandler) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var wrote PersonWrote
	if err := json.Unmarshal(payload, &wrote); err != nil {
		return nil, fmt.Errorf("failed to read a document write: %w", err)
	}

	document, err := documentstore.NewDocumentStore(tx).Get(ctx, wrote.DocumentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	body := buildEditMail(*document)
	if wrote.Created {
		body = buildCreateMail(*document)
	}

	return nil, events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: document.AgentID,
		Kind:      mailstore.KindDocumentEdited,
		SubjectID: document.ID,
		Body:      body,
	})
}
