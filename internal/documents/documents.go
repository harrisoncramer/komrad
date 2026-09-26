// Package documents owns the markdown documents agents write for people to read.
package documents

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"komrad/internal/fail"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
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
)

type NewDocumentServiceParams struct {
	Agents    *agentstore.AgentStore
	Documents *documentstore.DocumentStore
}

type DocumentService struct {
	agents    *agentstore.AgentStore
	documents *documentstore.DocumentStore
}

// NewDocumentService builds the service over the agents that write documents and the store that
// keeps them.
func NewDocumentService(params NewDocumentServiceParams) *DocumentService {
	return &DocumentService{agents: params.Agents, documents: params.Documents}
}

type WriteDocumentParams struct {
	// AgentID is the agent writing, whose document it is.
	AgentID string
	// ID is the document to rewrite, or nil to create one.
	ID   *string
	Name string
	Kind documentstore.Kind
	Body string
	// Path is a file on this machine to read the body from, resolved against the agent's
	// working directory when relative.
	Path string
}

// WriteDocument creates a document, or rewrites the one ID names.
func (s *DocumentService) WriteDocument(ctx context.Context, params WriteDocumentParams) (*documentstore.Document, error) {
	agent, err := s.agents.GetByID(ctx, params.AgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to find the agent writing the document: %w", err)
	}

	body := params.Body
	if params.Path != "" {
		body, err = readBody(agent.Cwd, params.Path)
		if err != nil {
			return nil, err
		}
	}
	if err := checkBody(body); err != nil {
		return nil, err
	}

	if params.ID == nil {
		return s.documents.Create(ctx, documentstore.CreateParams{
			AgentID: agent.ID,
			Name:    params.Name,
			Kind:    params.Kind,
			Body:    body,
		})
	}

	return s.documents.Update(ctx, documentstore.UpdateParams{
		ID:      *params.ID,
		AgentID: agent.ID,
		Name:    params.Name,
		Kind:    params.Kind,
		Body:    body,
	})
}

func readBody(cwd, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	file, err := os.Open(path)
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

// GetDocument returns one document whole.
func (s *DocumentService) GetDocument(ctx context.Context, id string) (*documentstore.Document, error) {
	return s.documents.Get(ctx, id)
}

// ListDocuments returns an agent's documents without their bodies, newest write first.
func (s *DocumentService) ListDocuments(ctx context.Context, agentID string, kind *documentstore.Kind) ([]documentstore.Document, error) {
	return s.documents.List(ctx, agentID, kind)
}
