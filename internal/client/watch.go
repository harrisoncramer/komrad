package client

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	watchv1 "weave/proto/gen/watch/v1"
	"weave/proto/gen/watch/v1/watchv1connect"

	"connectrpc.com/connect"
)

const (
	// WatchCoalesce is how long a change waits for the changes behind it before the listing goes
	// out.
	WatchCoalesce = 25 * time.Millisecond
	// WatchSilence is how long a stream may say nothing before it is taken for dead, which is
	// twice the service's heartbeat.
	WatchSilence = 30 * time.Second
	// WatchRetry is the wait before the first reopen, doubling each time up to WatchMaxRetry.
	WatchRetry    = time.Second
	WatchMaxRetry = 15 * time.Second
)

// WatchBatch is the events one stream delivered together, in the order they arrived.
type WatchBatch struct {
	Events []*watchv1.WatchEvent
}

// WatchStream is one open watch, which the stream a connect client answers with is.
type WatchStream interface {
	Receive() bool
	Msg() *watchv1.WatchEvent
	Err() error
	Close() error
}

// WatchParams says how a watch is opened and who is told what it carries.
type WatchParams struct {
	// Open starts one stream.
	Open func(ctx context.Context) (WatchStream, error)
	// OnBatch is handed each batch of events, the first of a stream opening with its snapshot.
	OnBatch func(WatchBatch)
	// OnLost is told a stream has ended, with the error that ended it or nil for a close the
	// service chose, before the next one is opened.
	OnLost func(error)
	// Coalesce, Silence, Retry and MaxRetry take their Watch defaults when zero.
	Coalesce time.Duration
	Silence  time.Duration
	Retry    time.Duration
	MaxRetry time.Duration
}

// OpenWatch is the Open a connect client answers.
func OpenWatch(watches watchv1connect.WatchServiceClient) func(ctx context.Context) (WatchStream, error) {
	return func(ctx context.Context) (WatchStream, error) {
		stream, err := watches.Watch(ctx, connect.NewRequest(&watchv1.WatchRequest{}))
		if err != nil {
			return nil, fmt.Errorf("failed to open the watch: %w", err)
		}

		return stream, nil
	}
}

// Watch holds a watch open for the life of ctx, opening a fresh one after every close.
func Watch(ctx context.Context, params WatchParams) {
	params = params.withDefaults()
	wait := params.Retry

	for {
		delivered, err := watchOnce(ctx, params)
		if ctx.Err() != nil {
			return
		}

		params.OnLost(err)

		if delivered {
			wait = params.Retry
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		wait = min(wait*2, params.MaxRetry)
	}
}

func (p WatchParams) withDefaults() WatchParams {
	p.Coalesce = cmp.Or(p.Coalesce, WatchCoalesce)
	p.Silence = cmp.Or(p.Silence, WatchSilence)
	p.Retry = cmp.Or(p.Retry, WatchRetry)
	p.MaxRetry = cmp.Or(p.MaxRetry, WatchMaxRetry)

	if p.OnLost == nil {
		p.OnLost = func(error) {}
	}

	return p
}

// watchOnce reads one stream to its end and answers with whether it delivered anything and
// what ended it.
func watchOnce(ctx context.Context, params WatchParams) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)

	stream, err := params.Open(ctx)
	if err != nil {
		cancel()

		return false, err
	}

	// The reader is let go before the stream is closed under it.
	defer func() {
		cancel()

		_ = stream.Close() //nolint:errcheck // the stream is being let go of
	}()

	events := make(chan *watchv1.WatchEvent)
	ended := make(chan error, 1)

	go func() {
		for stream.Receive() {
			select {
			case events <- stream.Msg():
			case <-ctx.Done():
				return
			}
		}

		ended <- stream.Err()
	}()

	var (
		batch     []*watchv1.WatchEvent
		delivered bool
		flush     <-chan time.Time
	)

	// A stream that ends inside the window still hands over what it delivered.
	hand := func() {
		if len(batch) == 0 {
			return
		}

		delivered = true
		params.OnBatch(WatchBatch{Events: batch})
		batch, flush = nil, nil
	}

	silence := time.NewTimer(params.Silence)
	defer silence.Stop()

	for {
		select {
		case <-ctx.Done():
			return delivered, ctx.Err()
		case err := <-ended:
			hand()

			return delivered, err
		case <-silence.C:
			hand()

			return delivered, errors.New("the watch said nothing for " + params.Silence.String())
		case <-flush:
			hand()
		case event := <-events:
			if !silence.Stop() {
				<-silence.C
			}

			silence.Reset(params.Silence)

			if _, heartbeat := event.GetEvent().(*watchv1.WatchEvent_Heartbeat); heartbeat || event.GetEvent() == nil {
				continue
			}

			batch = append(batch, event)

			if flush == nil {
				flush = time.After(params.Coalesce)
			}
		}
	}
}
