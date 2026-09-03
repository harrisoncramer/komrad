package server

import (
	"context"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	runstore "weave/internal/store/run"
	schedulestore "weave/internal/store/schedule"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
)

type stores struct {
	pool        *store.Pool
	agents      *agentstore.Store
	workflows   *workflowstore.Store
	schedules   *schedulestore.Store
	transcripts *transcriptstore.Store
	mail        *mailstore.Store
	review      *reviewstore.Store
	runs        *runstore.Store
}

func openStores(ctx context.Context, databaseURL string) (*stores, error) {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	db := pool.GetDB()

	return &stores{
		pool:        pool,
		agents:      agentstore.New(db),
		workflows:   workflowstore.New(db),
		schedules:   schedulestore.New(db),
		transcripts: transcriptstore.New(db),
		mail:        mailstore.New(db),
		review:      reviewstore.New(db),
		runs:        runstore.New(db),
	}, nil
}
