package mail

import (
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/eventprompts"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	"komrad/proto/gen/oneofs"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addressBook is the agents an address may resolve to, across three workflows.
type addressBook struct {
	sender        *agentstore.Agent
	reviewerHere  *agentstore.Agent
	reviewerThere *agentstore.Agent
	migrations    *agentstore.Agent
	testerKomrad  *agentstore.Agent
	testerDocs    *agentstore.Agent
	retired       *agentstore.Agent
	unheldID      uuid.UUID
}

// TestSendMailRefusals pins which sentinel each way of addressing mail wrongly comes back with.
func TestSendMailRefusals(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		replyTo *uuid.UUID
		pending int
		wantErr error
	}{
		{
			name:    "an address nothing answers to",
			from:    "api",
			to:      "ghost",
			wantErr: ErrRecipientNotFound,
		},
		{
			name:    "a recipient that has been archived",
			from:    "api",
			to:      "archived",
			wantErr: ErrAgentArchived,
		},
		{
			name:    "a sender naming an agent that does not exist",
			from:    "ghost",
			to:      "migrations",
			wantErr: ErrSenderUnknown,
		},
		{
			name:    "an agent addressing itself",
			from:    "api",
			to:      "api",
			wantErr: ErrSelfAddressed,
		},
		{
			name:    "a recipient already holding a full mailbox",
			from:    "api",
			to:      "migrations",
			pending: maxPendingMail,
			wantErr: ErrMailboxFull,
		},
		{
			name:    "a reply_to naming a well-formed id nothing holds",
			from:    "api",
			to:      "migrations",
			replyTo: xptr.Ptr(uuid.New()),
			wantErr: ErrReplyToNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)

			ids := map[string]uuid.UUID{
				"api":        st.SeedAgent(t, agentstore.Agent{Name: "api"}).ID,
				"migrations": st.SeedAgent(t, agentstore.Agent{Name: "migrations"}).ID,
				"archived":   st.SeedAgent(t, agentstore.Agent{Name: "archived"}).ID,
				"ghost":      uuid.New(),
			}
			_, err := st.Agents.Archive(t.Context(), []uuid.UUID{ids["archived"]})
			require.NoError(t, err)

			for range tt.pending {
				_, err := st.Mail.Send(t.Context(), mailstore.SendParams{
					ToAgentID: ids[tt.to],
					Body:      "waiting",
					Deadline:  time.Hour,
				})
				require.NoError(t, err)
			}

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      ids[tt.from],
				ToAgentID: ids[tt.to],
				Body:      "hello",
				ReplyTo:   tt.replyTo,
			})
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sent)

			pending, err := st.Mail.CountPending(t.Context(), ids[tt.to])
			require.NoError(t, err)
			assert.EqualValues(t, tt.pending, pending, "a refused send leaves nothing behind")
		})
	}
}

// TestSendMailRecordsWhoSentIt pins the sender an agent's mail, a person's and a schedule's each carry.
func TestSendMailRecordsWhoSentIt(t *testing.T) {
	tests := []struct {
		name         string
		from         string
		schedule     string
		reply        bool
		wantFromID   bool
		wantFromName string
	}{
		{
			name:         "a person sending as nobody",
			wantFromName: "",
		},
		{
			name:         "a schedule sending for the person who wrote it",
			schedule:     "nightly",
			wantFromName: "schedule nightly",
		},
		{
			name:         "an agent sending to another",
			from:         "api",
			wantFromID:   true,
			wantFromName: "api",
		},
		{
			name:         "an agent answering on a thread",
			from:         "api",
			reply:        true,
			wantFromID:   true,
			wantFromName: "api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			sender := st.SeedAgent(t, agentstore.Agent{Name: "api"})
			recipient := st.SeedAgent(t, agentstore.Agent{Name: "migrations"})

			var replyTo *uuid.UUID
			if tt.reply {
				opening, err := st.Mail.Send(t.Context(), mailstore.SendParams{
					FromAgentID: &recipient.ID,
					ToAgentID:   sender.ID,
					Body:        "which column holds the refund?",
					Deadline:    time.Hour,
				})
				require.NoError(t, err)
				replyTo = &opening.ID
			}

			var sent *mailstore.Mail
			var err error
			switch {
			case tt.schedule != "":
				sent, err = svc.SendScheduledMail(t.Context(), SendScheduledMailParams{
					ToAgentID:    recipient.ID,
					ScheduleName: tt.schedule,
					Body:         "check CI",
				})
			case tt.from != "":
				sent, err = svc.SendMail(t.Context(), SendMailParams{
					From:      sender.ID,
					ToAgentID: recipient.ID,
					Body:      "refunded_cents",
					ReplyTo:   replyTo,
				})
			default:
				sent, err = svc.SendMail(t.Context(), SendMailParams{
					ToAgentID: recipient.ID,
					Body:      "which column holds the refund?",
				})
			}
			require.NoError(t, err)

			waiting, err := st.ClaimMail(t.Context(), recipient.ID, maxClaimedMail)
			require.NoError(t, err)
			require.Len(t, waiting, 1)
			assert.Equal(t, sent.ID, waiting[0].ID)
			assert.Equal(t, mailstore.KindPlain, waiting[0].Kind)
			assert.Equal(t, tt.wantFromName, waiting[0].FromName)
			assert.Equal(t, replyTo, waiting[0].ReplyTo)

			if !tt.wantFromID {
				assert.Nil(t, waiting[0].FromAgentID)

				return
			}

			require.NotNil(t, waiting[0].FromAgentID)
			assert.Equal(t, sender.ID, *waiting[0].FromAgentID)
		})
	}
}

// TestSendMailRefusesAPairAtTheExchangeCap pins that komrad refuses ordinary mail between a pair once they reach the cap.
func TestSendMailRefusesAPairAtTheExchangeCap(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, 2)
	agents := seedTask(t, st)

	exchange(t, svc, exchangeParams{
		Reviewer: agents["reviewer"],
		Writer:   agents["writer"],
		Messages: 2,
	})

	sent, err := svc.SendMail(t.Context(), SendMailParams{
		From:      agents["reviewer"].ID,
		ToAgentID: agents["writer"].ID,
		Body:      "one more",
	})
	require.ErrorIs(t, err, ErrExchangeCapped)
	assert.Nil(t, sent)
}

// TestSendMailLetsThroughWhatTheCapDoesNotStop pins the sends the exchange cap leaves alone.
func TestSendMailLetsThroughWhatTheCapDoesNotStop(t *testing.T) {
	tests := []struct {
		name      string
		exchanged int
	}{
		{
			name:      "a pair inside the cap",
			exchanged: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, 2)
			agents := seedTask(t, st)

			exchange(t, svc, exchangeParams{
				Reviewer: agents["reviewer"],
				Writer:   agents["writer"],
				Messages: tt.exchanged,
			})

			_, err := svc.SendMail(t.Context(), SendMailParams{
				From:      agents["reviewer"].ID,
				ToAgentID: agents["writer"].ID,
				Body:      "one more",
			})
			require.NoError(t, err)
		})
	}
}

// TestSendMailTellsTheRecipientWhenTheCapRefusesASend walks the mail a refused send leaves the side that would otherwise wait on it.
func TestSendMailTellsTheRecipientWhenTheCapRefusesASend(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, 2)
	agents := seedTask(t, st)
	writer, reviewer, secondReviewer := agents["writer"], agents["reviewer"], agents["second-reviewer"]

	refuse := func(t *testing.T, from agentstore.Agent) {
		t.Helper()

		_, err := svc.SendMail(t.Context(), SendMailParams{
			From:      from.ID,
			ToAgentID: writer.ID,
			Body:      "one more",
		})
		require.ErrorIs(t, err, ErrExchangeCapped)
	}

	exchange(t, svc, exchangeParams{
		Reviewer: reviewer,
		Writer:   writer,
		Messages: 2,
	})

	t.Run("a pair at the cap is nobody's business until a send is refused", func(t *testing.T) {
		assert.Empty(t, listCappedMail(t, st, writer.ID))
	})

	t.Run("the refusal leaves the recipient mail naming the sender", func(t *testing.T) {
		refuse(t, reviewer)

		told := listCappedMail(t, st, writer.ID)
		require.Len(t, told, 1)
		assert.Contains(t, told[0].Body, reviewer.Name)
		assert.Contains(t, told[0].Body, reviewer.ID.String(), "the id, since two workflows may hold one name")
	})

	t.Run("unread mail stands for the refusals behind it", func(t *testing.T) {
		refuse(t, reviewer)
		refuse(t, reviewer)

		assert.Len(t, listCappedMail(t, st, writer.ID), 1)
	})

	t.Run("a recipient that has read the mail is told again", func(t *testing.T) {
		_, err := st.ClaimMail(t.Context(), writer.ID, maxPendingMail)
		require.NoError(t, err)

		refuse(t, reviewer)

		assert.Len(t, listCappedMail(t, st, writer.ID), 2)
	})

	t.Run("a second severed pair gets mail of its own", func(t *testing.T) {
		exchange(t, svc, exchangeParams{
			Reviewer: secondReviewer,
			Writer:   writer,
			Messages: 2,
		})
		refuse(t, secondReviewer)

		about := xslice.Map(listCappedMail(t, st, writer.ID), func(told mailstore.Mail) uuid.UUID {
			require.NotNil(t, told.SubjectID)

			return *told.SubjectID
		})
		assert.ElementsMatch(t, []uuid.UUID{reviewer.ID, reviewer.ID, secondReviewer.ID}, about)
	})
}

// TestReviewerMailStaysOnItsTask pins that a reviewer may reach nobody outside its task and its
// workflow's orchestrator.
func TestReviewerMailStaysOnItsTask(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "a reviewer reaching another task's writer",
			from: "reviewer",
			to:   "other-writer",
		},
		{
			name: "a reviewer reaching another task's reviewer",
			from: "reviewer",
			to:   "other-reviewer",
		},
		{
			name: "another task's writer reaching a reviewer",
			from: "other-writer",
			to:   "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			agents := seedTask(t, st)

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      agents[tt.from].ID,
				ToAgentID: agents[tt.to].ID,
				Body:      "hello",
			})
			require.ErrorIs(t, err, ErrOutsideTask)
			assert.Nil(t, sent)
		})
	}
}

// TestTaskMailGetsThrough pins the sends the task rule leaves alone.
func TestTaskMailGetsThrough(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{
			name: "a reviewer answers its task's writer",
			from: "reviewer",
			to:   "writer",
		},
		{
			name: "a writer reaches its reviewer",
			from: "writer",
			to:   "reviewer",
		},
		{
			name: "one reviewer reaches another on its task",
			from: "reviewer",
			to:   "second-reviewer",
		},
		{
			name: "one writer reaches another",
			from: "writer",
			to:   "other-writer",
		},
		{
			name: "a reviewer reaches its workflow's orchestrator",
			from: "reviewer",
			to:   "orchestrator",
		},
		{
			name: "the orchestrator reaches a reviewer",
			from: "orchestrator",
			to:   "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			agents := seedTask(t, st)

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      agents[tt.from].ID,
				ToAgentID: agents[tt.to].ID,
				Body:      "hello",
			})
			require.NoError(t, err)
			assert.Equal(t, agents[tt.to].ID, sent.ToAgentID)
		})
	}
}

// TestSendFromKomradRefusesIncompleteMail pins that mail from komrad must say what it is and what it is about.
func TestSendFromKomradRefusesIncompleteMail(t *testing.T) {
	tests := []struct {
		name    string
		params  SendFromKomradParams
		wantErr error
	}{
		{
			name: "a plain kind",
			params: SendFromKomradParams{
				Mail:      fakeMail{kind: mailstore.KindPlain, body: "hello"},
				SubjectID: uuid.New(),
			},
			wantErr: ErrKindMissing,
		},
		{
			name:    "no subject",
			params:  SendFromKomradParams{Mail: fakeMail{kind: mailstore.KindDocumentEdited, body: "hello"}},
			wantErr: errSubjectMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			tt.params.ToAgentID = st.SeedAgent(t, agentstore.Agent{Name: "migrations"}).ID

			sent, err := svc.SendFromKomrad(t.Context(), tt.params)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sent)
		})
	}
}

// TestSendFromKomradRewritesTheUnreadOne pins that one unread mail stands for every change to its subject until it is read.
func TestSendFromKomradRewritesTheUnreadOne(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	ctx := t.Context()
	recipient := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	one, other := uuid.New(), uuid.New()

	notify := func(document uuid.UUID, body string) *mailstore.Mail {
		t.Helper()
		sent, err := svc.SendFromKomrad(ctx, SendFromKomradParams{
			ToAgentID: recipient.ID,
			Mail:      fakeMail{kind: mailstore.KindDocumentEdited, body: body},
			SubjectID: document,
		})
		require.NoError(t, err)

		return sent
	}

	first := notify(one, "first edit")
	second := notify(one, "second edit")
	assert.Equal(t, first.ID, second.ID, "a second edit rewrites the unread mail")
	assert.Equal(t, "second edit", second.Body)
	assert.NotEqual(t, first.ID, notify(other, "edit").ID, "an edit of another document is told apart")

	_, err := st.ClaimMail(ctx, recipient.ID, maxClaimedMail)
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, notify(one, "third edit").ID, "an edit after the mail was read sends another")
}

// TestSendFromKomradCarriesTheWorkflowsEventPrompts pins which of the workflow's prompts mail from komrad carries as its events start and stop holding.
func TestSendFromKomradCarriesTheWorkflowsEventPrompts(t *testing.T) {
	const (
		failing     = "pull_request.checks_failing"
		behind      = "pull_request.behind"
		approved    = "pull_request.approved"
		conflicting = "pull_request.conflicting"
	)

	prompts := eventprompts.Prompts{
		{Event: failing, Prompt: "fix the checks"},
		{Event: behind, Prompt: "rebase onto main"},
		{
			Event:  conflicting,
			Prompt: "settle the conflict",
			Muted:  true,
		},
	}

	type send struct {
		events  []string
		holding []string
		read    bool
	}

	tests := []struct {
		name       string
		archived   bool
		sends      []send
		wantEvents []string
		wantNone   bool
	}{
		{
			name:       "an event with a prompt carries it after the body",
			sends:      []send{{events: []string{failing}}},
			wantEvents: []string{failing},
		},
		{
			name:  "an event the workflow attaches nothing to carries no section",
			sends: []send{{events: []string{approved}}},
		},
		{
			name: "a rewrite keeps the prompts of events started earlier that still hold",
			sends: []send{
				{events: []string{failing}},
				{events: []string{behind}, holding: []string{failing, behind}},
			},
			wantEvents: []string{failing, behind},
		},
		{
			name: "a rewrite drops the prompt of an event that no longer holds",
			sends: []send{
				{events: []string{failing, behind}},
				{holding: []string{behind}},
			},
			wantEvents: []string{behind},
		},
		{
			name: "an event still holding after the mail was read is not started again",
			sends: []send{
				{events: []string{failing}},
				{read: true, holding: []string{failing}},
			},
		},
		{
			name:     "a muted event opens no mail",
			sends:    []send{{events: []string{conflicting}}},
			wantNone: true,
		},
		{
			name: "a muted event rewrites held mail and leaves its prompt out",
			sends: []send{
				{events: []string{failing}},
				{events: []string{conflicting}, holding: []string{failing, conflicting}},
			},
			wantEvents: []string{failing},
		},
		{
			name:       "a muted event alongside one that is not opens mail without the muted prompt",
			sends:      []send{{events: []string{failing, conflicting}}},
			wantEvents: []string{failing},
		},
		{
			name:     "an archived workflow's prompts are not carried",
			archived: true,
			sends:    []send{{events: []string{failing}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			ctx := t.Context()
			workflow := st.SeedWorkflow(t, workflowstore.Config{
				Name:         "backend",
				EventPrompts: prompts,
			})
			recipient := st.SeedAgent(t, agentstore.Agent{Name: "author", WorkflowID: workflow.ID})
			if tt.archived {
				require.NoError(t, st.Workflows.Archive(ctx, workflow.ID))
			}
			subject := uuid.New()

			var sent *mailstore.Mail
			for _, s := range tt.sends {
				if s.read {
					_, err := st.ClaimMail(ctx, recipient.ID, maxClaimedMail)
					require.NoError(t, err)
				}

				var err error
				sent, err = svc.SendFromKomrad(ctx, SendFromKomradParams{
					ToAgentID: recipient.ID,
					Mail: fakeMail{
						kind:   mailstore.KindPullRequestChanged,
						body:   "changed",
						events: s.events,
					},
					SubjectID: subject,
					Holding:   s.holding,
				})
				if tt.wantNone {
					require.ErrorIs(t, err, ErrNothingUnread)
					assert.Nil(t, sent)

					return
				}
				require.NoError(t, err)
			}

			assert.Equal(t, prompts.Append("changed", tt.wantEvents, MaxMailBody), sent.Body)
		})
	}
}

// TestListWaitingMail pins that komrad refuses a listing for an id no agent holds and lists at most
// one batch otherwise, claiming none of it.
func TestListWaitingMail(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	recipient := st.SeedAgent(t, agentstore.Agent{Name: "migrations"})

	for range maxClaimedMail + 1 {
		_, err := svc.SendMail(t.Context(), SendMailParams{
			ToAgentID: recipient.ID,
			Body:      "hello",
		})
		require.NoError(t, err)
	}

	t.Run("an id no agent holds", func(t *testing.T) {
		waiting, err := svc.ListWaitingMail(t.Context(), uuid.New())
		require.ErrorIs(t, err, ErrAgentNotFound)
		assert.Empty(t, waiting)
	})

	t.Run("an agent reading its own sees one batch", func(t *testing.T) {
		waiting, err := svc.ListWaitingMail(t.Context(), recipient.ID)
		require.NoError(t, err)
		assert.Len(t, waiting, maxClaimedMail)

		again, err := svc.ListWaitingMail(t.Context(), recipient.ID)
		require.NoError(t, err)
		assert.Equal(t, waiting, again, "a listing claims nothing")
	})
}

// TestClaimMail pins that a claim refuses an id no agent holds, takes only the mail named, and in
// the same transaction stamps the comments its review_comment mail carries as read on the snapshot,
// leaving other mail's comments untouched.
func TestClaimMail(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	ctx := t.Context()
	writer := st.SeedAgent(t, agentstore.Agent{Name: "writer"})
	reviewStore := reviewstore.NewReviewStore(st.Pool.GetDB())

	snapshot, err := reviewStore.CreateSnapshot(ctx, reviewstore.CreateSnapshotParams{
		TaskID:  *writer.TaskID,
		BaseSHA: "base",
		SHA:     "head",
	})
	require.NoError(t, err)
	readOn, err := reviewStore.CreateSnapshot(ctx, reviewstore.CreateSnapshotParams{
		TaskID:  *writer.TaskID,
		BaseSHA: "base",
		SHA:     "later",
	})
	require.NoError(t, err)

	comment := func(body string) (uuid.UUID, uuid.UUID) {
		opened, err := reviewStore.CreateThread(ctx, reviewstore.CreateThreadParams{
			TaskID:     *writer.TaskID,
			SnapshotID: snapshot.ID,
			BaseSHA:    "base",
			FilePath:   "main.go",
			NewStart:   1,
			NewEnd:     1,
			Body:       body,
			AuthorKind: reviewstore.AuthorHuman,
		})
		require.NoError(t, err)
		sent, err := st.Mail.UpsertFromKomrad(ctx, mailstore.UpsertFromKomradParams{
			FromName:  "komrad",
			ToAgentID: writer.ID,
			Body:      body,
			Kind:      mailstore.KindReviewComment,
			SubjectID: opened.MessageID,
			Deadline:  time.Hour,
		})
		require.NoError(t, err)
		require.NoError(t, reviewStore.SetMessageMail(ctx, opened.MessageID, sent.ID))

		return opened.ID, sent.ID
	}

	readThread, readMail := comment("first")
	laterThread, laterMail := comment("second")
	plain, err := svc.SendMail(ctx, SendMailParams{ToAgentID: writer.ID, Body: "hello"})
	require.NoError(t, err)

	t.Run("an id no agent holds", func(t *testing.T) {
		claimed, err := svc.ClaimMail(ctx, ClaimMailParams{AgentID: uuid.New(), IDs: []uuid.UUID{readMail}})
		require.ErrorIs(t, err, ErrAgentNotFound)
		assert.Empty(t, claimed)
	})

	t.Run("only the mail named is claimed and its comment stamped", func(t *testing.T) {
		claimed, err := svc.ClaimMail(ctx, ClaimMailParams{
			AgentID:        writer.ID,
			IDs:            []uuid.UUID{readMail, plain.ID},
			ReadSnapshotID: &readOn.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{readMail, plain.ID}, xslice.Map(claimed, func(m mailstore.Mail) uuid.UUID { return m.ID }))

		waiting, err := svc.ListWaitingMail(ctx, writer.ID)
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{laterMail}, xslice.Map(waiting, func(m mailstore.Mail) uuid.UUID { return m.ID }))

		messages, err := reviewStore.ListMessages(ctx, []uuid.UUID{readThread, laterThread})
		require.NoError(t, err)
		require.Len(t, messages, 2)
		read, later := messages[0], messages[1]
		if read.ThreadID != readThread {
			read, later = later, read
		}
		require.NotNil(t, read.ReadAt)
		assert.Equal(t, &readOn.ID, read.ReadSnapshotID)
		assert.Nil(t, later.ReadAt)
		assert.Nil(t, later.ReadSnapshotID)
	})

	t.Run("a comment read with no snapshot is stamped read on none", func(t *testing.T) {
		_, err := svc.ClaimMail(ctx, ClaimMailParams{AgentID: writer.ID, IDs: []uuid.UUID{laterMail}})
		require.NoError(t, err)

		messages, err := reviewStore.ListMessages(ctx, []uuid.UUID{laterThread})
		require.NoError(t, err)
		require.Len(t, messages, 1)
		assert.NotNil(t, messages[0].ReadAt)
		assert.Nil(t, messages[0].ReadSnapshotID)
	})
}

// TestListMail pins that komrad refuses a listing for an unknown agent and otherwise reads the half of the correspondence asked for.
func TestListMail(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	api := st.SeedAgent(t, agentstore.Agent{Name: "api"})
	migrations := st.SeedAgent(t, agentstore.Agent{Name: "migrations"})

	_, err := svc.SendMail(t.Context(), SendMailParams{
		From:      api.ID,
		ToAgentID: migrations.ID,
		Body:      "what api said",
	})
	require.NoError(t, err)

	_, err = svc.SendMail(t.Context(), SendMailParams{
		From:      migrations.ID,
		ToAgentID: api.ID,
		Body:      "what migrations said",
	})
	require.NoError(t, err)

	t.Run("an id no agent holds", func(t *testing.T) {
		listed, err := svc.ListMail(t.Context(), ListMailParams{
			AgentID: uuid.New(),
			Limit:   10,
		})
		require.ErrorIs(t, err, ErrAgentNotFound)
		assert.Empty(t, listed)
	})

	tests := []struct {
		name     string
		agentID  uuid.UUID
		sent     bool
		wantBody string
	}{
		{
			name:     "the mail addressed to the agent",
			agentID:  api.ID,
			wantBody: "what migrations said",
		},
		{
			name:     "the mail the agent sent",
			agentID:  api.ID,
			sent:     true,
			wantBody: "what api said",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed, err := svc.ListMail(t.Context(), ListMailParams{
				AgentID: tt.agentID,
				Sent:    tt.sent,
				Limit:   10,
			})
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, tt.wantBody, listed[0].Body)
		})
	}
}

// TestDeleteMail pins that a batch goes whole or not at all.
func TestDeleteMail(t *testing.T) {
	tests := []struct {
		name        string
		withUnknown bool
		wantErr     error
		wantLeft    int
	}{
		{
			name:        "a batch naming one id nothing holds",
			withUnknown: true,
			wantErr:     ErrMailNotFound,
			wantLeft:    2,
		},
		{
			name: "a batch every id of which holds mail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			recipient := st.SeedAgent(t, agentstore.Agent{Name: "migrations"})

			var ids []uuid.UUID
			for range 2 {
				sent, err := svc.SendMail(t.Context(), SendMailParams{
					ToAgentID: recipient.ID,
					Body:      "hello",
				})
				require.NoError(t, err)
				ids = append(ids, sent.ID)
			}
			if tt.withUnknown {
				ids = append(ids, uuid.New())
			}

			err := svc.DeleteMail(t.Context(), ids)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			left, err := st.Mail.CountPending(t.Context(), recipient.ID)
			require.NoError(t, err)
			assert.EqualValues(t, tt.wantLeft, left)
		})
	}
}

// TestFindRecipientByID pins that an id answers the agent holding it, live or archived.
func TestFindRecipientByID(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	book := seedAddressBook(t, st)

	tests := []struct {
		name    string
		id      uuid.UUID
		want    uuid.UUID
		wantErr error
	}{
		{
			name:    "an id nothing holds",
			id:      book.unheldID,
			wantErr: ErrRecipientNotFound,
		},
		{
			name: "a live agent",
			id:   book.reviewerHere.ID,
			want: book.reviewerHere.ID,
		},
		{
			name: "an archived agent, for the send to refuse as archived",
			id:   book.retired.ID,
			want: book.retired.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.FindRecipient(t.Context(), FindRecipientParams{
				To: oneofs.To{ToAgentId: xptr.Ptr(uuid.MustParse(tt.id.String()))},
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.ID)
		})
	}
}

// TestFindRecipientRefusals pins which sentinel each name nothing answers to, or more than one agent does, comes back with.
func TestFindRecipientRefusals(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	book := seedAddressBook(t, st)

	tests := []struct {
		name     string
		from     uuid.UUID
		to       string
		wantErr  error
		wantSaid []string
	}{
		{
			name:    "a name nobody holds",
			from:    book.sender.ID,
			to:      "nobody",
			wantErr: ErrRecipientNotFound,
		},
		{
			name:    "a name held only by an archived agent",
			from:    book.sender.ID,
			to:      "retired",
			wantErr: ErrRecipientNotFound,
		},
		{
			name:     "a name two other workflows hold",
			from:     book.sender.ID,
			to:       "tester",
			wantErr:  ErrRecipientAmbiguous,
			wantSaid: []string{book.testerKomrad.ID.String() + ` in workflow "komrad"`, book.testerDocs.ID.String() + ` in workflow "docs"`},
		},
		{
			name:     "a name two workflows hold, from nobody",
			to:       "reviewer",
			wantErr:  ErrRecipientAmbiguous,
			wantSaid: []string{book.reviewerHere.ID.String() + ` in workflow "gadgets"`, book.reviewerThere.ID.String() + ` in workflow "komrad"`},
		},
		{
			name:    "a name from a sender the panel does not know",
			from:    uuid.New(),
			to:      "migrations",
			wantErr: ErrSenderUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.FindRecipient(t.Context(), FindRecipientParams{
				From: tt.from,
				To:   oneofs.To{ToAgentName: xptr.Ptr(tt.to)},
			})
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, got)
			for _, said := range tt.wantSaid {
				assert.ErrorContains(t, err, said)
			}
		})
	}
}

// TestFindRecipient pins that a name resolves in the sender's workflow first, then in any workflow.
func TestFindRecipient(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	book := seedAddressBook(t, st)

	tests := []struct {
		name string
		from uuid.UUID
		to   string
		want uuid.UUID
	}{
		{
			name: "a name held in the sender's own workflow and in another",
			from: book.sender.ID,
			to:   "reviewer",
			want: book.reviewerHere.ID,
		},
		{
			name: "a name held only in another workflow",
			from: book.sender.ID,
			to:   "migrations",
			want: book.migrations.ID,
		},
		{
			name: "a name from nobody, which has no workflow to look in first",
			to:   "migrations",
			want: book.migrations.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.FindRecipient(t.Context(), FindRecipientParams{
				From: tt.from,
				To:   oneofs.To{ToAgentName: xptr.Ptr(tt.to)},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.ID)
		})
	}
}

// seedAddressBook records the agents of an address book across three workflows, archiving one.
func seedAddressBook(t *testing.T, st *storetest.Stores) addressBook {
	t.Helper()

	gadgets := st.SeedWorkflow(t, workflowstore.Config{Name: "gadgets"})
	komrad := st.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	docs := st.SeedWorkflow(t, workflowstore.Config{Name: "docs"})

	seed := func(name string, workflowID uuid.UUID) *agentstore.Agent {
		return st.SeedAgent(t, agentstore.Agent{
			Name:       name,
			WorkflowID: workflowID,
		})
	}

	book := addressBook{
		sender:        seed("api", gadgets.ID),
		reviewerHere:  seed("reviewer", gadgets.ID),
		reviewerThere: seed("reviewer", komrad.ID),
		migrations:    seed("migrations", komrad.ID),
		testerKomrad:  seed("tester", komrad.ID),
		testerDocs:    seed("tester", docs.ID),
		retired:       seed("retired", komrad.ID),
		unheldID:      uuid.New(),
	}

	_, err := st.Agents.Archive(t.Context(), []uuid.UUID{book.retired.ID})
	require.NoError(t, err)

	return book
}

func newMailService(st *storetest.Stores, maxExchange int) *MailService {
	return NewMailService(NewMailServiceParams{
		Mail:      st.Mail,
		Agents:    st.Agents,
		Workflows: st.Workflows,
		Config:    config.Mail{MaxExchange: maxExchange},
		Tx:        st.Pool,
	})
}

// seedTask records a writer with two reviewers on its task, its workflow's orchestrator, and another
// task's writer and reviewer.
func seedTask(t *testing.T, st *storetest.Stores) map[string]agentstore.Agent {
	t.Helper()

	writer := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	otherWriter := st.SeedAgent(t, agentstore.Agent{Name: "other-author"})

	seedReviewer := func(name string, writer agentstore.Agent) agentstore.Agent {
		return *st.SeedAgent(t, agentstore.Agent{
			Name:       name,
			Role:       agentstore.RoleReviewer,
			WorkflowID: writer.WorkflowID,
			TaskID:     writer.TaskID,
		})
	}

	return map[string]agentstore.Agent{
		"writer":          *writer,
		"reviewer":        seedReviewer("reviewer", *writer),
		"second-reviewer": seedReviewer("test-runner", *writer),
		"orchestrator": *st.SeedAgent(t, agentstore.Agent{
			Name:       "orchestrator",
			Role:       agentstore.RoleOrchestrator,
			WorkflowID: writer.WorkflowID,
		}),
		"other-writer":   *otherWriter,
		"other-reviewer": seedReviewer("their-reviewer", *otherWriter),
	}
}

type exchangeParams struct {
	Reviewer agentstore.Agent
	Writer   agentstore.Agent
	Messages int
}

// exchange sends mail back and forth between a reviewer and its task's writer, every message opening a thread of its own.
func exchange(t *testing.T, svc *MailService, params exchangeParams) {
	t.Helper()

	for at := range params.Messages {
		from, to := params.Reviewer, params.Writer
		if at%2 == 1 {
			from, to = params.Writer, params.Reviewer
		}

		_, err := svc.SendMail(t.Context(), SendMailParams{
			From:      from.ID,
			ToAgentID: to.ID,
			Body:      "snapshot",
		})
		require.NoError(t, err)
	}
}

func listCappedMail(t *testing.T, st *storetest.Stores, agentID uuid.UUID) []mailstore.Mail {
	t.Helper()

	listed, err := st.Mail.ListPage(t.Context(), agentID, maxPendingMail, nil)
	require.NoError(t, err)

	return xslice.Filter(listed, func(mail mailstore.Mail) bool {
		return mail.Kind == mailstore.KindExchangeCapped
	})
}

// TestIsDormant covers which agents nothing runs behind, so mail to them waits for a resume.
func TestIsDormant(t *testing.T) {
	tests := []struct {
		name        string
		state       agentstore.State
		running     bool
		wantDormant bool
	}{
		{
			name:        "an idle agent whose pane is gone",
			state:       agentstore.StateIdle,
			wantDormant: true,
		},
		{
			name:  "an agent still being set up, whose pane does not decide yet",
			state: agentstore.StateSettingUp,
		},
		{
			name:    "an idle agent in its pane",
			state:   agentstore.StateIdle,
			running: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := tmuxtest.NewFake()
			agent := agentstore.Agent{
				ID:    uuid.New(),
				Pane:  "%beta",
				State: tt.state,
			}
			if tt.running {
				terminal.Owners[agent.Pane] = agent.ID
			}
			svc := NewMailService(NewMailServiceParams{Panes: paneOwners{terminal: terminal}})

			assert.Equal(t, tt.wantDormant, svc.IsDormant(agent))
		})
	}
}

type fakeMail struct {
	kind   mailstore.Kind
	body   string
	events []string
}

func (m fakeMail) Kind() mailstore.Kind {
	return m.kind
}

func (m fakeMail) Events() []string {
	return m.events
}

func (m fakeMail) Body() string {
	return m.body
}

type paneOwners struct {
	terminal *tmuxtest.Fake
}

func (p paneOwners) IsAgentRunning(agent agentstore.Agent) bool {
	return agent.ID != "" && p.terminal.GetPaneOwner(agent.Pane) == agent.ID
}
