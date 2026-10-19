package controlplane

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/github"
	"komrad/internal/logging"
)

// gitHost is the host git reaches GitHub on, which takes a login as basic auth.
const gitHost = "github.com"

const (
	handshakeTimeout  = 10 * time.Second
	readHeaderTimeout = 30 * time.Second
	idleTimeout       = 90 * time.Second
)

// noLoginMessage is what an engine whose person has not authorized GitHub hears.
const noLoginMessage = "This engine has no GitHub login. Authorize GitHub from the panel, then try again."

// loginStore keeps each engine's GitHub login.
type loginStore interface {
	// Get is the engine's login, or forge.ErrNoCredential.
	Get(ctx context.Context, engine string) (forge.Secret, error)
	Set(ctx context.Context, engine string, login forge.Secret) error
}

// forgeProxy takes CONNECT requests from engines for GitHub's hosts and sends what comes
// through each tunnel to GitHub with the engine's GitHub login.
type forgeProxy struct {
	logger    *slog.Logger
	engines   map[string][sha256.Size]byte
	tlsConfig *tls.Config
	tunnels   *tunnelListener
	inner     *http.Server
	served    chan error
}

// newForgeProxyParams is what the forge proxy checks engines against and sends their requests on.
type newForgeProxyParams struct {
	Logger *slog.Logger
	// Engines is each engine's forge proxy credential keyed by its name.
	Engines map[string]string
	Logins  loginStore
	CA      *tls.Certificate
	// Upstream sends requests to GitHub.
	Upstream http.RoundTripper
	Now      func() time.Time
}

// newForgeProxy starts serving the tunnels it accepts, until Close.
func newForgeProxy(params newForgeProxyParams) *forgeProxy {
	engines := make(map[string][sha256.Size]byte, len(params.Engines))
	for name, credential := range params.Engines {
		engines[name] = sha256.Sum256([]byte(credential))
	}

	signer := newLeafSigner(params.CA, params.Now)
	tunnels := newTunnelListener()

	upstream := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			tunnel := getTunnel(r.In.Context())
			r.SetURL(&url.URL{Scheme: "https", Host: tunnel.host})
			r.Out.Header.Set("Authorization", formatLogin(tunnel.host, getLogin(r.In.Context())))
		},
		Transport:     params.Upstream,
		FlushInterval: -1,
		ErrorLog:      slog.NewLogLogger(params.Logger.Handler(), slog.LevelWarn),
	}

	proxy := &forgeProxy{
		logger:  params.Logger,
		engines: engines,
		tlsConfig: &tls.Config{
			GetCertificate: signer.getCertificate,
			NextProtos:     []string{"http/1.1"},
			MinVersion:     tls.VersionTLS12,
		},
		tunnels: tunnels,
		served:  make(chan error, 1),
	}

	proxy.inner = &http.Server{
		Handler:           &loginHandler{logins: params.Logins, logger: params.Logger, next: upstream},
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(params.Logger.Handler(), slog.LevelWarn),
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			terminated, ok := conn.(*tunnelConn)
			if !ok {
				return ctx
			}

			return withTunnel(ctx, terminated.tunnel)
		},
	}

	go func() { proxy.served <- proxy.inner.Serve(tunnels) }()

	return proxy
}

// ServeHTTP answers a CONNECT from an engine and serves the tunnel it opens.
func (p *forgeProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	engine, ok := p.authenticate(r.Header.Get("Proxy-Authorization"))
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="komrad forge proxy"`)
		http.Error(w, "the forge proxy needs this engine's forge proxy credential", http.StatusProxyAuthRequired)

		return
	}

	if r.Method != http.MethodConnect {
		http.Error(w, "the forge proxy takes CONNECT only", http.StatusMethodNotAllowed)

		return
	}

	host, port, err := net.SplitHostPort(r.Host)
	host = strings.ToLower(host)
	if err != nil || port != "443" || !slices.Contains(github.ForgeProxyHosts, host) {
		http.Error(w, "the forge proxy reaches GitHub's hosts on 443 only", http.StatusForbidden)

		return
	}

	conn, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		p.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to take over a forge proxy connection",
			append(logging.Err(err), slog.String(logging.EngineName, engine))...)

		return
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close() //nolint:errcheck // the engine hears the tunnel close either way

		return
	}

	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		_ = conn.Close() //nolint:errcheck // the engine hears the tunnel close either way

		return
	}

	p.serveTunnel(r.Context(), tunnel{engine: engine, host: host}, &bufferedConn{Conn: conn, reader: buffered.Reader})
}

// serveTunnel terminates TLS on the tunnel and hands it to the server for GitHub's hosts.
func (p *forgeProxy) serveTunnel(ctx context.Context, opened tunnel, conn net.Conn) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handshakeTimeout)
	defer cancel()

	terminated := tls.Server(conn, p.tlsConfig)
	if err := terminated.HandshakeContext(ctx); err != nil {
		p.logger.LogAttrs(ctx, slog.LevelDebug, "a forge proxy tunnel failed its handshake",
			append(logging.Err(err), slog.String(logging.EngineName, opened.engine), slog.String(logging.ForgeHost, opened.host))...)
		_ = terminated.Close() //nolint:errcheck // the engine hears the tunnel close either way

		return
	}

	if !p.tunnels.push(&tunnelConn{Conn: terminated, tunnel: opened}) {
		_ = terminated.Close() //nolint:errcheck // the engine hears the tunnel close either way
	}
}

// authenticate is the engine whose name and forge proxy credential the header carries.
func (p *forgeProxy) authenticate(header string) (string, bool) {
	encoded, ok := strings.CutPrefix(header, "Basic ")
	if !ok {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", false
	}

	engine, credential, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return "", false
	}

	want, known := p.engines[engine]
	got := sha256.Sum256([]byte(credential))
	if subtle.ConstantTimeCompare(want[:], got[:]) != 1 || !known {
		return "", false
	}

	return engine, true
}

// Close stops serving tunnels and closes those open.
func (p *forgeProxy) Close() error {
	err := errors.Join(p.tunnels.Close(), p.inner.Close())
	if served := <-p.served; !errors.Is(served, http.ErrServerClosed) && !errors.Is(served, net.ErrClosed) {
		return errors.Join(err, served)
	}

	return err
}

// formatLogin is the Authorization header GitHub takes the login in on a host.
func formatLogin(host string, login forge.Secret) string {
	if host == gitHost {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+login.Expose()))
	}

	return "Bearer " + login.Expose()
}

// loginHandler reads the engine's login before a request goes to GitHub, and answers for
// GitHub when there is none.
type loginHandler struct {
	logins loginStore
	logger *slog.Logger
	next   http.Handler
}

// ServeHTTP sends the request on with the engine's login, or answers 401 without one.
func (h *loginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	opened := getTunnel(r.Context())

	login, err := h.logins.Get(r.Context(), opened.engine)
	if errors.Is(err, forge.ErrNoCredential) {
		writeMessage(w, http.StatusUnauthorized, noLoginMessage)

		return
	}
	if err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelError, "failed to read an engine's GitHub login",
			append(logging.Err(err), slog.String(logging.EngineName, opened.engine))...)
		http.Error(w, "the forge proxy failed to read this engine's GitHub login", http.StatusBadGateway)

		return
	}

	h.next.ServeHTTP(w, r.WithContext(withLogin(r.Context(), login)))
}

// tunnel is the engine that opened a tunnel and the GitHub host it asked for.
type tunnel struct {
	engine string
	host   string
}

type tunnelKey struct{}

func withTunnel(ctx context.Context, opened tunnel) context.Context {
	return context.WithValue(ctx, tunnelKey{}, opened)
}

func getTunnel(ctx context.Context) tunnel {
	opened, ok := ctx.Value(tunnelKey{}).(tunnel)
	if !ok {
		return tunnel{}
	}

	return opened
}

type loginKey struct{}

func withLogin(ctx context.Context, login forge.Secret) context.Context {
	return context.WithValue(ctx, loginKey{}, login)
}

func getLogin(ctx context.Context) forge.Secret {
	login, ok := ctx.Value(loginKey{}).(forge.Secret)
	if !ok {
		return forge.Secret{}
	}

	return login
}

// tunnelConn is a terminated tunnel and who opened it.
type tunnelConn struct {
	net.Conn
	tunnel tunnel
}

// bufferedConn reads what the server buffered past the CONNECT before reading the connection.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

// Read drains the buffer first.
func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// tunnelListener hands the server each tunnel the forge proxy accepted.
type tunnelListener struct {
	conns  chan net.Conn
	done   chan struct{}
	closed sync.Once
}

func newTunnelListener() *tunnelListener {
	return &tunnelListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

// push hands a tunnel to the server, and is false once the listener is closed.
func (l *tunnelListener) push(conn net.Conn) bool {
	select {
	case l.conns <- conn:
		return true
	case <-l.done:
		return false
	}
}

// Accept waits for the next tunnel.
func (l *tunnelListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close stops handing out tunnels.
func (l *tunnelListener) Close() error {
	l.closed.Do(func() { close(l.done) })

	return nil
}

// Addr names no address, since tunnels arrive through CONNECT.
func (l *tunnelListener) Addr() net.Addr {
	return tunnelAddr{}
}

type tunnelAddr struct{}

// Network names the listener's kind.
func (tunnelAddr) Network() string {
	return "tunnel"
}

// String names the listener.
func (tunnelAddr) String() string {
	return "forge proxy tunnels"
}
