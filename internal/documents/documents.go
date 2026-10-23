package documents

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"
	"komrad/proto/gen/oneofs"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// MaxBodyBytes is the most a document's body may hold.
const MaxBodyBytes = 1 << 20

var (
	// ErrTooLarge is a body over MaxBodyBytes.
	ErrTooLarge = fail.InvalidArgument("the document is larger than 1 MiB")
	// ErrNotUTF8 is a body that is not valid UTF-8.
	ErrNotUTF8 = fail.InvalidArgument("the document is not valid UTF-8")
	// ErrUnreadablePath is a path that names no regular file the service can read.
	ErrUnreadablePath = fail.InvalidArgument("the path names no regular file that can be read")
	// ErrEmptyFile is a path naming a file with nothing in it.
	ErrEmptyFile = fail.InvalidArgument("the file is empty")
	// ErrNoBody is a write naming neither a body nor a path.
	ErrNoBody = fail.InvalidArgument("a document needs a body or a path to read one from")
	// ErrNoAuthor is a new document written by something that is not an agent, naming no agent it is for.
	ErrNoAuthor = fail.InvalidArgument("a document created by something that is not an agent needs the agent it is for")
	// ErrNoVersion is a rewrite that does not say which version it replaces.
	ErrNoVersion = fail.InvalidArgument("a rewrite needs the version it replaces, as the last read or write returned it")
	// ErrNoTypeVersion is a type rewrite that does not say which version it replaces.
	ErrNoTypeVersion = fail.InvalidArgument("a document type rewrite needs the version it replaces, as the last read or write returned it")
	// ErrForAnother is a rewrite naming an agent other than the one whose document it is.
	ErrForAnother = fail.InvalidArgument("the document belongs to another agent, so leave agent_id unset to rewrite it")
	// ErrAgentWritingForAnother is an agent writing a document for an agent other than itself.
	ErrAgentWritingForAnother = fail.InvalidArgument("an agent writes only its own documents, so leave agent_id unset")
	// ErrDocumentTypeByAgent is an agent writing or deleting a document type.
	ErrDocumentTypeByAgent = fail.Denied("only a person writes or deletes a document type")
	// ErrDeleteByAgent is an agent deleting a document.
	ErrDeleteByAgent = fail.Denied("only a person deletes a document")
)

type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

type taskFinder interface {
	FindTask(ctx context.Context, task oneofs.Task) (*taskstore.Task, error)
}

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type NewDocumentServiceParams struct {
	Agents    *agentstore.AgentStore
	Documents *documentstore.DocumentStore
	Types     *documenttypestore.DocumentTypeStore
	Events    eventRecorder
	Tasks     taskFinder
	Tx        txRunner
}

type DocumentService struct {
	agents    *agentstore.AgentStore
	documents *documentstore.DocumentStore
	types     *documenttypestore.DocumentTypeStore
	events    eventRecorder
	tasks     taskFinder
	tx        txRunner
}

// NewDocumentService builds the service over the agents that write documents, the store that
// keeps them, and the queue that records the event telling an author a person wrote its document.
func NewDocumentService(params NewDocumentServiceParams) *DocumentService {
	return &DocumentService{
		agents:    params.Agents,
		documents: params.Documents,
		types:     params.Types,
		events:    params.Events,
		tasks:     params.Tasks,
		tx:        params.Tx,
	}
}

type WriteDocumentParams struct {
	// AgentID is the agent writing, whose document it is, or empty for a person rewriting the
	// document ID names.
	AgentID uuid.UUID
	// For is the agent a person creates a document for, and empty for an agent writing its own.
	For uuid.UUID
	// ID is the document to rewrite, or nil to create one.
	ID *uuid.UUID
	// Version is the version a rewrite replaces, required with ID.
	Version *int32
	Name    string
	Type    string
	// Source is the body, or a file on this machine to read it from, resolved against the agent's
	// working directory when relative.
	Source oneofs.Source
}

// WriteDocument creates a document, or rewrites the one ID names.
func (s *DocumentService) WriteDocument(ctx context.Context, params WriteDocumentParams) (*documentstore.Document, error) {
	if params.AgentID != uuid.Nil && params.For != uuid.Nil && params.For != params.AgentID {
		return nil, ErrAgentWritingForAnother
	}

	if params.ID != nil && params.Version == nil {
		return nil, ErrNoVersion
	}

	byPerson := params.AgentID == uuid.Nil
	if byPerson {
		owner, err := s.ownerFor(ctx, params)
		if err != nil {
			return nil, err
		}
		params.AgentID = owner
	}

	agent, err := s.agents.GetByID(ctx, params.AgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to find the agent writing the document: %w", err)
	}

	if _, err := s.GetDocumentType(ctx, params.Type, nil); err != nil {
		return nil, err
	}

	body := xptr.Deref(params.Source.Body)
	if params.Source.Path != nil {
		body, err = readBody(agent.Cwd, *params.Source.Path)
		if err != nil {
			return nil, err
		}
	}
	if err := checkBody(body); err != nil {
		return nil, err
	}

	var written *documentstore.Document
	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		documents := documentstore.NewDocumentStore(tx)

		var err error
		if params.ID == nil {
			written, err = documents.Create(ctx, documentstore.CreateParams{
				AgentID:  agent.ID,
				Name:     params.Name,
				Type:     params.Type,
				Body:     body,
				ByPerson: byPerson,
			})
		} else {
			written, err = documents.Update(ctx, documentstore.UpdateParams{
				ID:       *params.ID,
				AgentID:  agent.ID,
				Name:     params.Name,
				Type:     params.Type,
				Body:     body,
				Version:  *params.Version,
				ByPerson: byPerson,
			})
		}
		if err != nil {
			return nil, err
		}
		if !byPerson {
			return nil, nil
		}

		event, err := events.NewEvent(personWrote{DocumentID: written.ID})
		if err != nil {
			return nil, err
		}

		return []river.JobArgs{*event}, nil
	})
	if err != nil {
		return nil, err
	}

	return written, nil
}

// ownerFor is the agent a person's write is to: the owner of the document it rewrites, or the
// agent it names for a new one.
func (s *DocumentService) ownerFor(ctx context.Context, params WriteDocumentParams) (uuid.UUID, error) {
	if params.ID == nil {
		if params.For == uuid.Nil {
			return uuid.Nil, ErrNoAuthor
		}

		return params.For, nil
	}

	held, err := s.documents.Get(ctx, *params.ID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to find the document to rewrite: %w", err)
	}
	if params.For != uuid.Nil && params.For != held.AgentID {
		return uuid.Nil, ErrForAnother
	}

	return held.AgentID, nil
}

func readBody(cwd, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrUnreadablePath, path)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrUnreadablePath, path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular file", ErrUnreadablePath, path)
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("%w: %s", ErrEmptyFile, path)
	}
	if info.Size() > MaxBodyBytes {
		return "", fmt.Errorf("%w: %s holds %d bytes", ErrTooLarge, path, info.Size())
	}

	content, err := io.ReadAll(io.LimitReader(file, MaxBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrUnreadablePath, path, err)
	}

	return string(content), nil
}

func checkBody(body string) error {
	if body == "" {
		return ErrNoBody
	}
	if len(body) > MaxBodyBytes {
		return fmt.Errorf("%w: it holds %d bytes", ErrTooLarge, len(body))
	}
	if !utf8.ValidString(body) {
		return ErrNotUTF8
	}

	return nil
}

// GetDocument returns one document whole, as it stands or as the write version names left it.
func (s *DocumentService) GetDocument(ctx context.Context, id uuid.UUID, version *int32) (*documentstore.Document, error) {
	got, err := s.documents.Get(ctx, id)
	if err != nil || version == nil {
		return got, err
	}

	written, err := s.documents.GetVersion(ctx, id, *version)
	if err != nil {
		return nil, err
	}

	got.Name, got.Type, got.Body = written.Name, written.Type, written.Body
	got.Version, got.UpdatedAt = written.Version, written.CreatedAt

	return got, nil
}

// DeleteDocumentsParams is the documents to delete and the agent asking.
type DeleteDocumentsParams struct {
	IDs []uuid.UUID
	// ByAgentID is the agent asking, empty for a person.
	ByAgentID uuid.UUID
}

// DeleteDocuments removes documents and every write of each, or none when any id holds no document.
func (s *DocumentService) DeleteDocuments(ctx context.Context, params DeleteDocumentsParams) error {
	if params.ByAgentID != uuid.Nil {
		return ErrDeleteByAgent
	}

	return s.tx.InTx(ctx, func(db store.DBTX) error {
		return documentstore.NewDocumentStore(db).Delete(ctx, params.IDs)
	})
}

// ListDocumentVersions returns every write of a document, the newest first, with the agent that owns it.
func (s *DocumentService) ListDocumentVersions(ctx context.Context, id uuid.UUID) (uuid.UUID, []documentstore.Version, error) {
	got, err := s.documents.Get(ctx, id)
	if err != nil {
		return uuid.Nil, nil, err
	}

	versions, err := s.documents.ListVersions(ctx, id)
	if err != nil {
		return uuid.Nil, nil, err
	}

	return got.AgentID, versions, nil
}

// ListDocumentsParams narrows a page of documents.
type ListDocumentsParams struct {
	AgentID *uuid.UUID
	Task    oneofs.Task
	Type    *string
	Limit   int32
	Cursor  *documentstore.Cursor
}

// ListDocuments returns a page of documents without their bodies, newest write first.
func (s *DocumentService) ListDocuments(ctx context.Context, params ListDocumentsParams) ([]documentstore.Document, error) {
	var taskID *uuid.UUID
	if params.Task != (oneofs.Task{}) {
		task, err := s.tasks.FindTask(ctx, params.Task)
		if err != nil {
			return nil, err
		}

		taskID = &task.ID
	}

	return s.documents.List(ctx, documentstore.ListParams{
		AgentID: params.AgentID,
		TaskID:  taskID,
		Type:    params.Type,
		Limit:   params.Limit,
		Cursor:  params.Cursor,
	})
}

// SearchDocuments finds the documents whose current body holds the words of a query.
func (s *DocumentService) SearchDocuments(ctx context.Context, params documentstore.SearchParams) (*documentstore.Results, error) {
	return s.documents.Search(ctx, params)
}
