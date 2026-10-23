package render

import (
	"komrad/internal/forge"
	"komrad/internal/pullrequests"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/dates"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// PullRequest draws one stored pull request as the wire carries it.
func PullRequest(stored pullrequeststore.PullRequest) *pullrequestv1.PullRequest {
	return &pullrequestv1.PullRequest{
		Id:                stored.ID.String(),
		Forge:             stored.Forge,
		ProviderId:        stored.ProviderID,
		Url:               stored.URL,
		RepositoryUrl:     stored.RepositoryURL,
		HeadRepositoryUrl: stored.HeadRepositoryURL,
		Number:            int32(stored.Number),
		Title:             stored.Title,
		Author:            stored.Author,
		Body:              stored.Body,
		HeadBranch:        stored.HeadBranch,
		HeadSha:           stored.HeadSHA,
		BaseBranch:        stored.BaseBranch,
		BaseSha:           stored.BaseSHA,
		Draft:             stored.Draft,
		State:             pullrequests.StateOf(stored),
		OpenedAt:          dates.Stamp(stored.OpenedAt),
		ChangedAt:         dates.Stamp(stored.ChangedAt),
		MergedAt:          dates.StampPtr(stored.MergedAt),
		ClosedAt:          dates.StampPtr(stored.ClosedAt),
		MergeReadiness:    mergeReadiness(stored.MergeReadiness.DisplayForHead(stored.HeadSHA)),
	}
}

// mergeReadiness draws what komrad shows of a pull request's merge readiness as the wire carries it.
func mergeReadiness(displayed forge.DisplayedMergeReadiness) *pullrequestv1.MergeReadiness {
	return &pullrequestv1.MergeReadiness{
		Decision:                displayed.Decision.Value,
		Checks:                  displayed.Checks.Value,
		Mergeability:            displayed.Mergeability.Value,
		MergeabilityUnconfirmed: displayed.Mergeability.IsUnconfirmed,
		AutoMerge:               displayed.AutoMerge.On,
		AutoMergeable:           displayed.AutoMerge.Allowed,
	}
}

// Repository draws a repository the sync has read as the wire carries it.
func Repository(sync pullrequeststore.Sync) *pullrequestv1.Repository {
	return &pullrequestv1.Repository{Url: sync.RepositoryURL}
}
