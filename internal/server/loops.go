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

// TranscriptSyncInterval is how often an agent's log is read for new turns, which decides how
// soon a conversation is searchable.
const TranscriptSyncInterval = 15 * time.Second

const CwdSyncInterval = 15 * time.Second

// PRArchiveInterval is how often recorded pull requests are asked about, in minutes rather than
// seconds because every pass spends somebody's GitHub quota.
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

func buildLoops(params buildLoopsParams) []Loop {
	return []Loop{
		{
			Name:     ScheduleReconcileLoop,
			Interval: ScheduleReconcileInterval,
			Pass:     internaljobs.NewScheduler(params.Stores.schedules, params.Periodic).Reconcile,
		},
		{
			Name:     ReconcileLoop,
			Interval: ReconcileInterval,
			Pass:     params.Services.reconciler.Reconcile,
		},
		{
			Name:     MailDeliveryLoop,
			Interval: MailDeliveryInterval,
			Pass:     params.Services.delivery.DeliverAll,
		},
		{
			Name:     TranscriptSyncLoop,
			Interval: TranscriptSyncInterval,
			Pass: agents.NewTranscriptSyncer(agents.NewTranscriptSyncerParams{
				Agents:      params.Stores.agents,
				Transcripts: params.Stores.transcripts,
				Harnesses:   params.Harnesses,
			}).Sync,
		},
		{
			Name:     CwdSyncLoop,
			Interval: CwdSyncInterval,
			Pass:     agents.NewCwdSyncer(params.Stores.agents).Sync,
		},
		{
			Name:     PRArchiveLoop,
			Interval: PRArchiveInterval,
			Pass: agents.NewPRArchiver(agents.NewPRArchiverParams{
				Agents:       params.Stores.agents,
				Workflows:    params.Stores.workflows,
				Archiver:     params.Services.lifecycle,
				PullRequests: agents.NewGHPullRequests(),
			}).Sync,
		},
	}
}
