package documents

import (
	"context"
	"errors"
	"slices"

	"komrad/internal/eventmail"
	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	documentstore "komrad/internal/store/document"
	mailstore "komrad/internal/store/mail"

	"github.com/riverqueue/river"
)

const personWroteTopic = "document_written_by_person"

// personWrote is a person writing a document an agent owns.
type personWrote struct {
	DocumentID string `json:"document_id"`
}

// Topic names the handler that tells the document's owner.
func (personWrote) Topic() string {
	return personWroteTopic
}

// NewEventHandler builds the handler the router calls for a person's writes.
func NewEventHandler() events.HandlerFor[personWrote] {
	return handlePersonWrote
}

// handlePersonWrote tells the owner of the document a person wrote.
func handlePersonWrote(ctx context.Context, tx store.DBTX, mailer *mail.MailService, wrote personWrote) ([]river.JobArgs, error) {
	documents := documentstore.NewDocumentStore(tx)
	document, err := documents.Get(ctx, wrote.DocumentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	versions, err := documents.ListVersions(ctx, document.ID)
	if err != nil {
		return nil, err
	}

	told, err := mailstore.NewMailStore(tx).ExistsReadAbout(ctx, document.AgentID, mailstore.KindDocumentEdited, document.ID)
	if err != nil {
		return nil, err
	}

	_, err = events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: document.AgentID,
		Mail: eventmail.DocumentWritten{
			DocumentID: document.ID,
			Name:       document.Name,
			Version:    document.Version,
			Created:    !told && !slices.ContainsFunc(versions, func(version documentstore.Version) bool { return !version.ByPerson }),
		},
		SubjectID: document.ID,
	})

	return nil, err
}
