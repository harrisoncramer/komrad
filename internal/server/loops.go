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

// PullRequestSyncInterval is how often the forges are asked what changed, which costs a request
// per repository live agents sit on and is what makes a pull request appear without a nudge.
const PullRequestSyncInterval = time.Minute

const (
	ScheduleReconcileLoop = "schedule reconcile"
	ReconcileLoop         = "reconcile"
	MailDeliveryLoop      = "mail delivery"
	TranscriptSyncLoop    = "transcript sync"
	CwdSyncLoop           = "cwd sync"
	PullRequestSyncLoop   = "pull request sync"
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
			Pass:     params.Services.cwdSync.Sync,
		},
		{
			Name:     PullRequestSyncLoop,
			Interval: PullRequestSyncInterval,
			Pass:     params.Services.pullRequestSync.Sync,
		},
	}
}
