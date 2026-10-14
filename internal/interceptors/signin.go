package interceptors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/rpcerr"
	"komrad/internal/store"
	"komrad/lib/clock"

	"connectrpc.com/connect"
)

// accessRecheck is how long a granted caller is taken at its word before the database is asked
// again, which bounds how long a revoke takes to land.
const accessRecheck = time.Minute

var (
	errNoToken          = errors.New("the request carries no bearer token, so run komrad login")
	errEmailUnverified  = errors.New("the provider has not verified this email, so it cannot claim a grant")
	errNoAccess         = errors.New("nobody has granted this account access to the engine")
	errTokenExpired     = errors.New("the token this request signed in with ran out")
	errAccessRevoked    = errors.New("access to the engine was revoked")
	errAnotherSignIn    = errors.New("the token names someone other than who opened the request")
	errAccessUnreadable = errors.New("failed to read whether this account has access")
)

// refusalStatuses is the http status a caller that does not speak connect hears for each refusal.
var refusalStatuses = map[connect.Code]int{
	connect.CodeUnauthenticated:  http.StatusUnauthorized,
	connect.CodePermissionDenied: http.StatusForbidden,
	connect.CodeUnavailable:      http.StatusServiceUnavailable,
}

// signedIn is who a verified ID token names, and when it runs out.
type signedIn struct {
	Caller        caller.Caller
	EmailVerified bool
	Expiry        time.Time
}

// tokenVerifier checks an ID token's signature, issuer, audience and expiry.
type tokenVerifier interface {
	VerifyToken(ctx context.Context, raw string) (*signedIn, error)
}

// AccessChecker answers whether a signed-in caller has access, and records a first sign-in.
type AccessChecker interface {
	ClaimAccess(ctx context.Context, params store.ClaimAccessParams) (bool, error)
	IsAccessGranted(ctx context.Context, issuer, subject string) (bool, error)
}

// NewSignInGuardParams is what the guard checks each request against.
type NewSignInGuardParams struct {
	Logger   *slog.Logger
	Verifier tokenVerifier
	Access   AccessChecker
	Next     http.Handler
}

// NewSignInGuard refuses a request without a valid ID token from someone granted access, and
// holds a granted request only as long as its token lasts and its access stands.
func NewSignInGuard(params NewSignInGuardParams) http.Handler {
	return &signInGuard{
		logger:   params.Logger,
		verifier: params.Verifier,
		access:   params.Access,
		next:     params.Next,
		granted:  make(map[string]time.Time),
		errors:   connect.NewErrorWriter(),
	}
}

type signInGuard struct {
	logger   *slog.Logger
	verifier tokenVerifier
	access   AccessChecker
	next     http.Handler
	errors   *connect.ErrorWriter
	mu       sync.Mutex
	granted  map[string]time.Time
}

// ServeHTTP checks the bearer token and the caller's access, then serves the request as them.
func (g *signInGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Header.Del(caller.AgentHeader)
	r.Header.Del(caller.SessionHeader)

	ctx := r.Context()

	signedIn, err := g.signIn(ctx, r.Header.Get("Authorization"))
	if err != nil {
		g.refuse(w, r, err)

		return
	}

	ctx, cancel := context.WithCancelCause(caller.With(ctx, signedIn.Caller))
	defer cancel(nil)

	held := &heldRequest{
		guard:    g,
		signedIn: *signedIn,
		cancel:   cancel,
	}
	ctx = caller.WithRenewal(ctx, held.renew)

	go held.hold(ctx)

	g.next.ServeHTTP(w, r.WithContext(ctx))
}

// signIn verifies the bearer token and lets its caller in when they have access.
func (g *signInGuard) signIn(ctx context.Context, authorization string) (*signedIn, error) {
	raw, found := strings.CutPrefix(authorization, "Bearer ")
	if !found || raw == "" {
		return nil, rpcerr.Unauthenticated(errNoToken)
	}

	signedIn, err := g.verifier.VerifyToken(ctx, raw)
	if err != nil {
		return nil, err
	}

	if g.isGrantedRecently(signedIn.Caller) {
		return signedIn, nil
	}

	if err := g.admit(ctx, *signedIn); err != nil {
		return nil, err
	}

	return signedIn, nil
}

// admit asks the database whether the caller has access, claiming a grant for their verified
// email the first time they sign in.
func (g *signInGuard) admit(ctx context.Context, signedIn signedIn) error {
	from := signedIn.Caller

	granted, err := g.access.IsAccessGranted(ctx, from.Issuer, from.Subject)
	if err != nil {
		return rpcerr.Unavailable(fmt.Errorf("%w: %w", errAccessUnreadable, err))
	}

	if !granted {
		if !signedIn.EmailVerified {
			return rpcerr.Unauthenticated(errEmailUnverified)
		}

		claimed, err := g.access.ClaimAccess(ctx, store.ClaimAccessParams{
			Email:   from.Email,
			Issuer:  from.Issuer,
			Subject: from.Subject,
		})
		if err != nil {
			return rpcerr.Unavailable(fmt.Errorf("%w: %w", errAccessUnreadable, err))
		}

		if !claimed {
			return rpcerr.New(fail.KindDenied, errNoAccess)
		}
	}

	g.mu.Lock()
	g.granted[grantKey(from)] = time.Now()
	g.mu.Unlock()

	return nil
}

func (g *signInGuard) isGrantedRecently(from caller.Caller) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	at, found := g.granted[grantKey(from)]

	return found && time.Since(at) < accessRecheck
}

// forget drops a caller's cached grant, so the next request asks the database.
func (g *signInGuard) forget(from caller.Caller) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.granted, grantKey(from))
}

func (g *signInGuard) refuse(w http.ResponseWriter, r *http.Request, err error) {
	g.logger.LogAttrs(r.Context(), slog.LevelWarn, "refused a request that did not sign in",
		slog.String(logging.HTTPMethod, r.Method),
		slog.String(logging.URLPath, r.URL.Path),
		slog.String(logging.ClientAddress, r.RemoteAddr),
		slog.String(logging.ErrorMessage, err.Error()),
	)

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		connectErr = connect.NewError(connect.CodeUnauthenticated, err) //nolint:forbidigo // a refusal with no code of its own is a caller who did not sign in
	}

	if g.errors.IsSupported(r) {
		_ = g.errors.Write(w, r, connectErr) //nolint:errcheck // a caller that hung up is refused already

		return
	}

	http.Error(w, connectErr.Message(), connectStatus(connectErr.Code()))
}

func connectStatus(code connect.Code) int {
	if status, found := refusalStatuses[code]; found {
		return status
	}

	return http.StatusUnauthorized
}

func grantKey(from caller.Caller) string {
	return from.Issuer + "\x00" + from.Subject
}

// heldRequest is one granted request, ended when its token runs out or its access goes.
type heldRequest struct {
	guard    *signInGuard
	cancel   context.CancelCauseFunc
	mu       sync.Mutex
	signedIn signedIn
}

// hold re-checks the caller's access every minute and ends the request when the token runs out.
func (h *heldRequest) hold(ctx context.Context) {
	for {
		wait := min(time.Until(h.getExpiry()), accessRecheck)
		clock.Sleep(ctx, wait)
		if ctx.Err() != nil {
			return
		}

		if !time.Now().Before(h.getExpiry()) {
			h.cancel(errTokenExpired)

			return
		}

		from := h.getSignedIn().Caller

		granted, err := h.guard.access.IsAccessGranted(ctx, from.Issuer, from.Subject)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			h.cancel(fmt.Errorf("%w: %w", errAccessUnreadable, err))

			return
		}

		if !granted {
			h.guard.forget(from)
			h.cancel(errAccessRevoked)

			return
		}
	}
}

// renew takes a fresh token for the same caller, so the request outlives the one it opened with.
func (h *heldRequest) renew(ctx context.Context, raw string) error {
	fresh, err := h.guard.verifier.VerifyToken(ctx, raw)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if grantKey(fresh.Caller) != grantKey(h.signedIn.Caller) {
		return rpcerr.New(fail.KindDenied, errAnotherSignIn)
	}

	h.signedIn.Expiry = fresh.Expiry

	return nil
}

func (h *heldRequest) getExpiry() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.signedIn.Expiry
}

func (h *heldRequest) getSignedIn() signedIn {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.signedIn
}
