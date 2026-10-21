package controlplane

import (
	"context"
	"log/slog"
	"net/http"

	"komrad/internal/logging"
)

// StartPath starts the engine whose path it follows.
const StartPath = "/engine/start"

// StopPath stops the engine whose path it follows.
const StopPath = "/engine/stop"

// EngineState is where an engine behind the control plane is between stopped and running.
type EngineState string

const (
	EngineRunning  EngineState = "running"
	EngineStarting EngineState = "starting"
	EngineStopping EngineState = "stopping"
	EngineStopped  EngineState = "stopped"
)

// runner starts and stops the engines behind the control plane.
type runner interface {
	// Start sets the engine starting and returns before it answers.
	Start(ctx context.Context, engine string) error
	// Stop sets the engine stopping and returns before it has stopped.
	Stop(ctx context.Context, engine string) error
	State(ctx context.Context, engine string) (EngineState, error)
}

func (h *handler) startEngine(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, runParams{
		Run:      h.runner.Start,
		Action:   "start",
		Accepted: "the engine is starting",
	})
}

func (h *handler) stopEngine(w http.ResponseWriter, r *http.Request) {
	h.run(w, r, runParams{
		Run:      h.runner.Stop,
		Action:   "stop",
		Accepted: "the engine is stopping",
	})
}

type runParams struct {
	Run      func(ctx context.Context, engine string) error
	Action   string
	Accepted string
}

// run starts or stops the engine for someone holding its grant.
func (h *handler) run(w http.ResponseWriter, r *http.Request, params runParams) {
	engine, ok := h.admit(w, r)
	if !ok {
		return
	}

	if err := params.Run(r.Context(), engine); err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to start or stop an engine",
			append(logging.Err(err), slog.String(logging.EngineName, engine), slog.String(logging.LifecycleAction, params.Action))...)
		writeMessage(w, http.StatusBadGateway, err.Error())

		return
	}

	writeMessage(w, http.StatusAccepted, params.Accepted)
}
