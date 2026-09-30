package documents

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
}

// Topic names the handler that tells the document's owner.
func (PersonWrote) Topic() string {
	return personWroteTopic
}

// NewEventHandler builds the handler the router is given for a person's writes.
func NewEventHandler() events.HandlerFor[PersonWrote] {
	return handlePersonWrote
}

// handlePersonWrote tells the owner of the document a person wrote.
func handlePersonWrote(ctx context.Context, tx store.DBTX, mailer *mail.MailService, wrote PersonWrote) ([]river.JobArgs, error) {
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

	body := buildEditMail(*document)
	if !told && !slices.ContainsFunc(versions, func(version documentstore.Version) bool { return !version.ByPerson }) {
		body = buildCreateMail(*document)
	}

	return nil, events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: document.AgentID,
		Kind:      mailstore.KindDocumentEdited,
		SubjectID: document.ID,
		Body:      body,
	})
}

func buildCreateMail(created documentstore.Document) string {
	return fmt.Sprintf("komrad: a person wrote a new document for you in the panel, %q (%s). "+
		"Read it with get_document. A further save before you read this rewrites this mail rather than sending another, and "+
		"write_document is refused as failed_precondition unless you rewrite from the newest version list_document_versions lists.",
		created.Name, created.ID)
}

func buildEditMail(edited documentstore.Document) string {
	return fmt.Sprintf("komrad: a person edited your document %q (%s) in the panel, writing version %d. "+
		"A further save before you read this rewrites this mail rather than sending another. "+
		"list_document_versions lists every write and marks a person's, get_document reads any version, "+
		"and write_document is refused as failed_precondition unless you rewrite from the newest version.",
		edited.Name, edited.ID, edited.Version)
}
