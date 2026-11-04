// Package api implements the RSCON HTTPS control plane.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/ca"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/observability"
	"github.com/rsdenck/rscon/internal/recording"
	"github.com/rsdenck/rscon/internal/store"
)

// Config is the HTTP control plane configuration.
type Config struct {
	ListenAddress      string
	PublicURL          string
	TLSCertFile        string
	TLSKeyFile         string
	SessionTTL         time.Duration
	LoginRateLimit     int
	LoginRateWindow    time.Duration
	LockoutThreshold   int
	LockoutDuration    time.Duration
	DefaultCertTTL     time.Duration
	MaxCertTTL         time.Duration
	MaxBodyBytes       int64
	RateLimitPerMinute int
	TrustedProxyCIDRs  []string
	CORSOrigins        []string
	AllowSelfApproval  bool
	SecureCookies      bool
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCAuthURL        string
	OIDCTokenURL       string
	OIDCUserInfoURL    string
	OIDCRedirectURL    string
	InsecureSkipVerify bool
}

// Gateway is the local SSH data plane, if the daemon hosts one.
type Gateway interface {
	TerminateSession(id string) bool
	Addr() string
}

// Options are the API dependencies.
type Options struct {
	Config     Config
	Store      *store.Store
	Authz      *authz.Service
	Signer     ca.Signer
	Recordings recording.Store
	Audit      *audit.Service
	Metrics    *observability.Metrics
	Logger     *slog.Logger
	Gateway    Gateway
	Web        http.Handler
	Version    string
}

// Server is the control plane HTTP server.
type Server struct {
	cfg        Config
	store      *store.Store
	authz      *authz.Service
	signer     ca.Signer
	recs       recording.Store
	audit      *audit.Service
	metrics    *observability.Metrics
	log        *slog.Logger
	gateway    Gateway
	web        http.Handler
	version    string
	http       *http.Server
	trusted    []*net.IPNet
	limiter    *rateLimiter
	loginLimit *rateLimiter
	secure     bool
}

type contextKey string

const (
	ctxUser      contextKey = "user"
	ctxPrincipal contextKey = "principal"
	ctxToken     contextKey = "token"
	ctxRequestID contextKey = "request_id"
)

// New validates dependencies and constructs the API server.
func New(opts Options) (*Server, error) {
	if opts.Config.ListenAddress == "" {
		return nil, errors.New("api: listen address is required")
	}
	if opts.Store == nil || opts.Authz == nil || opts.Signer == nil {
		return nil, errors.New("api: store, authorizer and signer are required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Config.SessionTTL <= 0 {
		opts.Config.SessionTTL = 8 * time.Hour
	}
	if opts.Config.DefaultCertTTL <= 0 {
		opts.Config.DefaultCertTTL = 10 * time.Minute
	}
	if opts.Config.MaxCertTTL <= 0 {
		opts.Config.MaxCertTTL = time.Hour
	}
	if opts.Config.MaxBodyBytes < 1024 {
		opts.Config.MaxBodyBytes = 1 << 20
	}
	if opts.Config.RateLimitPerMinute < 1 {
		opts.Config.RateLimitPerMinute = 600
	}
	if opts.Config.LoginRateLimit < 1 {
		opts.Config.LoginRateLimit = 10
	}
	if opts.Config.LoginRateWindow <= 0 {
		opts.Config.LoginRateWindow = time.Minute
	}
	if opts.Config.LockoutThreshold < 1 {
		opts.Config.LockoutThreshold = 5
	}
	if opts.Config.LockoutDuration <= 0 {
		opts.Config.LockoutDuration = 15 * time.Minute
	}
	if opts.Config.PublicURL == "" {
		opts.Config.PublicURL = "https://localhost"
	}
	s := &Server{
		cfg:        opts.Config,
		store:      opts.Store,
		authz:      opts.Authz,
		signer:     opts.Signer,
		recs:       opts.Recordings,
		audit:      opts.Audit,
		metrics:    opts.Metrics,
		log:        opts.Logger,
		gateway:    opts.Gateway,
		web:        opts.Web,
		version:    opts.Version,
		limiter:    newRateLimiter(opts.Config.RateLimitPerMinute, time.Minute),
		loginLimit: newRateLimiter(opts.Config.LoginRateLimit, opts.Config.LoginRateWindow),
		secure:     opts.Config.SecureCookies || strings.HasPrefix(strings.ToLower(opts.Config.PublicURL), "https://"),
	}
	for _, cidr := range opts.Config.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("api: invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		s.trusted = append(s.trusted, network)
	}
	s.http = &http.Server{
		Addr:              opts.Config.ListenAddress,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return s, nil
}

// Addr returns the bound address after Listen has been called.
func (s *Server) Addr() string {
	if s.http == nil {
		return ""
	}
	return s.http.Addr
}

// Handler exposes the configured HTTP handler (used by tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", s.cfg.ListenAddress, err)
	}
	s.http.Addr = ln.Addr().String()
	s.log.Info("HTTPS control plane listening", "address", ln.Addr().String(), "tls", s.cfg.TLSCertFile != "")
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()
	if s.cfg.TLSCertFile != "" {
		err = s.http.ServeTLS(ln, s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	} else {
		err = s.http.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/version", s.versionInfo)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/auth/session", s.currentSession)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)

	mux.HandleFunc("GET /api/v1/auth/oidc/login", s.oidcLogin)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", s.oidcCallback)

	mux.HandleFunc("GET /api/v1/me", s.protected("", s.me))
	mux.HandleFunc("GET /api/v1/dashboard", s.protected("", s.dashboard))

	mux.HandleFunc("GET /api/v1/users", s.protected(authz.ActionUserRead, s.listUsers))
	mux.HandleFunc("POST /api/v1/users", s.protected(authz.ActionUserManage, s.createUser))
	mux.HandleFunc("GET /api/v1/users/{id}", s.protected(authz.ActionUserRead, s.getUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}", s.protected(authz.ActionUserManage, s.updateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.protected(authz.ActionUserManage, s.deleteUser))
	mux.HandleFunc("POST /api/v1/users/{id}/status", s.protected(authz.ActionUserManage, s.setUserStatus))
	mux.HandleFunc("POST /api/v1/users/{id}/password", s.protected(authz.ActionUserManage, s.setUserPassword))
	mux.HandleFunc("GET /api/v1/users/{id}/groups", s.protected(authz.ActionUserRead, s.getUserGroups))
	mux.HandleFunc("PUT /api/v1/users/{id}/groups", s.protected(authz.ActionUserManage, s.setUserGroups))
	mux.HandleFunc("GET /api/v1/users/{id}/bindings", s.protected(authz.ActionUserRead, s.getUserBindings))
	mux.HandleFunc("GET /api/v1/users/{id}/keys", s.protected(authz.ActionUserRead, s.listUserKeys))
	mux.HandleFunc("POST /api/v1/users/{id}/keys", s.protected(authz.ActionUserManage, s.addUserKey))
	mux.HandleFunc("DELETE /api/v1/users/{id}/keys/{keyID}", s.protected(authz.ActionUserManage, s.deleteUserKey))

	mux.HandleFunc("GET /api/v1/groups", s.protected(authz.ActionGroupRead, s.listGroups))
	mux.HandleFunc("POST /api/v1/groups", s.protected(authz.ActionGroupManage, s.createGroup))
	mux.HandleFunc("PATCH /api/v1/groups/{id}", s.protected(authz.ActionGroupManage, s.updateGroup))
	mux.HandleFunc("DELETE /api/v1/groups/{id}", s.protected(authz.ActionGroupManage, s.deleteGroup))
	mux.HandleFunc("GET /api/v1/groups/{id}/members", s.protected(authz.ActionGroupRead, s.listGroupMembers))
	mux.HandleFunc("POST /api/v1/groups/{id}/members", s.protected(authz.ActionGroupManage, s.addGroupMember))
	mux.HandleFunc("DELETE /api/v1/groups/{id}/members/{userID}", s.protected(authz.ActionGroupManage, s.removeGroupMember))

	mux.HandleFunc("GET /api/v1/roles", s.protected(authz.ActionRoleRead, s.listRoles))
	mux.HandleFunc("GET /api/v1/permissions", s.protected(authz.ActionRoleRead, s.listPermissions))
	mux.HandleFunc("GET /api/v1/role-bindings", s.protected(authz.ActionRoleRead, s.listRoleBindings))
	mux.HandleFunc("POST /api/v1/role-bindings", s.protected(authz.ActionRoleManage, s.createRoleBinding))
	mux.HandleFunc("DELETE /api/v1/role-bindings/{id}", s.protected(authz.ActionRoleManage, s.deleteRoleBinding))

	mux.HandleFunc("GET /api/v1/environments", s.protected("", s.listEnvironments))
	mux.HandleFunc("POST /api/v1/environments", s.protected(authz.ActionEnvironmentManage, s.createEnvironment))
	mux.HandleFunc("GET /api/v1/server-groups", s.protected(authz.ActionServerRead, s.listServerGroups))
	mux.HandleFunc("POST /api/v1/server-groups", s.protected(authz.ActionServerManage, s.createServerGroup))
	mux.HandleFunc("DELETE /api/v1/server-groups/{id}", s.protected(authz.ActionServerManage, s.deleteServerGroup))

	mux.HandleFunc("GET /api/v1/servers", s.protected(authz.ActionServerRead, s.listServers))
	mux.HandleFunc("POST /api/v1/servers", s.protected(authz.ActionServerManage, s.createServer))
	mux.HandleFunc("GET /api/v1/servers/{id}", s.protected(authz.ActionServerRead, s.getServer))
	mux.HandleFunc("PATCH /api/v1/servers/{id}", s.protected(authz.ActionServerManage, s.updateServer))
	mux.HandleFunc("DELETE /api/v1/servers/{id}", s.protected(authz.ActionServerManage, s.deleteServer))
	mux.HandleFunc("PUT /api/v1/servers/{id}/groups", s.protected(authz.ActionServerManage, s.setServerGroups))

	mux.HandleFunc("GET /api/v1/policies", s.protected(authz.ActionPolicyRead, s.listPolicies))
	mux.HandleFunc("POST /api/v1/policies", s.protected(authz.ActionPolicyManage, s.createPolicy))
	mux.HandleFunc("GET /api/v1/policies/{id}", s.protected(authz.ActionPolicyRead, s.getPolicy))
	mux.HandleFunc("PATCH /api/v1/policies/{id}", s.protected(authz.ActionPolicyManage, s.updatePolicy))
	mux.HandleFunc("DELETE /api/v1/policies/{id}", s.protected(authz.ActionPolicyManage, s.deletePolicy))

	mux.HandleFunc("GET /api/v1/access-requests", s.protected(authz.ActionAccessRead, s.listAccessRequests))
	mux.HandleFunc("POST /api/v1/access-requests", s.protected(authz.ActionAccessRequest, s.createAccessRequest))
	mux.HandleFunc("GET /api/v1/access-requests/{id}", s.protected(authz.ActionAccessRead, s.getAccessRequest))
	mux.HandleFunc("POST /api/v1/access-requests/{id}/decision", s.protected(authz.ActionAccessApprove, s.decideAccessRequest))
	mux.HandleFunc("POST /api/v1/access-requests/{id}/cancel", s.protected("", s.cancelAccessRequest))
	mux.HandleFunc("GET /api/v1/access-requests/{id}/approvals", s.protected(authz.ActionAccessRead, s.listAccessApprovals))

	mux.HandleFunc("GET /api/v1/certificates", s.protected(authz.ActionCertificateRead, s.listCertificates))
	mux.HandleFunc("POST /api/v1/certificates", s.protected(authz.ActionCertificateIssue, s.issueCertificate))
	mux.HandleFunc("POST /api/v1/certificates/{id}/revoke", s.protected(authz.ActionCertificateRevoke, s.revokeCertificate))
	mux.HandleFunc("POST /api/v1/certificates/revoke-user/{userID}", s.protected(authz.ActionCertificateRevoke, s.revokeUserCertificates))

	mux.HandleFunc("GET /api/v1/sessions", s.protected(authz.ActionSessionRead, s.listSessions))
	mux.HandleFunc("GET /api/v1/sessions/{id}", s.protected(authz.ActionSessionRead, s.getSession))
	mux.HandleFunc("POST /api/v1/sessions/{id}/terminate", s.protected(authz.ActionSessionTerminate, s.terminateSession))
	mux.HandleFunc("GET /api/v1/sessions/{id}/recording", s.protected(authz.ActionRecordingRead, s.downloadRecording))

	// WebSocket endpoint for real-time SSH terminal output streaming
	mux.HandleFunc("GET /ws/session/{id}", s.terminalWS)

	mux.HandleFunc("GET /api/v1/audit", s.protected(authz.ActionAuditRead, s.listAudit))
	mux.HandleFunc("GET /api/v1/audit/verify", s.protected(authz.ActionAuditRead, s.verifyAudit))

	mux.HandleFunc("GET /api/v1/tokens", s.protected("", s.listTokens))
	mux.HandleFunc("POST /api/v1/tokens", s.protected("", s.createToken))
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", s.protected("", s.revokeToken))

	mux.HandleFunc("GET /metrics", s.metricsHandler)

	if s.web != nil {
		mux.Handle("/", s.web)
	}
	return s.common(mux)
}

func (s *Server) common(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > 128 {
			requestID = identity.NewID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if s.secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if origin := r.Header.Get("Origin"); origin != "" && s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		clientIP := s.clientIP(r)
		if !s.limiter.Allow(clientIP) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxRequestID, requestID))
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		if s.metrics != nil {
			s.metrics.ObserveAPI(r.Method, route, recorder.status, started)
		}
		if s.log != nil && !strings.HasPrefix(r.URL.Path, "/assets/") {
			s.log.Info("api request", "method", r.Method, "path", r.URL.Path,
				"status", recorder.status, "duration_ms", time.Since(started).Milliseconds(),
				"remote", clientIP, "request_id", requestID)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote := net.ParseIP(host)
	if remote != nil && s.isTrustedProxy(remote) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if candidate := net.ParseIP(strings.TrimSpace(parts[0])); candidate != nil {
				return candidate.String()
			}
		}
	}
	return host
}

func (s *Server) isTrustedProxy(ip net.IP) bool {
	for _, network := range s.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	public, err := url.Parse(s.cfg.PublicURL)
	if err == nil && strings.EqualFold(u.Scheme, public.Scheme) && strings.EqualFold(u.Host, public.Host) {
		return true
	}
	for _, allowed := range s.cfg.CORSOrigins {
		if strings.EqualFold(strings.TrimSuffix(allowed, "/"), strings.TrimSuffix(origin, "/")) {
			return true
		}
	}
	return false
}

// protected authenticates a request and enforces a capability.
func (s *Server) protected(action string, next func(http.ResponseWriter, *http.Request, *model.User, model.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, principal, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		if action != "" && !principal.HasPermission(action) {
			s.auditEvent(r, audit.CategoryAuthz, "api.denied", audit.ResultDenied, user, "missing permission "+action, nil)
			writeError(w, http.StatusForbidden, "missing permission "+action)
			return
		}
		next(w, r, user, principal)
	}
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*model.User, model.Principal, bool) {
	ctx := r.Context()
	var (
		token *model.APIToken
		raw   string
		err   error
	)
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		raw = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return nil, model.Principal{}, false
		}
		token, err = s.store.GetTokenByHash(ctx, identity.HashToken(raw))
		if err != nil || token.Kind != model.TokenAPI {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return nil, model.Principal{}, false
		}
	} else {
		cookie, cookieErr := r.Cookie(sessionCookie)
		if cookieErr != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return nil, model.Principal{}, false
		}
		raw = cookie.Value
		token, err = s.store.GetTokenByHash(ctx, identity.HashToken(raw))
		if err != nil || token.Kind != model.TokenSession {
			writeError(w, http.StatusUnauthorized, "session expired")
			return nil, model.Principal{}, false
		}
		if !safeMethod(r.Method) {
			if !s.csrfValid(r) {
				writeError(w, http.StatusForbidden, "CSRF token missing or invalid")
				return nil, model.Principal{}, false
			}
			if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
				writeError(w, http.StatusForbidden, "origin is not allowed")
				return nil, model.Principal{}, false
			}
		}
	}
	user, err := s.store.GetUserByID(ctx, token.UserID)
	if err != nil || !user.Active() {
		writeError(w, http.StatusUnauthorized, "account is not active")
		return nil, model.Principal{}, false
	}
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		writeError(w, http.StatusUnauthorized, "account is temporarily locked")
		return nil, model.Principal{}, false
	}
	principal, err := s.authz.LoadPrincipal(ctx, user, user.MFAEnrolled)
	if err != nil {
		s.log.Error("load principal failed", "user", user.Username, "error", err)
		writeError(w, http.StatusInternalServerError, "authorization lookup failed")
		return nil, model.Principal{}, false
	}
	// Record use without blocking the request.
	go func(tokenID string) {
		tctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.store.TouchToken(tctx, tokenID)
	}(token.ID)
	return user, principal, true
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func (s *Server) csrfValid(r *http.Request) bool {
	header := r.Header.Get("X-CSRF-Token")
	if header == "" {
		return false
	}
	cookie, err := r.Cookie(csrfCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	return subtleCompare(header, cookie.Value)
}

func (s *Server) auditEvent(r *http.Request, category, action, result string, user *model.User, message string, details map[string]any) {
	if s.audit == nil {
		return
	}
	event := audit.Event{
		Category: category, Action: action, Result: result, Message: message,
		SourceIP: s.clientIP(r), Details: details,
		CorrelationID: requestID(r.Context()),
	}
	if user != nil {
		event.ActorID = user.ID
		event.ActorName = user.Username
	}
	if err := s.audit.Write(r.Context(), event); err != nil {
		s.log.Error("audit write failed", "action", action, "error", err)
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unhealthy", "database": "unavailable"})
		return
	}
	body := map[string]any{"status": "ok", "database": "ok", "version": s.version}
	if s.signer != nil {
		if err := s.signer.Health(ctx); err != nil {
			body["status"] = "degraded"
			body["certificate_authority"] = "unavailable"
		} else {
			body["certificate_authority"] = "ok"
		}
	}
	status := http.StatusOK
	if body["status"] != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, body)
}

func (s *Server) versionInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"name": "RSCON", "version": s.version})
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		http.NotFound(w, r)
		return
	}
	s.metrics.Handler().ServeHTTP(w, r)
}

func requestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

func subtleCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// rateLimiter is a fixed-window per-key limiter. It is deliberately in-memory:
// limits protect a single instance and are not a distributed quota.
type rateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	counters map[string]*counter
}

type counter struct {
	value int
	reset time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, counters: map[string]*counter{}}
}

func (l *rateLimiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.counters[key]
	if !ok || now.After(c.reset) {
		l.counters[key] = &counter{value: 1, reset: now.Add(l.window)}
		if len(l.counters) > 10000 {
			for k, v := range l.counters {
				if now.After(v.reset) {
					delete(l.counters, k)
				}
			}
		}
		return true
	}
	c.value++
	return c.value <= l.limit
}

func parseInt(v string, fallback int) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
