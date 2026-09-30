package assemble_test

import (
	"testing"
	"time"

	"komrad/internal/assemble"
	"komrad/lib/xslice"

	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func offer(id, pullRequestID string, writtenAgo time.Duration, set ...func(*pullrequestv1.ForgeThread)) *pullrequestv1.ForgeThread {
	thread := &pullrequestv1.ForgeThread{
		Id:            id,
		PullRequestId: pullRequestID,
		Path:          "main.go",
		Line:          3,
		WrittenAt:     stamp(-writtenAgo),
		Comments:      []*pullrequestv1.ForgeComment{{Id: id + "-c", AuthorLogin: "dana"}},
	}
	for _, f := range set {
		f(thread)
	}

	return thread
}

func resolved(thread *pullrequestv1.ForgeThread) { thread.Resolved = true }

func fromBot(thread *pullrequestv1.ForgeThread) { thread.Comments[0].Bot = true }

func fromYou(thread *pullrequestv1.ForgeThread) { thread.Comments[0].Yours = true }

func onTheWhole(thread *pullrequestv1.ForgeThread) { thread.Path, thread.Line = "", 0 }

func answeredByDana(thread *pullrequestv1.ForgeThread) {
	thread.Comments = append(thread.Comments, &pullrequestv1.ForgeComment{Id: thread.GetId() + "-r", AuthorLogin: "dana"})
}

func offers(threads ...*pullrequestv1.ForgeThread) map[string]*pullrequestv1.ForgeThread {
	rows := map[string]*pullrequestv1.ForgeThread{}
	for _, thread := range threads {
		rows[thread.GetId()] = thread
	}

	return rows
}

// TestAgentRowsOfferCounts counts only open line threads a person other than the viewer started on the current pull request.
func TestAgentRowsOfferCounts(t *testing.T) {
	t.Parallel()

	open := pullRequest{id: "p1", headRepo: repo, branch: "agent/" + newer, number: 1, openedAgo: time.Hour}
	merged := pullRequest{id: "p1", headRepo: repo, branch: "agent/" + newer, number: 1, openedAgo: time.Hour, merged: true}

	tests := []struct {
		name        string
		pullRequest pullRequest
		threads     []*pullrequestv1.ForgeThread
		want        *agentv1.CommentCounts
	}{
		{
			name:        "no offer draws nothing",
			pullRequest: open,
		},
		{
			name:        "open offers from people count as external",
			pullRequest: open,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p1", time.Hour), offer("f2", "p1", time.Minute)},
			want:        &agentv1.CommentCounts{External: 2},
		},
		{
			name:        "resolved and bot offers are left out",
			pullRequest: open,
			threads: []*pullrequestv1.ForgeThread{
				offer("f1", "p1", time.Hour),
				offer("f2", "p1", time.Hour, resolved),
				offer("f3", "p1", time.Hour, fromBot),
			},
			want: &agentv1.CommentCounts{External: 1},
		},
		{
			name:        "only bot offers draw nothing",
			pullRequest: open,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p1", time.Hour, fromBot)},
		},
		{
			name:        "a bot thread a person answered counts",
			pullRequest: open,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p1", time.Hour, fromBot, answeredByDana)},
			want:        &agentv1.CommentCounts{External: 1},
		},
		{
			name:        "comments on the change as a whole are left out",
			pullRequest: open,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p1", time.Hour, onTheWhole)},
		},
		{
			name:        "threads the person started are left out",
			pullRequest: open,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p1", time.Hour, fromYou)},
		},
		{
			name:        "offers on a finished pull request are not counted",
			pullRequest: merged,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p1", time.Hour)},
		},
		{
			name:        "offers on another pull request are not this agent's",
			pullRequest: open,
			threads:     []*pullrequestv1.ForgeThread{offer("f1", "p2", time.Hour)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			world := assemble.World{
				Agents:       map[string]*agentv1.Agent{newer: agent(newer)},
				PullRequests: pullRequests(tt.pullRequest),
				ForgeThreads: offers(tt.threads...),
			}

			rows := assemble.AgentRows(world, now)
			require.Len(t, rows, 1)
			assert.True(t, proto.Equal(tt.want, rows[0].GetPersonComments()), "person: %v", rows[0].GetPersonComments())
		})
	}
}

// TestOffers orders an agent's offers oldest first and keeps only its open pull request's.
func TestOffers(t *testing.T) {
	t.Parallel()

	current := pullRequest{id: "p2", headRepo: repo, branch: "agent/" + newer, number: 2, openedAgo: time.Hour}
	earlier := pullRequest{id: "p1", headRepo: repo, branch: "agent/" + newer, number: 1, openedAgo: 3 * time.Hour, closed: true}
	world := assemble.World{
		Agents:       map[string]*agentv1.Agent{newer: agent(newer), older: agent(older)},
		PullRequests: pullRequests(current, earlier),
		ForgeThreads: offers(
			offer("f1", "p2", time.Minute),
			offer("f2", "p2", time.Hour, fromBot),
			offer("f3", "p1", time.Hour),
		),
	}

	assert.Equal(t, []string{"f2", "f1"}, idsOf(assemble.Offers(world, newer)), "the current pull request's, oldest first, bots included")
	assert.Empty(t, assemble.Offers(world, older))
	assert.Empty(t, assemble.Offers(world, "nobody"))
}

func idsOf(threads []*pullrequestv1.ForgeThread) []string {
	return xslice.Map(threads, func(thread *pullrequestv1.ForgeThread) string { return thread.GetId() })
}
