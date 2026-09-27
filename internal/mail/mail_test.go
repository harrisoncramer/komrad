package mail

import (
	"testing"
	"time"

	"komrad/config"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

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
	unheldID      string
}

// TestSendMailRefusals pins which sentinel each way of addressing mail wrongly comes back with.
func TestSendMailRefusals(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		replyTo *string
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
			name:    "a person's mail to a mailbox full of agent mail",
			to:      "migrations",
			pending: maxPendingMail,
			wantErr: ErrMailboxFull,
		},
		{
			name:    "a reply_to that is not a well-formed id",
			from:    "api",
			to:      "migrations",
			replyTo: xptr.Ptr("not-an-id"),
			wantErr: ErrReplyToNotFound,
		},
		{
			name:    "a reply_to naming a well-formed id nothing holds",
			from:    "api",
			to:      "migrations",
			replyTo: xptr.Ptr(uuid.NewString()),
			wantErr: ErrReplyToNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)

			ids := map[string]string{
				"api":        st.SeedAgent(t, agentstore.Agent{Name: "api"}).ID,
				"migrations": st.SeedAgent(t, agentstore.Agent{Name: "migrations"}).ID,
				"archived":   st.SeedAgent(t, agentstore.Agent{Name: "archived"}).ID,
				"neighbour":  st.SeedAgent(t, agentstore.Agent{Name: "neighbour"}).ID,
				"ghost":      uuid.NewString(),
			}
			_, err := st.Agents.Archive(t.Context(), []string{ids["archived"]})
			require.NoError(t, err)

			for range tt.pending {
				_, err := st.Mail.Send(t.Context(), mailstore.SendParams{
					FromAgentID: xptr.Ptr(ids["neighbour"]),
					ToAgentID:   ids[tt.to],
					Body:        "waiting",
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

// TestSendMailRecordsWhoSentIt pins the sender an agent's mail, a person's, a schedule's and komrad's own each carry.
func TestSendMailRecordsWhoSentIt(t *testing.T) {
	tests := []struct {
		name         string
		from         string
		system       bool
		schedule     string
		reply        bool
		wantFromID   bool
		wantFromName string
		wantExpires  bool
	}{
		{
			name:         "a person sending as nobody",
			wantFromName: "",
			wantExpires:  true,
		},
		{
			name:         "komrad's own mail",
			system:       true,
			wantFromName: KomradSenderName,
			wantExpires:  true,
		},
		{
			name:         "a schedule sending for the person who wrote it",
			schedule:     "nightly",
			wantFromName: "schedule nightly",
			wantExpires:  true,
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

			var replyTo *string
			if tt.reply {
				opening, err := st.Mail.Send(t.Context(), mailstore.SendParams{
					FromAgentID: &recipient.ID,
					ToAgentID:   sender.ID,
					Body:        "which column holds the refund?",
					Deadline:    xptr.Ptr(time.Hour),
				})
				require.NoError(t, err)
				replyTo = &opening.ID
			}

			var sent *mailstore.Mail
			var err error
			switch {
			case tt.system:
				sent, err = svc.SendSystemMail(t.Context(), recipient.ID, "your delegate was archived")
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

			waiting, err := st.Mail.Claim(t.Context(), recipient.ID, maxClaimedMail)
			require.NoError(t, err)
			require.Len(t, waiting, 1)
			assert.Equal(t, sent.ID, waiting[0].ID)
			assert.Equal(t, mailstore.KindPlain, waiting[0].Kind)
			assert.Equal(t, tt.wantFromName, waiting[0].FromName)
			assert.Equal(t, replyTo, waiting[0].ReplyTo)
			assert.Equal(t, tt.wantExpires, waiting[0].ExpiresAt != nil)

			if !tt.wantFromID {
				assert.Nil(t, waiting[0].FromAgentID)

				return
			}

			require.NotNil(t, waiting[0].FromAgentID)
			assert.Equal(t, sender.ID, *waiting[0].FromAgentID)
		})
	}
}

// TestAgentMailGetsPastKomradsOwn pins that unread mail no agent sent never keeps an agent's mail out.
func TestAgentMailGetsPastKomradsOwn(t *testing.T) {
	tests := []struct {
		name string
		fill func(svc *MailService, recipientID string) error
	}{
		{
			name: "a mailbox full of schedule mail",
			fill: func(svc *MailService, recipientID string) error {
				_, err := svc.SendScheduledMail(t.Context(), SendScheduledMailParams{
					ToAgentID:    recipientID,
					ScheduleName: "every-minute",
					Body:         "Hey!",
				})

				return err
			},
		},
		{
			name: "a mailbox full of komrad's own mail",
			fill: func(svc *MailService, recipientID string) error {
				_, err := svc.SendSystemMail(t.Context(), recipientID, "your delegate was archived")

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			sender := st.SeedAgent(t, agentstore.Agent{Name: "api"})
			recipient := st.SeedAgent(t, agentstore.Agent{Name: "migrations"})

			for range maxPendingMail {
				require.NoError(t, tt.fill(svc, recipient.ID))
			}
			require.ErrorIs(t, tt.fill(svc, recipient.ID), ErrMailboxFull)

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      sender.ID,
				ToAgentID: recipient.ID,
				Body:      "the PR is ready",
			})
			require.NoError(t, err)
			assert.Equal(t, recipient.ID, sent.ToAgentID)
		})
	}
}

// TestSendMailRefusesAPairAtTheExchangeCap pins that ordinary mail between a pair is refused once they reach the cap.
func TestSendMailRefusesAPairAtTheExchangeCap(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, 2)
	family := seedFamily(t, st)

	exchange(t, svc, exchangeParams{
		Child:    family["child-one"],
		Parent:   family["parent"],
		Messages: 2,
	})

	sent, err := svc.SendMail(t.Context(), SendMailParams{
		From:      family["child-one"].ID,
		ToAgentID: family["parent"].ID,
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
		system    bool
	}{
		{
			name:      "komrad's own mail to an agent whose pair has reached the cap",
			exchanged: 2,
			system:    true,
		},
		{
			name:      "a pair inside the cap",
			exchanged: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, 2)
			family := seedFamily(t, st)

			exchange(t, svc, exchangeParams{
				Child:    family["child-one"],
				Parent:   family["parent"],
				Messages: tt.exchanged,
			})

			if tt.system {
				_, err := svc.SendSystemMail(t.Context(), family["parent"].ID, "your delegate was archived")
				require.NoError(t, err)

				return
			}

			_, err := svc.SendMail(t.Context(), SendMailParams{
				From:      family["child-one"].ID,
				ToAgentID: family["parent"].ID,
				Body:      "one more",
			})
			require.NoError(t, err)
		})
	}
}

// TestSendMailTellsTheRecipientWhenTheCapRefusesASend walks the notice a refused send leaves the side that would otherwise wait on it.
func TestSendMailTellsTheRecipientWhenTheCapRefusesASend(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, 2)
	family := seedFamily(t, st)
	parent, childOne, childTwo := family["parent"], family["child-one"], family["child-two"]

	refuse := func(t *testing.T, from agentstore.Agent) {
		t.Helper()

		_, err := svc.SendMail(t.Context(), SendMailParams{
			From:      from.ID,
			ToAgentID: parent.ID,
			Body:      "one more",
		})
		require.ErrorIs(t, err, ErrExchangeCapped)
	}

	exchange(t, svc, exchangeParams{
		Child:    childOne,
		Parent:   parent,
		Messages: 2,
	})

	t.Run("a pair at the cap is nobody's business until a send is refused", func(t *testing.T) {
		assert.Empty(t, listCappedNotices(t, st, parent.ID))
	})

	t.Run("the refusal leaves the recipient a notice naming the sender", func(t *testing.T) {
		refuse(t, childOne)

		told := listCappedNotices(t, st, parent.ID)
		require.Len(t, told, 1)
		assert.Contains(t, told[0].Body, childOne.Name)
		assert.Contains(t, told[0].Body, childOne.ID, "the id, since two workflows may hold one name")
	})

	t.Run("an unread notice stands for the refusals behind it", func(t *testing.T) {
		refuse(t, childOne)
		refuse(t, childOne)

		assert.Len(t, listCappedNotices(t, st, parent.ID), 1)
	})

	t.Run("a recipient that has read the notice is told again", func(t *testing.T) {
		_, err := st.Mail.Claim(t.Context(), parent.ID, maxPendingMail)
		require.NoError(t, err)

		refuse(t, childOne)

		assert.Len(t, listCappedNotices(t, st, parent.ID), 2)
	})

	t.Run("a second severed pair gets a notice of its own", func(t *testing.T) {
		exchange(t, svc, exchangeParams{
			Child:    childTwo,
			Parent:   parent,
			Messages: 2,
		})
		refuse(t, childTwo)

		about := xslice.Map(listCappedNotices(t, st, parent.ID), func(notice mailstore.Mail) string {
			require.NotNil(t, notice.AboutAgentID)

			return *notice.AboutAgentID
		})
		assert.ElementsMatch(t, []string{childOne.ID, childOne.ID, childTwo.ID}, about)
	})
}

// TestChildMailStaysInTheFamily pins that a child may reach nobody outside its parent and that parent's other children.
func TestChildMailStaysInTheFamily(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		opening *opening
	}{
		{
			name: "a child reaching an unrelated agent",
			from: "child-one",
			to:   "stranger",
		},
		{
			name: "a child reaching another parent's child",
			from: "child-one",
			to:   "cousin",
		},
		{
			name:    "a child answering mail a stranger sent to somebody else",
			from:    "child-one",
			to:      "stranger",
			opening: &opening{from: "stranger", to: "child-two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			family := seedFamily(t, st)

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      family[tt.from].ID,
				ToAgentID: family[tt.to].ID,
				Body:      "hello",
				ReplyTo:   sendOpening(t, st, family, tt.opening),
			})
			require.ErrorIs(t, err, ErrOutsideFamily)
			assert.Nil(t, sent)
		})
	}
}

// TestFamilyMailGetsThrough pins the sends the family rule leaves alone.
func TestFamilyMailGetsThrough(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		opening *opening
	}{
		{
			name: "a child answers its parent",
			from: "child-one",
			to:   "parent",
		},
		{
			name: "a parent reaches its child",
			from: "parent",
			to:   "child-one",
		},
		{
			name: "one child reaches its sibling",
			from: "child-one",
			to:   "child-two",
		},
		{
			name: "two agents that are nobody's children",
			from: "parent",
			to:   "stranger",
		},
		{
			name: "an unrelated agent reaching a child",
			from: "stranger",
			to:   "child-one",
		},
		{
			name:    "a child answering the stranger that reached it",
			from:    "child-one",
			to:      "stranger",
			opening: &opening{from: "stranger", to: "child-one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			svc := newMailService(st, config.DefaultMaxExchange)
			family := seedFamily(t, st)

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      family[tt.from].ID,
				ToAgentID: family[tt.to].ID,
				Body:      "hello",
				ReplyTo:   sendOpening(t, st, family, tt.opening),
			})
			require.NoError(t, err)
			assert.Equal(t, family[tt.to].ID, sent.ToAgentID)
		})
	}
}

// TestSystemMailReachesAChild pins that the family rule bounds what a child may do rather than what komrad may do to it.
func TestSystemMailReachesAChild(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	family := seedFamily(t, st)

	sent, err := svc.SendSystemMail(t.Context(), family["child-one"].ID, "your parent was archived")
	require.NoError(t, err)
	assert.Equal(t, KomradSenderName, sent.FromName)
}

// TestSendNoticeRefusesAPlainKind pins that a notice must say what it is about to be told apart from ordinary mail.
func TestSendNoticeRefusesAPlainKind(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	recipient := st.SeedAgent(t, agentstore.Agent{Name: "migrations"})

	sent, err := svc.SendNotice(t.Context(), SendNoticeParams{
		ToAgentID: recipient.ID,
		Body:      "hello",
		Kind:      mailstore.KindPlain,
	})
	require.ErrorIs(t, err, ErrNoticeKindMissing)
	assert.Nil(t, sent)
}

// TestSendNoticeAboutADocumentJoinsTheUnreadOne pins that one unread notice stands for every edit of a document until it is read.
func TestSendNoticeAboutADocumentJoinsTheUnreadOne(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	ctx := t.Context()
	recipient := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	one, other := uuid.NewString(), uuid.NewString()

	notify := func(document string) *mailstore.Mail {
		t.Helper()
		sent, err := svc.SendNotice(ctx, SendNoticeParams{
			ToAgentID:     recipient.ID,
			Body:          "a person edited your document",
			Kind:          mailstore.KindDocumentEdited,
			AboutDocument: &document,
		})
		require.NoError(t, err)

		return sent
	}

	first := notify(one)
	assert.Equal(t, first.ID, notify(one).ID, "a second edit joins the unread notice")
	assert.NotEqual(t, first.ID, notify(other).ID, "an edit of another document is told apart")

	_, err := svc.ClaimMail(ctx, recipient.ID)
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, notify(one).ID, "an edit after the notice was read sends another")
}

// TestClaimMail pins that a claim is refused for an id no agent holds and takes at most one batch otherwise.
func TestClaimMail(t *testing.T) {
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
		claimed, err := svc.ClaimMail(t.Context(), uuid.NewString())
		require.ErrorIs(t, err, ErrAgentNotFound)
		assert.Empty(t, claimed)
	})

	t.Run("an agent reading its own takes one batch and is told what is left", func(t *testing.T) {
		claimed, err := svc.ClaimMail(t.Context(), recipient.ID)
		require.NoError(t, err)
		assert.Len(t, claimed.Mail, maxClaimedMail)
		assert.EqualValues(t, 1, claimed.Remaining)
	})

	t.Run("and the rest on the next read", func(t *testing.T) {
		claimed, err := svc.ClaimMail(t.Context(), recipient.ID)
		require.NoError(t, err)
		assert.Len(t, claimed.Mail, 1)
		assert.Zero(t, claimed.Remaining)
	})
}

// TestListMail pins that a listing is refused for an unknown agent and otherwise reads the half of the correspondence asked for.
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
			AgentID: uuid.NewString(),
			Limit:   10,
		})
		require.ErrorIs(t, err, ErrAgentNotFound)
		assert.Empty(t, listed)
	})

	tests := []struct {
		name     string
		agentID  string
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

			var ids []string
			for range 2 {
				sent, err := svc.SendMail(t.Context(), SendMailParams{
					ToAgentID: recipient.ID,
					Body:      "hello",
				})
				require.NoError(t, err)
				ids = append(ids, sent.ID)
			}
			if tt.withUnknown {
				ids = append(ids, uuid.NewString())
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

// TestResolveRecipientRefusals pins which sentinel each address nothing answers to, or more than one agent does, comes back with.
func TestResolveRecipientRefusals(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	book := seedAddressBook(t, st)

	tests := []struct {
		name     string
		from     string
		to       string
		wantErr  error
		wantSaid []string
	}{
		{
			name:    "an id nothing holds is not found rather than tried as a name",
			from:    book.sender.ID,
			to:      book.unheldID,
			wantErr: ErrRecipientNotFound,
		},
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
			wantSaid: []string{book.testerKomrad.ID + ` in workflow "komrad"`, book.testerDocs.ID + ` in workflow "docs"`},
		},
		{
			name:     "a name two workflows hold, from nobody",
			to:       "reviewer",
			wantErr:  ErrRecipientAmbiguous,
			wantSaid: []string{book.reviewerHere.ID + ` in workflow "gadgets"`, book.reviewerThere.ID + ` in workflow "komrad"`},
		},
		{
			name:    "a name from a sender the panel does not know",
			from:    uuid.NewString(),
			to:      "migrations",
			wantErr: ErrSenderUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ResolveRecipient(t.Context(), ResolveRecipientParams{
				From: tt.from,
				To:   tt.to,
			})
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, got)
			for _, said := range tt.wantSaid {
				assert.Contains(t, err.Error(), said)
			}
		})
	}
}

// TestResolveRecipient pins that an address resolves as an id first, then as a name in the sender's workflow, then as a name in any workflow.
func TestResolveRecipient(t *testing.T) {
	st := storetest.Open(t)
	svc := newMailService(st, config.DefaultMaxExchange)
	book := seedAddressBook(t, st)

	tests := []struct {
		name string
		from string
		to   string
		want string
	}{
		{
			name: "an id, even when another agent is named that id",
			from: book.sender.ID,
			to:   book.reviewerHere.ID,
			want: book.reviewerHere.ID,
		},
		{
			name: "an archived agent by id, for the send to refuse as archived",
			from: book.sender.ID,
			to:   book.retired.ID,
			want: book.retired.ID,
		},
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
			got, err := svc.ResolveRecipient(t.Context(), ResolveRecipientParams{
				From: tt.from,
				To:   tt.to,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.ID)
		})
	}
}

// seedAddressBook records the address book, naming one agent after an id nothing holds and another after another agent's id.
func seedAddressBook(t *testing.T, st *storetest.Stores) addressBook {
	t.Helper()

	gadgets := st.SeedWorkflow(t, workflowstore.Config{Name: "gadgets"})
	komrad := st.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	docs := st.SeedWorkflow(t, workflowstore.Config{Name: "docs"})

	seed := func(name, workflowID string) *agentstore.Agent {
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
		unheldID:      uuid.NewString(),
	}
	seed(book.unheldID, komrad.ID)
	seed(book.reviewerHere.ID, komrad.ID)

	_, err := st.Agents.Archive(t.Context(), []string{book.retired.ID})
	require.NoError(t, err)

	return book
}

func newMailService(st *storetest.Stores, maxExchange int) *MailService {
	return NewMailService(NewMailServiceParams{
		MailReadWriter: st.Mail,
		Agents:         st.Agents,
		Workflows:      st.Workflows,
		Config:         config.Mail{MaxExchange: maxExchange},
	})
}

// seedFamily records one parent with two children, an unrelated agent, and another parent's child.
func seedFamily(t *testing.T, st *storetest.Stores) map[string]agentstore.Agent {
	t.Helper()

	parent := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	otherParent := st.SeedAgent(t, agentstore.Agent{Name: "other-author"})

	seedChild := func(name, parentID string) agentstore.Agent {
		return *st.SeedAgent(t, agentstore.Agent{
			Name:          name,
			ParentAgentID: &parentID,
		})
	}

	return map[string]agentstore.Agent{
		"parent":    *parent,
		"child-one": seedChild("reviewer", parent.ID),
		"child-two": seedChild("test-runner", parent.ID),
		"cousin":    seedChild("their-reviewer", otherParent.ID),
		"stranger":  *st.SeedAgent(t, agentstore.Agent{Name: "unrelated"}),
	}
}

type opening struct {
	from, to string
}

// sendOpening records the mail a reply answers and returns its id, or nil when there is none.
func sendOpening(t *testing.T, st *storetest.Stores, family map[string]agentstore.Agent, o *opening) *string {
	t.Helper()

	if o == nil {
		return nil
	}

	sent, err := st.Mail.Send(t.Context(), mailstore.SendParams{
		FromAgentID: xptr.Ptr(family[o.from].ID),
		ToAgentID:   family[o.to].ID,
		Body:        "which column holds the refund?",
	})
	require.NoError(t, err)

	return &sent.ID
}

type exchangeParams struct {
	Child    agentstore.Agent
	Parent   agentstore.Agent
	Messages int
}

// exchange sends mail back and forth between a child and its parent, every message opening a thread of its own.
func exchange(t *testing.T, svc *MailService, params exchangeParams) {
	t.Helper()

	for at := range params.Messages {
		from, to := params.Child, params.Parent
		if at%2 == 1 {
			from, to = params.Parent, params.Child
		}

		_, err := svc.SendMail(t.Context(), SendMailParams{
			From:      from.ID,
			ToAgentID: to.ID,
			Body:      "round",
		})
		require.NoError(t, err)
	}
}

func listCappedNotices(t *testing.T, st *storetest.Stores, agentID string) []mailstore.Mail {
	t.Helper()

	listed, err := st.Mail.ListPage(t.Context(), agentID, maxPendingMail, nil)
	require.NoError(t, err)

	return xslice.Filter(listed, func(mail mailstore.Mail) bool {
		return mail.Kind == mailstore.KindExchangeCapped
	})
}
