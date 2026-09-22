package dbtest

import (
	"context"
	"testing"
	"time"

	"weave/db"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// notificationWait is how long a test waits for a notification before failing.
const notificationWait = 5 * time.Second

// Notifications is what a database raises on the watch channel, in the order it raised them.
type Notifications struct {
	payloads chan string
}

// Listen opens a connection to the database url names that hears every notification the
// schema raises on the watch channel from then on.
func Listen(t *testing.T, url string) *Notifications {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, "LISTEN "+db.WatchChannel)
	require.NoError(t, err)

	notifications := &Notifications{payloads: make(chan string, 1024)}
	done := make(chan struct{})
	go func() {
		defer close(done)

		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				return
			}
			notifications.payloads <- notification.Payload
		}
	}()

	t.Cleanup(func() {
		cancel()
		<-done
		_ = conn.Close(t.Context())
	})

	return notifications
}

// Next is the next payload raised, failing the test when none arrives in time.
func (n *Notifications) Next(t *testing.T) string {
	t.Helper()

	select {
	case payload := <-n.payloads:
		return payload
	case <-time.After(notificationWait):
		t.Fatal("no notification arrived")

		return ""
	}
}
