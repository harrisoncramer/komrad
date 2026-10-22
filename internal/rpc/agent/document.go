package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/documents"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/rpcpage"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	"google.golang.org/protobuf/types/known/emptypb"
)

type documentReadWriter interface {
	WriteDocument(ctx context.Context, params documents.WriteDocumentParams) (*documentstore.Document, error)
	GetDocument(ctx context.Context, id string, version *int32) (*documentstore.Document, error)
	ListDocumentVersions(ctx context.Context, id string) (string, []documentstore.Version, error)
	ListDocuments(ctx context.Context, params documents.ListDocumentsParams) ([]documentstore.Document, error)
	SearchDocuments(ctx context.Context, params documentstore.SearchParams) (*documentstore.Results, error)
	DeleteDocument(ctx context.Context, params documents.DeleteDocumentParams) error
}

type documentTypeKeeper interface {
	ListDocumentTypes(ctx context.Context) ([]documenttypestore.Type, error)
	GetDocumentType(ctx context.Context, name string, version *int32) (*documenttypestore.Type, error)
	ListDocumentTypeVersions(ctx context.Context, id string) ([]documenttypestore.Version, error)
	WriteDocumentType(ctx context.Context, params documents.WriteDocumentTypeParams) (*documenttypestore.Type, error)
	DeleteDocumentType(ctx context.Context, params documents.DeleteDocumentTypeParams) error
}

// WriteDocument creates or rewrites one of the calling agent's documents, or lets a caller that
// is not an agent rewrite any one or create one for the agent it names.
func (s *Handler) WriteDocument(ctx context.Context, req *agentv1komrad.WriteDocumentRequest) (*agentv1.WriteDocumentResponse, *fail.Error) {
	written, err := s.documents.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: caller.From(ctx).AgentID,
		For:     xptr.Deref(formatOptionalID(req.AgentId)),
		ID:      formatOptionalID(req.Id),
		Version: req.Version,
		Name:    req.Name,
		Type:    req.Type,
		Source:  req.Source,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to write the document")
	}

	written.Body = ""

	return &agentv1.WriteDocumentResponse{Document: render.Document(*written)}, nil
}

// ListDocuments returns a page of documents without their bodies, narrowed to an agent or task
// when one is named.
func (s *Handler) ListDocuments(ctx context.Context, req *agentv1komrad.ListDocumentsRequest) (*agentv1.ListDocumentsResponse, *fail.Error) {
	if req.AgentId != nil {
		logCrossAgentRead(ctx, req.AgentId.String(), "agent listed another agent's documents")
	}

	page, failure := rpcpage.List(req.Pagination, func(limit int64, cursor *documentstore.Cursor) ([]documentPage, *fail.Error) {
		listed, err := s.documents.ListDocuments(ctx, documents.ListDocumentsParams{
			AgentID: formatOptionalID(req.AgentId),
			Task:    req.Task,
			Type:    req.Type,
			Limit:   int32(limit),
			Cursor:  cursor,
		})
		if err != nil {
			return nil, rpcerr.From(err, "failed to list documents")
		}

		return xslice.Map(listed, renderDocumentPage), nil
	})
	if failure != nil {
		return nil, failure
	}

	response := &agentv1.ListDocumentsResponse{NextPageToken: page.Next}
	response.Documents = xslice.Map(page.Resources, func(resource documentPage) *agentv1.Document { return resource.Document })

	return response, nil
}

// SearchDocuments finds the documents whose current body holds the words of a query, every
// agent's unless one is named.
func (s *Handler) SearchDocuments(ctx context.Context, req *agentv1komrad.SearchDocumentsRequest) (*agentv1.SearchDocumentsResponse, *fail.Error) {
	if req.AgentId != nil {
		logCrossAgentRead(ctx, req.AgentId.String(), "agent searched another agent's documents")
	}

	results, err := s.documents.SearchDocuments(ctx, documentstore.SearchParams{
		Query:   req.Query,
		AgentID: formatOptionalID(req.AgentId),
		Type:    req.Type,
		Limit:   req.Limit,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to search documents")
	}

	return &agentv1.SearchDocumentsResponse{
		Matches: xslice.Map(results.Matches, render.DocumentHit),
		Near:    xslice.Map(results.Near, render.DocumentHit),
	}, nil
}

// GetDocument returns one document whole.
func (s *Handler) GetDocument(ctx context.Context, req *agentv1komrad.GetDocumentRequest) (*agentv1.GetDocumentResponse, *fail.Error) {
	got, err := s.documents.GetDocument(ctx, req.Id.String(), req.Version)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the document")
	}

	logCrossAgentRead(ctx, got.AgentID, "agent read another agent's document")

	return &agentv1.GetDocumentResponse{Document: render.Document(*got)}, nil
}

// ListDocumentVersions returns every write of one document without their bodies.
func (s *Handler) ListDocumentVersions(ctx context.Context, req *agentv1komrad.ListDocumentVersionsRequest) (*agentv1.ListDocumentVersionsResponse, *fail.Error) {
	agentID, versions, err := s.documents.ListDocumentVersions(ctx, req.Id.String())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the document's versions")
	}

	logCrossAgentRead(ctx, agentID, "agent listed the versions of another agent's document")

	return &agentv1.ListDocumentVersionsResponse{Versions: xslice.Map(versions, render.DocumentVersion)}, nil
}

// ListDocumentTypes returns every document type with its context.
func (s *Handler) ListDocumentTypes(ctx context.Context, _ *emptypb.Empty) (*agentv1.ListDocumentTypesResponse, *fail.Error) {
	types, err := s.documentTypes.ListDocumentTypes(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list document types")
	}

	return &agentv1.ListDocumentTypesResponse{DocumentTypes: xslice.Map(types, render.DocumentType)}, nil
}

// GetDocumentType returns one document type by name, or as an earlier write left it.
func (s *Handler) GetDocumentType(ctx context.Context, req *agentv1.GetDocumentTypeRequest) (*agentv1.GetDocumentTypeResponse, *fail.Error) {
	got, err := s.documentTypes.GetDocumentType(ctx, req.GetName(), req.Version)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the document type")
	}

	return &agentv1.GetDocumentTypeResponse{DocumentType: render.DocumentType(*got)}, nil
}

// ListDocumentTypeVersions returns every write of a document type, the newest first.
func (s *Handler) ListDocumentTypeVersions(ctx context.Context, req *agentv1komrad.ListDocumentTypeVersionsRequest) (*agentv1.ListDocumentTypeVersionsResponse, *fail.Error) {
	versions, err := s.documentTypes.ListDocumentTypeVersions(ctx, req.Id.String())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the document type's versions")
	}

	return &agentv1.ListDocumentTypeVersionsResponse{Versions: xslice.Map(versions, render.DocumentTypeVersion)}, nil
}

// WriteDocumentType creates a document type, or renames or rewrites one.
func (s *Handler) WriteDocumentType(ctx context.Context, req *agentv1komrad.WriteDocumentTypeRequest) (*agentv1.WriteDocumentTypeResponse, *fail.Error) {
	written, err := s.documentTypes.WriteDocumentType(ctx, documents.WriteDocumentTypeParams{
		ID:        formatOptionalID(req.Id),
		Version:   req.Version,
		Name:      req.Name,
		Context:   req.Context,
		ByAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to write the document type")
	}

	return &agentv1.WriteDocumentTypeResponse{DocumentType: render.DocumentType(*written)}, nil
}

// DeleteDocument removes a document and every write of it.
func (s *Handler) DeleteDocument(ctx context.Context, req *agentv1komrad.DeleteDocumentRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.documents.DeleteDocument(ctx, documents.DeleteDocumentParams{
		ID:        req.Id.String(),
		ByAgentID: caller.From(ctx).AgentID,
	}); err != nil {
		return nil, rpcerr.From(err, "failed to delete the document")
	}

	return &emptypb.Empty{}, nil
}

// DeleteDocumentType removes a document type that no document uses.
func (s *Handler) DeleteDocumentType(ctx context.Context, req *agentv1komrad.DeleteDocumentTypeRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.documentTypes.DeleteDocumentType(ctx, documents.DeleteDocumentTypeParams{
		ID:        req.Id.String(),
		ByAgentID: caller.From(ctx).AgentID,
	}); err != nil {
		return nil, rpcerr.From(err, "failed to delete the document type")
	}

	return &emptypb.Empty{}, nil
}
