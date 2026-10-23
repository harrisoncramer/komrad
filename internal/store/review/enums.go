package review

import (
	"fmt"

	"komrad/db/models"
	agentv1 "komrad/proto/gen/agent/v1"
)

// fromServiceToDBReviewAuthorKind is the stored value of a review author kind.
func fromServiceToDBReviewAuthorKind(kind agentv1.ReviewAuthorKind) (models.ReviewAuthorKind, error) {
	switch kind {
	case agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN:
		return models.ReviewAuthorKindHuman, nil
	case agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT:
		return models.ReviewAuthorKindAgent, nil
	case agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE:
		return models.ReviewAuthorKindForge, nil
	case agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_UNSPECIFIED:
	}

	return "", fmt.Errorf("a review author kind %v has no stored value", kind)
}

// fromDBToServiceReviewAuthorKind is the review author kind a stored value names.
func fromDBToServiceReviewAuthorKind(kind models.ReviewAuthorKind) (agentv1.ReviewAuthorKind, error) {
	switch kind {
	case models.ReviewAuthorKindHuman:
		return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN, nil
	case models.ReviewAuthorKindAgent:
		return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT, nil
	case models.ReviewAuthorKindForge:
		return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE, nil
	}

	return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_UNSPECIFIED, fmt.Errorf("stored review author kind %q has no service value", kind)
}

// fromDBToServiceForgeReplyState is the forge reply state a stored value names.
func fromDBToServiceForgeReplyState(state models.ForgeReplyState) (agentv1.ForgeReplyState, error) {
	switch state {
	case models.ForgeReplyStateApproved:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED, nil
	case models.ForgeReplyStateSending:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING, nil
	case models.ForgeReplyStateSent:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT, nil
	case models.ForgeReplyStateFailed:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED, nil
	case models.ForgeReplyStateDraft:
	}

	return agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED, fmt.Errorf("stored forge reply state %q has no service value", state)
}

// fromServiceToDBReviewVerdict is the stored value of a review verdict.
func fromServiceToDBReviewVerdict(verdict agentv1.ReviewVerdict) (models.ReviewVerdict, error) {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return models.ReviewVerdictApproved, nil
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return models.ReviewVerdictChangesRequested, nil
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED:
		return models.ReviewVerdictCommented, nil
	case agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return "", fmt.Errorf("a review verdict %v has no stored value", verdict)
}

// fromDBToServiceReviewVerdict is the review verdict a stored value names.
func fromDBToServiceReviewVerdict(verdict models.ReviewVerdict) (agentv1.ReviewVerdict, error) {
	switch verdict {
	case models.ReviewVerdictApproved:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, nil
	case models.ReviewVerdictChangesRequested:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, nil
	case models.ReviewVerdictCommented:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, nil
	}

	return agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED, fmt.Errorf("stored review verdict %q has no service value", verdict)
}

// fromServiceToDBNullReviewAuthorKind is the stored value of a review author kind, null for an unspecified one.
func fromServiceToDBNullReviewAuthorKind(kind agentv1.ReviewAuthorKind) (models.NullReviewAuthorKind, error) {
	if kind == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_UNSPECIFIED {
		return models.NullReviewAuthorKind{}, nil
	}

	stored, err := fromServiceToDBReviewAuthorKind(kind)
	if err != nil {
		return models.NullReviewAuthorKind{}, err
	}

	return models.NullReviewAuthorKind{ReviewAuthorKind: stored, Valid: true}, nil
}

// fromDBToServiceNullReviewAuthorKind is the review author kind a stored value names, unspecified for a null one.
func fromDBToServiceNullReviewAuthorKind(kind models.NullReviewAuthorKind) (agentv1.ReviewAuthorKind, error) {
	if !kind.Valid {
		return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_UNSPECIFIED, nil
	}

	return fromDBToServiceReviewAuthorKind(kind.ReviewAuthorKind)
}

// fromServiceToDBNullReviewVerdict is the stored value of a review verdict, null for an unspecified one.
func fromServiceToDBNullReviewVerdict(verdict agentv1.ReviewVerdict) (models.NullReviewVerdict, error) {
	if verdict == agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED {
		return models.NullReviewVerdict{}, nil
	}

	stored, err := fromServiceToDBReviewVerdict(verdict)
	if err != nil {
		return models.NullReviewVerdict{}, err
	}

	return models.NullReviewVerdict{ReviewVerdict: stored, Valid: true}, nil
}

// fromDBToServiceNullReviewVerdict is the review verdict a stored value names, unspecified for a null one.
func fromDBToServiceNullReviewVerdict(verdict models.NullReviewVerdict) (agentv1.ReviewVerdict, error) {
	if !verdict.Valid {
		return agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED, nil
	}

	return fromDBToServiceReviewVerdict(verdict.ReviewVerdict)
}
