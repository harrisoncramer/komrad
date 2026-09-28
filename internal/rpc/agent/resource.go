package agent

import (
	"komrad/internal/rpc/render"

	mailstore "komrad/internal/store/mail"
	transcriptstore "komrad/internal/store/transcript"
	agentv1 "komrad/proto/gen/agent/v1"
	viewv1 "komrad/proto/gen/view/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// agentViewOptions is how much of an agent the view a request asked for draws.
func agentViewOptions(view viewv1.View) render.AgentOptions {
	if view == viewv1.View_VIEW_FULL {
		return render.AgentOptions{}
	}

	return render.AgentOptions{PromptHead: render.SummaryHead}
}

type mailPage struct {
	*agentv1.Mail
	cursor mailstore.Cursor
}

// Cursor returns the keyset cursor locating this mail.
func (m mailPage) Cursor() (mailstore.Cursor, error) {
	return m.cursor, nil
}

func renderMailPage(stored mailstore.Mail) mailPage {
	return mailPage{Mail: render.Mail(stored), cursor: mailstore.Cursor{ID: stored.ID}}
}

// searchPage lets the paginator decode a search cursor, which locates a page of both
// sections rather than a single hit the way every other listing's cursor does.
type searchPage struct {
	cursor transcriptstore.Cursor
}

// Cursor returns the keyset cursor locating this page.
func (p searchPage) Cursor() (transcriptstore.Cursor, error) {
	return p.cursor, nil
}

func renderSearchHit(hit transcriptstore.SearchHit) *agentv1.SearchHit {
	rendered := &agentv1.SearchHit{
		AgentId:       hit.AgentID,
		Name:          hit.Name,
		Archived:      hit.Archived,
		Snippet:       hit.Snippet,
		Score:         hit.Rank,
		Seq:           hit.Seq,
		Role:          hit.Role,
		ContentOffset: hit.ContentOffset,
		ContentLength: hit.ContentLength,
	}
	if hit.SpokenAt != nil {
		rendered.SpokenAt = timestamppb.New(*hit.SpokenAt)
	}

	return rendered
}
