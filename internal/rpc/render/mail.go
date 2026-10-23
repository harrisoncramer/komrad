package render

import (
	mailstore "komrad/internal/store/mail"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// Mail draws one piece of mail as the wire carries it.
func Mail(stored mailstore.Mail) *agentv1.Mail {
	return &agentv1.Mail{
		Id:          stored.ID.String(),
		FromAgentId: formatOptionalID(stored.FromAgentID),
		FromName:    stored.FromName,
		ToAgentId:   stored.ToAgentID.String(),
		Body:        stored.Body,
		ReplyTo:     formatOptionalID(stored.ReplyTo),
		CreatedAt:   dates.Stamp(stored.CreatedAt),
		ClaimedAt:   dates.StampPtr(stored.ClaimedAt),
		ExpiresAt:   dates.Stamp(stored.ExpiresAt),
		Kind:        stored.Kind,
	}
}
