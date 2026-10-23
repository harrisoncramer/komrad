package pullrequest

import (
	"fmt"

	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// fromServiceToDBCheckLogState is the stored text of a check log state.
func fromServiceToDBCheckLogState(state pullrequestv1.CheckLogState) (string, error) {
	switch state {
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE:
		return "none", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING:
		return "pending", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED:
		return "fetched", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE:
		return "too_large", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING:
		return "missing", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED:
		return "expired", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED:
		return "failed", nil
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_UNSPECIFIED:
	}

	return "", fmt.Errorf("a check log state %v has no stored text", state)
}

// fromDBToServiceCheckLogState is the check log state stored text names.
func fromDBToServiceCheckLogState(state string) (pullrequestv1.CheckLogState, error) {
	switch state {
	case "none":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE, nil
	case "pending":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING, nil
	case "fetched":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED, nil
	case "too_large":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE, nil
	case "missing":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING, nil
	case "expired":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED, nil
	case "failed":
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED, nil
	}

	return pullrequestv1.CheckLogState_CHECK_LOG_STATE_UNSPECIFIED, fmt.Errorf("stored check log state %q has no service value", state)
}

// fromServiceToDBStatusCheckKind is the stored text of a status check kind.
func fromServiceToDBStatusCheckKind(kind pullrequestv1.StatusCheckKind) (string, error) {
	switch kind {
	case pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN:
		return "check_run", nil
	case pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_STATUS:
		return "status", nil
	case pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE:
		return "pipeline", nil
	case pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_UNSPECIFIED:
	}

	return "", fmt.Errorf("a status check kind %v has no stored text", kind)
}

// fromDBToServiceStatusCheckKind is the status check kind stored text names.
func fromDBToServiceStatusCheckKind(kind string) (pullrequestv1.StatusCheckKind, error) {
	switch kind {
	case "check_run":
		return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN, nil
	case "status":
		return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_STATUS, nil
	case "pipeline":
		return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE, nil
	}

	return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_UNSPECIFIED, fmt.Errorf("stored status check kind %q has no service value", kind)
}

// fromServiceToDBStatusCheckState is the stored text of a status check state.
func fromServiceToDBStatusCheckState(state pullrequestv1.StatusCheckState) (string, error) {
	switch state {
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED:
		return "queued", nil
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING:
		return "running", nil
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_WAITING:
		return "waiting", nil
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED:
		return "completed", nil
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN:
		return "unknown", nil
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNSPECIFIED:
	}

	return "", fmt.Errorf("a status check state %v has no stored text", state)
}

// fromDBToServiceStatusCheckState is the status check state stored text names.
func fromDBToServiceStatusCheckState(state string) (pullrequestv1.StatusCheckState, error) {
	switch state {
	case "queued":
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED, nil
	case "running":
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING, nil
	case "waiting":
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_WAITING, nil
	case "completed":
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, nil
	case "unknown":
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN, nil
	}

	return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNSPECIFIED, fmt.Errorf("stored status check state %q has no service value", state)
}

// fromServiceToDBReviewDecision is the stored text of a review decision.
func fromServiceToDBReviewDecision(decision pullrequestv1.ReviewDecision) (string, error) {
	switch decision {
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING:
		return "pending", nil
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED:
		return "approved", nil
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED:
		return "changes_requested", nil
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED:
		return "rereview_requested", nil
	case pullrequestv1.ReviewDecision_REVIEW_DECISION_UNSPECIFIED, pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE:
	}

	return "", fmt.Errorf("a review decision %v has no stored text", decision)
}

// fromDBToServiceReviewDecision is the review decision stored text names.
func fromDBToServiceReviewDecision(decision string) (pullrequestv1.ReviewDecision, error) {
	switch decision {
	case "pending":
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, nil
	case "approved":
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED, nil
	case "changes_requested":
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED, nil
	case "rereview_requested":
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED, nil
	}

	return pullrequestv1.ReviewDecision_REVIEW_DECISION_UNSPECIFIED, fmt.Errorf("stored review decision %q has no service value", decision)
}

// fromServiceToDBMergeability is the stored text of a mergeability.
func fromServiceToDBMergeability(mergeability pullrequestv1.Mergeability) (string, error) {
	switch mergeability {
	case pullrequestv1.Mergeability_MERGEABILITY_CLEAN:
		return "clean", nil
	case pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE:
		return "unstable", nil
	case pullrequestv1.Mergeability_MERGEABILITY_BLOCKED:
		return "blocked", nil
	case pullrequestv1.Mergeability_MERGEABILITY_BEHIND:
		return "behind", nil
	case pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING:
		return "conflicting", nil
	case pullrequestv1.Mergeability_MERGEABILITY_UNSPECIFIED, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN:
	}

	return "", fmt.Errorf("a mergeability %v has no stored text", mergeability)
}

// fromDBToServiceMergeability is the mergeability stored text names.
func fromDBToServiceMergeability(mergeability string) (pullrequestv1.Mergeability, error) {
	switch mergeability {
	case "clean":
		return pullrequestv1.Mergeability_MERGEABILITY_CLEAN, nil
	case "unstable":
		return pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE, nil
	case "blocked":
		return pullrequestv1.Mergeability_MERGEABILITY_BLOCKED, nil
	case "behind":
		return pullrequestv1.Mergeability_MERGEABILITY_BEHIND, nil
	case "conflicting":
		return pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING, nil
	}

	return pullrequestv1.Mergeability_MERGEABILITY_UNSPECIFIED, fmt.Errorf("stored mergeability %q has no service value", mergeability)
}
