package render

import (
	"komrad/config"
	"komrad/internal/forge"
	"komrad/lib/dates"
	"komrad/lib/xslice"

	pullrequeststore "komrad/internal/store/pullrequest"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// ForgeThread draws a forge review thread, marking its bots and the person's own comments by the forge config.
func ForgeThread(stored pullrequeststore.ForgeThread, identity config.Forge) *pullrequestv1.ForgeThread {
	return &pullrequestv1.ForgeThread{
		Id:            stored.ID,
		PullRequestId: stored.PullRequestID,
		Path:          stored.Path,
		Side:          forgeSideOf(stored.Side),
		CommitSha:     stored.CommitSHA,
		BaseSha:       stored.BaseSHA,
		StartLine:     int32(stored.StartLine),
		EndLine:       int32(stored.EndLine),
		QuotedLines:   stored.QuotedLines,
		Resolved:      stored.Resolved,
		Outdated:      stored.Outdated,
		WrittenAt:     dates.Stamp(stored.WrittenAt),
		Comments: xslice.Map(stored.Comments, func(comment pullrequeststore.ForgeComment) *pullrequestv1.ForgeComment {
			return &pullrequestv1.ForgeComment{
				Id:          comment.ID,
				AuthorLogin: comment.AuthorLogin,
				Bot:         comment.AuthorIsBot || identity.IsBot(comment.AuthorLogin),
				Yours:       identity.IsYours(comment.AuthorLogin),
				Body:        comment.Body,
				WrittenAt:   dates.Stamp(comment.WrittenAt),
				EditedAt:    dates.StampPtr(comment.EditedAt),
			}
		}),
	}
}

func forgeSideOf(side forge.Side) pullrequestv1.ForgeSide {
	switch side {
	case forge.SideOld:
		return pullrequestv1.ForgeSide_FORGE_SIDE_OLD
	case forge.SideNew:
		return pullrequestv1.ForgeSide_FORGE_SIDE_NEW
	case forge.SideNone:
	}

	return pullrequestv1.ForgeSide_FORGE_SIDE_UNSPECIFIED
}
