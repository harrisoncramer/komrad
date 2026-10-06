package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/documents"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

type documentReadWriter interface {
	WriteDocument(ctx context.Context, params documents.WriteDocumentParams) (*documentstore.Document, error)
	GetDocument(ctx context.Context, id string, version *int32) (*documentstore.Document, error)
	ListDocumentVersions(ctx context.Context, id string) (string, []documentstore.Version, error)
	ListDocuments(ctx context.Context, agentID *string, typeName *string) ([]documentstore.Document, error)
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
func (s *Handler) WriteDocument(ctx context.Context, req *agentv1.WriteDocumentRequest) (*agentv1.WriteDocumentResponse, *fail.Error) {
	written, err := s.documents.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: caller.From(ctx).AgentID,
		For:     req.GetAgentId(),
		ID:      req.Id,
		Version: req.Version,
		Name:    req.GetName(),
		Type:    req.GetType(),
		Body:    req.GetBody(),
		Path:    req.GetPath(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to write the document")
	}

	written.Body = ""

	return &agentv1.WriteDocumentResponse{Document: render.Document(*written)}, nil
}

// errEveryAgentWithAgentID is a listing that sets every_agent and names an agent too.
var errEveryAgentWithAgentID = fail.InvalidArgument("name an agent or ask for every agent, not both")

// ListDocuments returns one agent's documents, or every agent's, without their bodies.
func (s *Handler) ListDocuments(ctx context.Context, req *agentv1.ListDocumentsRequest) (*agentv1.ListDocumentsResponse, *fail.Error) {
	var owner *string
	if req.GetEveryAgent() {
		if req.AgentId != nil {
			return nil, errEveryAgentWithAgentID
		}
	} else {
		id, failure := scopedAgentID(ctx, req.GetAgentId())
		if failure != nil {
			return nil, failure
		}

		logCrossAgentRead(ctx, id, "agent listed another agent's documents")
		owner = &id
	}

	listed, err := s.documents.ListDocuments(ctx, owner, req.Type)
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
func (s *Handler) ListDocumentTypeVersions(ctx context.Context, req *agentv1.ListDocumentTypeVersionsRequest) (*agentv1.ListDocumentTypeVersionsResponse, *fail.Error) {
	versions, err := s.documentTypes.ListDocumentTypeVersions(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the document type's versions")
	}

	return &agentv1.ListDocumentTypeVersionsResponse{Versions: xslice.Map(versions, render.DocumentTypeVersion)}, nil
}

// WriteDocumentType creates a document type, or renames or rewrites one.
func (s *Handler) WriteDocumentType(ctx context.Context, req *agentv1.WriteDocumentTypeRequest) (*agentv1.WriteDocumentTypeResponse, *fail.Error) {
	written, err := s.documentTypes.WriteDocumentType(ctx, documents.WriteDocumentTypeParams{
		ID:        req.Id,
		Version:   req.Version,
		Name:      req.GetName(),
		Context:   req.GetContext(),
		ByAgentID: caller.From(ctx).AgentID,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to write the document type")
	}

	return &agentv1.WriteDocumentTypeResponse{DocumentType: render.DocumentType(*written)}, nil
}

// DeleteDocument removes a document and every write of it.
func (s *Handler) DeleteDocument(ctx context.Context, req *agentv1.DeleteDocumentRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.documents.DeleteDocument(ctx, documents.DeleteDocumentParams{
		ID:        req.GetId(),
		ByAgentID: caller.From(ctx).AgentID,
	}); err != nil {
		return nil, rpcerr.From(err, "failed to delete the document")
	}

	return &emptypb.Empty{}, nil
}

// DeleteDocumentType removes a document type no document is written as.
func (s *Handler) DeleteDocumentType(ctx context.Context, req *agentv1.DeleteDocumentTypeRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.documentTypes.DeleteDocumentType(ctx, documents.DeleteDocumentTypeParams{
		ID:        req.GetId(),
		ByAgentID: caller.From(ctx).AgentID,
	}); err != nil {
		return nil, rpcerr.From(err, "failed to delete the document type")
	}

	return &emptypb.Empty{}, nil
}
