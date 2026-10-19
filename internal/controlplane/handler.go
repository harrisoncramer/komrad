package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"komrad/internal/interceptors"
	"komrad/internal/logging"
	"komrad/internal/store"

	"connectrpc.com/connect"
)

// grantChecker answers whether a signed-in person holds the grant on an engine.
type grantChecker interface {
	IsGranted(ctx context.Context, params store.IsGrantedParams) (bool, error)
}

// handler serves the control plane's own endpoints.
type handler struct {
	logger   *slog.Logger
	engines  map[string]string
	verifier interceptors.TokenVerifier
	grants   grantChecker
	device   *deviceFlow
}

func (h *handler) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{engine}"+DevicePath, h.startDevice)
	mux.HandleFunc("POST /{engine}"+DevicePollPath, h.pollDevice)

	return mux
}

func (h *handler) startDevice(w http.ResponseWriter, r *http.Request) {
	engine, ok := h.admit(w, r)
	if !ok {
		return
	}

	code, err := h.device.start(r.Context(), engine)
	if err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to start GitHub's device flow",
			append(logging.Err(err), slog.String(logging.EngineName, engine))...)
		writeMessage(w, http.StatusBadGateway, "the control plane failed to get a code from GitHub")

		return
	}

	writeJSON(w, http.StatusOK, code)
}

func (h *handler) pollDevice(w http.ResponseWriter, r *http.Request) {
	engine, ok := h.admit(w, r)
	if !ok {
		return
	}

	poll, err := h.device.poll(r.Context(), engine)
	if err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to poll GitHub's device flow",
			append(logging.Err(err), slog.String(logging.EngineName, engine))...)
		writeMessage(w, http.StatusBadGateway, "the control plane failed to ask GitHub whether the code was entered")

		return
	}

	writeJSON(w, http.StatusOK, poll)
}

// admit is the engine the request names, once its ID token names someone who holds the
// engine's grant.
func (h *handler) admit(w http.ResponseWriter, r *http.Request) (string, bool) {
	engine := r.PathValue("engine")
	if _, known := h.engines[engine]; !known {
		writeMessage(w, http.StatusNotFound, "the control plane has no engine by that name")

		return "", false
	}

	raw, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || raw == "" {
		writeMessage(w, http.StatusUnauthorized, "the request carries no bearer token, so run komrad login")

		return "", false
	}

	signedIn, err := h.verifier.VerifyToken(r.Context(), raw)
	if err != nil {
		status := http.StatusUnauthorized
		if connect.CodeOf(err) == connect.CodeUnavailable {
			status = http.StatusServiceUnavailable
		}
		writeMessage(w, status, err.Error())

		return "", false
	}

	granted, err := h.grants.IsGranted(r.Context(), store.IsGrantedParams{
		Engine:        engine,
		Email:         signedIn.Caller.Email,
		EmailVerified: signedIn.EmailVerified,
		Issuer:        signedIn.Caller.Issuer,
		Subject:       signedIn.Caller.Subject,
	})
	if err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelError, "failed to read a grant",
			append(logging.Err(err), slog.String(logging.EngineName, engine))...)
		writeMessage(w, http.StatusServiceUnavailable, "the control plane failed to read whether this account holds the engine's grant")

		return "", false
	}
	if !granted {
		writeMessage(w, http.StatusForbidden, "nobody has granted this account access to the engine")

		return "", false
	}

	return engine, true
}

func writeMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(w, errors.Join(errors.New("failed to encode the answer"), err).Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded) //nolint:errcheck // the caller hears a cut answer as a failed request
}
