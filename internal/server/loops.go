package server

import (
	"time"

	"weave/internal/agents"
	"weave/internal/harness"
	"weave/internal/jobs"
	"weave/internal/jobs/internaljobs"
)

const ReconcileInterval = 2 * time.Second

const ScheduleReconcileInterval = 10 * time.Second

const MailDeliveryInterval = 2 * time.Second

// TranscriptSyncInterval is how often an agent's log is read for new turns, which is also how
// long the unread mark can lag what the agent has actually written.
const TranscriptSyncInterval = 5 * time.Second

const CwdSyncInterval = 15 * time.Second

// PRArchiveInterval is how often recorded pull requests are asked about, in minutes rather
// than seconds because every pass spends somebody's GitHub quota.
const PRArchiveInterval = 2 * time.Minute

const (
	ScheduleReconcileLoop = "schedule reconcile"
	ReconcileLoop         = "reconcile"
	MailDeliveryLoop      = "mail delivery"
	TranscriptSyncLoop    = "transcript sync"
	CwdSyncLoop           = "cwd sync"
	PRArchiveLoop         = "pr archive"
)

type buildLoopsParams struct {
	Harnesses *harness.Registry
	Stores    *stores
	Services  *services
	Periodic  *jobs.RiverPeriodic
}

func buildLoops(p buildLoopsParams) []Loop {
	return []Loop{
		{
			Name:     ScheduleReconcileLoop,
			Interval: ScheduleReconcileInterval,
			Pass:     internaljobs.NewScheduler(p.Stores.schedules, p.Periodic).Reconcile,
		},
		{
			Name:     ReconcileLoop,
			Interval: ReconcileInterval,
			Pass:     p.Services.reconciler.Reconcile,
		},
		{
			Name:     MailDeliveryLoop,
			Interval: MailDeliveryInterval,
			Pass:     p.Services.delivery.DeliverAll,
		},
		{
			Name:     TranscriptSyncLoop,
			Interval: TranscriptSyncInterval,
			Pass: agents.NewTranscriptSyncer(agents.TranscriptSyncerParams{
				Agents:      p.Stores.agents,
				Transcripts: p.Stores.transcripts,
				Harnesses:   p.Harnesses,
			}).Sync,
		},
		{
			Name:     CwdSyncLoop,
			Interval: CwdSyncInterval,
			Pass:     agents.NewCwdSyncer(p.Stores.agents).Sync,
		},
		{
			Name:     PRArchiveLoop,
			Interval: PRArchiveInterval,
			Pass: agents.NewPRArchiver(agents.PRArchiverParams{
				Agents:       p.Stores.agents,
				Workflows:    p.Stores.workflows,
				Archiver:     p.Services.lifecycle,
				PullRequests: agents.NewGHPullRequests(),
			}).Sync,
		},
	}
}
