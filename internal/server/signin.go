package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/interceptors"
	"komrad/internal/logging"
)

type newPublicHandlerParams struct {
	Logger *slog.Logger
	SignIn config.SignIn
	Access interceptors.AccessChecker
	Mux    http.Handler
}

// newPublicHandler serves the network: the sign-in route to anyone, nothing that only this
// machine may reach, and everything else to a caller signed in with access.
func newPublicHandler(params newPublicHandlerParams) http.Handler {
	guard := interceptors.NewSignInGuard(interceptors.NewSignInGuardParams{
		Logger: params.Logger,
		Verifier: interceptors.NewProviderVerifier(interceptors.NewProviderVerifierParams{
			Issuer:   params.SignIn.Issuer,
			ClientID: params.SignIn.ClientID,
		}),
		Access: params.Access,
		Next:   params.Mux,
	})

	public := http.NewServeMux()
	public.Handle("GET "+engine.SignInPath, newSignInHandler(params.SignIn))
	public.Handle(StopPath, http.NotFoundHandler())
	public.Handle("/mcp", http.NotFoundHandler())
	public.Handle("/", guard)

	return public
}

func newSignInHandler(signIn config.SignIn) http.Handler {
	answer := engine.SignInProvider{
		Issuer:       signIn.Issuer,
		ClientID:     signIn.ClientID,
		ClientSecret: signIn.ClientSecret,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(answer); err != nil {
			ctx := r.Context()
			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to write the sign-in provider", logging.Err(err)...)
		}
	})
}
