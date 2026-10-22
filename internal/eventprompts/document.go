package eventprompts

import (
	"fmt"

	mailstore "komrad/internal/store/mail"

	"github.com/google/uuid"
)

// DocumentWritten tells an agent that a person wrote one of its documents in the panel.
type DocumentWritten struct {
	DocumentID uuid.UUID
	Name       string
	Version    int32
	// Created says the person wrote the document for the agent rather than editing one the agent wrote.
	Created bool
}

// Kind is the kind document mail is filed under.
func (DocumentWritten) Kind() mailstore.Kind {
	return mailstore.KindDocumentEdited
}

// Events is the event a person's write starts.
func (DocumentWritten) Events() []string {
	return []string{DocumentWrittenByPerson}
}

// Body tells the agent which document to read and how to write over it.
func (m DocumentWritten) Body() string {
	if m.Created {
		return fmt.Sprintf("komrad: a person wrote a new document for you in the panel, %q (%s). "+
			"Read it with get_document. A further save before you read this rewrites this mail rather than sending another, and "+
			"write_document is refused as failed_precondition unless you rewrite from the newest version list_document_versions lists.",
			m.Name, m.DocumentID)
	}

	return fmt.Sprintf("komrad: a person edited your document %q (%s) in the panel, writing version %d. "+
		"A further save before you read this rewrites this mail rather than sending another. "+
		"list_document_versions lists every write and marks a person's, get_document reads any version, "+
		"and write_document is refused as failed_precondition unless you rewrite from the newest version.",
		m.Name, m.DocumentID, m.Version)
}
