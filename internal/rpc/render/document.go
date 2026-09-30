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
		Id:        stored.ID,
		AgentId:   stored.AgentID,
		Name:      stored.Name,
		Type:      stored.Type,
		Body:      stored.Body,
		Version:   stored.Version,
		CreatedAt: dates.Stamp(stored.CreatedAt),
		UpdatedAt: dates.PreciseStamp(stored.UpdatedAt),
	}
}

// DocumentVersion draws one write of a document as the wire carries it.
func DocumentVersion(stored documentstore.Version) *agentv1.DocumentVersion {
	return &agentv1.DocumentVersion{
		Version:   stored.Version,
		Name:      stored.Name,
		Type:      stored.Type,
		ByPerson:  stored.ByPerson,
		CreatedAt: dates.PreciseStamp(stored.CreatedAt),
	}
}

// DocumentType draws one document type as the wire carries it.
func DocumentType(stored documenttypestore.Type) *agentv1.DocumentType {
	return &agentv1.DocumentType{
		Id:        stored.ID,
		Name:      stored.Name,
		Context:   stored.Context,
		Version:   stored.Version,
		UpdatedAt: dates.PreciseStamp(stored.UpdatedAt),
	}
}
