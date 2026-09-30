package render

import (
	"komrad/lib/dates"
	"komrad/lib/xslice"

	pullrequeststore "komrad/internal/store/pullrequest"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// ForgeThread draws a forge review thread with its comments.
func ForgeThread(stored pullrequeststore.ForgeThread) *pullrequestv1.ForgeThread {
	return &pullrequestv1.ForgeThread{
		Id:            stored.ID,
		PullRequestId: stored.PullRequestID,
		Path:          stored.Path,
		Line:          int32(stored.Line),
		Url:           stored.URL,
		Resolved:      stored.Resolved,
		WrittenAt:     dates.Stamp(stored.WrittenAt),
		Comments: xslice.Map(stored.Comments, func(comment pullrequeststore.ForgeComment) *pullrequestv1.ForgeComment {
			return &pullrequestv1.ForgeComment{
				Id:          comment.ID,
				AuthorLogin: comment.AuthorLogin,
				Bot:         comment.AuthorIsBot,
				Yours:       comment.Yours,
				Body:        comment.Body,
				WrittenAt:   dates.Stamp(comment.WrittenAt),
				EditedAt:    dates.StampPtr(comment.EditedAt),
			}
		}),
	}
}
