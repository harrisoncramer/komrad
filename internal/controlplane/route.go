package controlplane

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"komrad/internal/logging"
	"komrad/internal/server"
	"komrad/lib/xhttp"
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

// NewEngineTransport reaches engines over HTTP/2 without TLS, pinging each connection so a
// stream to an engine that vanished drops.
func NewEngineTransport() *http.Transport {
	transport := xhttp.NewStreamTransport()
	transport.Proxy = nil
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetUnencryptedHTTP2(true)

	return transport
}

// newEngineProxy sends a request under an engine's path to that engine, with the path cut off.
func (h *handler) newEngineProxy(transport http.RoundTripper) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			name := r.In.PathValue("engine")
			prefix := "/" + name
			r.SetURL(&url.URL{Scheme: "http", Host: h.engines[name].Addr})
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, prefix)
			r.Out.URL.RawPath = strings.TrimPrefix(r.In.URL.RawPath, prefix)
		},
		Transport:    transport,
		ErrorHandler: h.answerUnreachable,
		ErrorLog:     slog.NewLogLogger(h.logger.Handler(), slog.LevelWarn),
	}
}

func (h *handler) forward(w http.ResponseWriter, r *http.Request) {
	if _, known := h.engines[r.PathValue("engine")]; !known {
		writeMessage(w, http.StatusNotFound, "the control plane has no engine by that name")

		return
	}

	h.proxy.ServeHTTP(w, r)
}

// answerUnreachable answers for an engine the control plane failed to reach, saying when it is
// stopped or starting to someone holding its grant.
func (h *handler) answerUnreachable(w http.ResponseWriter, r *http.Request, failure error) {
	if r.Context().Err() != nil {
		return
	}

	name := r.PathValue("engine")
	state, err := h.runner.State(r.Context(), name)
	if err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to read whether an engine is running",
			append(logging.Err(err), slog.String(logging.EngineName, name))...)
	}
	if err != nil || (state != EngineStopped && state != EngineStarting) {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to reach an engine",
			append(logging.Err(failure), slog.String(logging.EngineName, name))...)
		writeMessage(w, http.StatusBadGateway, "the control plane failed to reach the engine")

		return
	}

	if _, ok := h.admit(w, r); !ok {
		return
	}

	status, message := server.HealthStopped, "the engine is stopped, so start it from the panel or with komrad engine start"
	if state == EngineStarting {
		status, message = server.HealthStarting, "the engine is starting, so try again in a moment"
	}

	if r.URL.Path == server.HealthPath {
		writeJSON(w, http.StatusServiceUnavailable, server.Health{Status: status})

		return
	}

	writeMessage(w, http.StatusServiceUnavailable, message)
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
