package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/internal/assemble"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	watchv1 "komrad/proto/gen/watch/v1"

	tea "charm.land/bubbletea/v2"
)

// offerNoticeReactor says in the header when someone other than the reader comments on the forge
// on a pull request an agent is linked to.
type offerNoticeReactor struct {
	// seen is every forge comment already accounted for, so each is announced once.
	seen map[string]bool
}

func newOfferNoticeReactor() offerNoticeReactor {
	return offerNoticeReactor{seen: map[string]bool{}}
}

func (o offerNoticeReactor) react(m *model, changed assemble.Changed) tea.Cmd {
	world := m.listing.world
	if changed.Replaced {
		for _, thread := range world.ForgeThreads {
			o.markSeen(thread)
		}

		return nil
	}

	touched := changed.Keys[watchv1.Resource_RESOURCE_FORGE_THREAD]
	if len(touched) == 0 {
		return nil
	}

	for _, agentID := range sortedAgentIDs(world) {
		var authors []string
		count := 0
		for _, offer := range assemble.Offers(world, agentID) {
			if offer.GetDismissed() {
				continue
			}
			if !slices.Contains(touched, offer.GetId()) {
				continue
			}
			for _, comment := range o.takeUnseen(offer) {
				count++
				if !slices.Contains(authors, comment.GetAuthorLogin()) {
					authors = append(authors, comment.GetAuthorLogin())
				}
			}
		}
		if count == 0 {
			continue
		}

		m.pushMessage(message{
			kind:    messageInfo,
			summary: describeOfferNotice(count, world.Agents[agentID].GetName(), authors),
		})
	}

	for _, key := range touched {
		if thread, ok := world.ForgeThreads[key]; ok {
			o.markSeen(thread)
		}
	}

	return nil
}

func (o offerNoticeReactor) markSeen(thread *pullrequestv1.ForgeThread) {
	for _, comment := range thread.GetComments() {
		o.seen[comment.GetId()] = true
	}
}

// takeUnseen is the comments on a thread not yet announced that a person other than the reader
// wrote, marking every comment on it seen.
func (o offerNoticeReactor) takeUnseen(thread *pullrequestv1.ForgeThread) []*pullrequestv1.ForgeComment {
	var fresh []*pullrequestv1.ForgeComment
	for _, comment := range thread.GetComments() {
		if o.seen[comment.GetId()] {
			continue
		}
		o.seen[comment.GetId()] = true
		if comment.GetBot() || comment.GetYours() {
			continue
		}
		fresh = append(fresh, comment)
	}

	return fresh
}

func sortedAgentIDs(world assemble.World) []string {
	ids := make([]string, 0, len(world.Agents))
	for id := range world.Agents {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	return ids
}

func describeOfferNotice(count int, agentName string, authors []string) string {
	return fmt.Sprintf("%d comment%s on %s from %s", count, pluralSuffix(count, "", "s"), agentName, strings.Join(xslice.Map(authors, describeForgeLogin), ", "))
}

func pluralSuffix(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}
