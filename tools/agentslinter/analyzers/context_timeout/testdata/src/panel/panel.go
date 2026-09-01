package panel

import (
	"context"
	"time"
)

const requestTimeout = 10 * time.Second

func bounded() {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	_ = ctx
}

func unbounded() {
	ctx := context.Background() // want `context.Background must be started with context.WithTimeout, so the call cannot wait forever`
	_ = ctx
}

func placeholder() {
	ctx := context.TODO() // want `context.TODO must be started with context.WithTimeout, so the call cannot wait forever`
	_ = ctx
}

func inlineDuration() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // want `the timeout must be a named constant rather than a duration written inline`
	defer cancel()
	_ = ctx
}

func inherited(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	_ = ctx
}

func bareDuration() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) // want `the timeout must be a named constant rather than a duration written inline`
	defer cancel()
	_ = ctx
}
