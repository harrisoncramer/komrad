package render

import (
	documentstore "komrad/internal/store/document"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// Document draws one document as the wire carries it.
func Document(stored documentstore.Document) *agentv1.Document {
	return &agentv1.Document{
		Id:        stored.ID,
		AgentId:   stored.AgentID,
		Name:      stored.Name,
		Kind:      DocumentKind(stored.Kind),
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
		Kind:      DocumentKind(stored.Kind),
		ByPerson:  stored.ByPerson,
		CreatedAt: dates.PreciseStamp(stored.CreatedAt),
	}
}

// DocumentKind is the wire's name for a kind of document.
func DocumentKind(kind documentstore.Kind) agentv1.DocumentKind {
	switch kind {
	case documentstore.KindRFC:
		return agentv1.DocumentKind_DOCUMENT_KIND_RFC
	case documentstore.KindReport:
		return agentv1.DocumentKind_DOCUMENT_KIND_REPORT
	case documentstore.KindExplainer:
		return agentv1.DocumentKind_DOCUMENT_KIND_EXPLAINER
	case documentstore.KindNotes:
		return agentv1.DocumentKind_DOCUMENT_KIND_NOTES
	}

	return agentv1.DocumentKind_DOCUMENT_KIND_UNSPECIFIED
}
