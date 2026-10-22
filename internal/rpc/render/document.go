package render

import (
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// Document draws one document as the wire carries it.
func Document(stored documentstore.Document) *agentv1.Document {
	return &agentv1.Document{
		Id:        stored.ID.String(),
		AgentId:   stored.AgentID.String(),
		TaskId:    formatOptionalID(stored.TaskID),
		Name:      stored.Name,
		Type:      stored.Type,
		Body:      stored.Body,
		Version:   stored.Version,
		CreatedAt: dates.Stamp(stored.CreatedAt),
		UpdatedAt: dates.Stamp(stored.UpdatedAt),
	}
}

// DocumentVersion draws one write of a document as the wire carries it.
func DocumentVersion(stored documentstore.Version) *agentv1.DocumentVersion {
	return &agentv1.DocumentVersion{
		Version:   stored.Version,
		Name:      stored.Name,
		Type:      stored.Type,
		ByPerson:  stored.ByPerson,
		CreatedAt: dates.Stamp(stored.CreatedAt),
	}
}

// DocumentHit draws one document a search found as the wire carries it.
func DocumentHit(hit documentstore.SearchHit) *agentv1.DocumentHit {
	return &agentv1.DocumentHit{
		DocumentId: hit.ID.String(),
		AgentId:    hit.AgentID.String(),
		Name:       hit.Name,
		Snippet:    hit.Snippet,
	}
}

// DocumentType draws one document type as the wire carries it.
func DocumentType(stored documenttypestore.Type) *agentv1.DocumentType {
	return &agentv1.DocumentType{
		Id:        stored.ID.String(),
		Name:      stored.Name,
		Context:   stored.Context,
		Version:   stored.Version,
		UpdatedAt: dates.Stamp(stored.UpdatedAt),
	}
}

// DocumentTypeVersion draws one write of a document type, without its context.
func DocumentTypeVersion(stored documenttypestore.Version) *agentv1.DocumentTypeVersion {
	return &agentv1.DocumentTypeVersion{
		Version:   stored.Version,
		Name:      stored.Name,
		CreatedAt: dates.Stamp(stored.CreatedAt),
	}
}
