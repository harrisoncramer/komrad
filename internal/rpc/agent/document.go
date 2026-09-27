package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/documents"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	documentstore "komrad/internal/store/document"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

type documentReadWriter interface {
	WriteDocument(ctx context.Context, params documents.WriteDocumentParams) (*documentstore.Document, error)
	GetDocument(ctx context.Context, id string, version *int32) (*documentstore.Document, error)
	ListDocumentVersions(ctx context.Context, id string) (string, []documentstore.Version, error)
	ListDocuments(ctx context.Context, agentID string, kind *documentstore.Kind) ([]documentstore.Document, error)
}

var errUnknownDocumentKind = fail.InvalidArgument("unknown document kind")

// WriteDocument creates or rewrites one of the calling agent's documents, or lets a caller that
// is not an agent rewrite any one.
func (s *Handler) WriteDocument(ctx context.Context, req *agentv1.WriteDocumentRequest) (*agentv1.WriteDocumentResponse, *fail.Error) {
	kind, failure := documentKindOf(req.GetKind())
	if failure != nil {
		return nil, failure
	}

	written, err := s.documents.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: caller.From(ctx).AgentID,
		ID:      req.Id,
		Version: req.Version,
		Name:    req.GetName(),
		Kind:    kind,
		Body:    req.GetBody(),
		Path:    req.GetPath(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to write the document")
	}

	written.Body = ""

	return &agentv1.WriteDocumentResponse{Document: render.Document(*written)}, nil
}

// ListDocuments returns an agent's documents without their bodies.
func (s *Handler) ListDocuments(ctx context.Context, req *agentv1.ListDocumentsRequest) (*agentv1.ListDocumentsResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetAgentId())
	if failure != nil {
		return nil, failure
	}

	var kind *documentstore.Kind
	if req.Kind != nil {
		named, failure := documentKindOf(req.GetKind())
		if failure != nil {
			return nil, failure
		}
		kind = &named
	}

	logCrossAgentRead(ctx, id, "agent listed another agent's documents")

	listed, err := s.documents.ListDocuments(ctx, id, kind)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list documents")
	}

	return &agentv1.ListDocumentsResponse{Documents: xslice.Map(listed, render.Document)}, nil
}

// GetDocument returns one document whole.
func (s *Handler) GetDocument(ctx context.Context, req *agentv1.GetDocumentRequest) (*agentv1.GetDocumentResponse, *fail.Error) {
	got, err := s.documents.GetDocument(ctx, req.GetId(), req.Version)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the document")
	}

	logCrossAgentRead(ctx, got.AgentID, "agent read another agent's document")

	return &agentv1.GetDocumentResponse{Document: render.Document(*got)}, nil
}

// ListDocumentVersions returns every write of one document without their bodies.
func (s *Handler) ListDocumentVersions(ctx context.Context, req *agentv1.ListDocumentVersionsRequest) (*agentv1.ListDocumentVersionsResponse, *fail.Error) {
	agentID, versions, err := s.documents.ListDocumentVersions(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the document's versions")
	}

	logCrossAgentRead(ctx, agentID, "agent listed the versions of another agent's document")

	return &agentv1.ListDocumentVersionsResponse{Versions: xslice.Map(versions, render.DocumentVersion)}, nil
}

func documentKindOf(kind agentv1.DocumentKind) (documentstore.Kind, *fail.Error) {
	switch kind {
	case agentv1.DocumentKind_DOCUMENT_KIND_RFC:
		return documentstore.KindRFC, nil
	case agentv1.DocumentKind_DOCUMENT_KIND_REPORT:
		return documentstore.KindReport, nil
	case agentv1.DocumentKind_DOCUMENT_KIND_EXPLAINER:
		return documentstore.KindExplainer, nil
	case agentv1.DocumentKind_DOCUMENT_KIND_NOTES:
		return documentstore.KindNotes, nil
	case agentv1.DocumentKind_DOCUMENT_KIND_UNSPECIFIED:
	}

	return "", errUnknownDocumentKind
}
