package events_test

import (
	"context"
	"encoding/json"
	"testing"

	"komrad/config"
	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	"komrad/internal/storetest"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pinged struct {
	Name string `json:"name"`
}

func (pinged) Topic() string {
	return "pinged"
}

type followUp struct {
	Name string
}

func (followUp) Kind() string {
	return "follow_up"
}

// TestRouterHandsEachEventToItsTopic covers the handlers of every other topic hearing nothing.
func TestRouterHandsEachEventToItsTopic(t *testing.T) {
	stores := storetest.Open(t)
	var heard []string
	handler := events.HandlerFor[pinged](func(_ context.Context, _ store.DBTX, _ *mail.MailService, event pinged) ([]river.JobArgs, error) {
		heard = append(heard, event.Name)

		return []river.JobArgs{followUp(event)}, nil
	})
	router := events.NewRouter(events.NewRouterParams{
		Mailer: mail.NewMailService(mail.NewMailServiceParams{
			MailReadWriter: stores.Mail,
			Agents:         stores.Agents,
			Config:         config.Mail{},
		}),
		Handlers: []events.Handler{handler},
	})

	event, err := events.NewEvent(pinged{Name: "a"})
	require.NoError(t, err)

	followUps, err := router.Route(t.Context(), stores.Pool.GetDB(), *event)
	require.NoError(t, err)
	assert.Equal(t, []river.JobArgs{followUp{Name: "a"}}, followUps)
	assert.Equal(t, []string{"a"}, heard)

	_, err = router.Route(t.Context(), stores.Pool.GetDB(), events.Event{Topic: "pinged", Payload: json.RawMessage(`{"name": 1}`)})
	require.Error(t, err)
	assert.Equal(t, []string{"a"}, heard)

	_, err = router.Route(t.Context(), stores.Pool.GetDB(), events.Event{Topic: "unheard", Payload: json.RawMessage(`{}`)})
	require.ErrorIs(t, err, events.ErrUnknownTopic)
}
