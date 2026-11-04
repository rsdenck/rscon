// Package gateway implements the RSCON SSH data plane: certificate
// authentication, authorization, proxying, recording and session lifecycle.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/ca"
	"github.com/rsdenck/rscon/internal/hostkey"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/observability"
	"github.com/rsdenck/rscon/internal/recording"
	"github.com/rsdenck/rscon/pkg/version"
	"golang.org/x/crypto/ssh"
)

// Certificate extension and critical option keys.
const (
	ExtRole     = "rscon-role"
	ExtTarget   = "rscon-target"
	ExtMFA      = "rscon-mfa"
	ExtCertID   = "rscon-cert-id"
	RoleUser    = "user"
	RoleGateway = "gateway"
)

// Permission extension keys propagated from authentication.
const (
	PermUserID        = "user_id"
	PermUsername      = "username"
	PermServerID      = "server_id"
	PermServerName    = "server_name"
	PermAccount       = "account"
	PermCertID        = "cert_id"
	PermAuthMethod    = "auth_method"
	PermMFA           = "mfa"
	PermSourceIP      = "source_ip"
	PermEnvironmentID = "environment_id"
	PermCorrelationID = "correlation_id"
)

var (
	accountPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	serverPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

// Store is the subset of persistence used by the data plane.
type Store interface {
	GetUserByID(ctx context.Context, id string) (*model.User, error)
	GetUserByUsername(ctx context.Context, username string) (*model.User, error)
	GetUserByKeyFingerprint(ctx context.Context, fingerprint string) (*model.User, error)
	TouchPublicKey(ctx context.Context, fingerprint string) error
	GetServerByName(ctx context.Context, name string) (*model.Server, error)
	GetServerByID(ctx context.Context, id string) (*model.Server, error)
	GetServerByAddress(ctx context.Context, address string) (*model.Server, error)
	ListServerGroupIDs(ctx context.Context, serverID string) ([]string, error)
	GetCertificateBySerial(ctx context.Context, serial uint64) (*model.Certificate, error)
	CreateSession(ctx context.Context, session *model.Session) error
	CloseSession(ctx context.Context, id, status, reason string, exitStatus *int, recordingID string) error
	SetSessionRecording(ctx context.Context, sessionID, recordingID string) error
	CountActiveSessionsForUser(ctx context.Context, userID string) (int, error)
	CreateRecording(ctx context.Context, rec *model.Recording) error
	ClaimTerminationRequests(ctx context.Context, gateway string) ([]model.Session, error)
	MarkStaleSessions(ctx context.Context, gateway string, olderThan time.Duration) (int64, error)
}

// Authorizer is the policy engine surface used by the gateway.
type Authorizer interface {
	LoadPrincipal(ctx context.Context, user *model.User, mfa bool) (model.Principal, error)
	Authorize(ctx context.Context, p model.Principal, action string, res authz.Resource, opts authz.Options) (authz.Decision, error)
}

// Config configures the gateway.
type Config struct {
	Name                  string
	ListenAddress         string
	HostSigners           []ssh.Signer
	GatewaySigner         ssh.Signer
	TrustedUserCAs        []ssh.PublicKey
	CertificateTTL        time.Duration
	DestinationCertTTL    time.Duration
	MaxSessionDuration    time.Duration
	IdleTimeout           time.Duration
	DialTimeout           time.Duration
	HandshakeTimeout      time.Duration
	MaxConcurrentSessions int
	MaxAuthTries          int
	AllowLocalKeyAuth     bool
	AllowTCPForwarding    bool
	AllowSFTP             bool
	RecordingFailClosed   bool
}

// Options are the gateway dependencies.
type Options struct {
	Config     Config
	Store      Store
	Authorizer Authorizer
	Signer     ca.Signer
	Recordings recording.Store
	Audit      *audit.Service
	Metrics    *observability.Metrics
	Logger     *slog.Logger
	HostKeys   *hostkey.Verifier
}

// Server is the SSH bastion listener.
type Server struct {
	cfg     Config
	store   Store
	authz   Authorizer
	signer  ca.Signer
	recs    recording.Store
	audit   *audit.Service
	metrics *observability.Metrics
	log     *slog.Logger
	hosts   *hostkey.Verifier

	listener net.Listener
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
	sessions map[string]*activeSession
	wg       sync.WaitGroup

	caMu      sync.RWMutex
	dynamicCA ssh.PublicKey

	sshConfig *ssh.ServerConfig
}

// New validates dependencies and builds a gateway.
func New(opts Options) (*Server, error) {
	if opts.Config.ListenAddress == "" {
		return nil, errors.New("gateway: listen address is required")
	}
	if len(opts.Config.HostSigners) == 0 {
		return nil, errors.New("gateway: at least one host signer is required")
	}
	if opts.Config.GatewaySigner == nil {
		return nil, errors.New("gateway: data-plane signer is required")
	}
	if opts.Store == nil || opts.Authorizer == nil || opts.Signer == nil {
		return nil, errors.New("gateway: store, authorizer and signer are required")
	}
	if opts.HostKeys == nil {
		return nil, errors.New("gateway: host key verifier is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Server{
		cfg:      opts.Config,
		store:    opts.Store,
		authz:    opts.Authorizer,
		signer:   opts.Signer,
		recs:     opts.Recordings,
		audit:    opts.Audit,
		metrics:  opts.Metrics,
		log:      opts.Logger,
		hosts:    opts.HostKeys,
		conns:    map[net.Conn]struct{}{},
		sessions: map[string]*activeSession{},
	}
	for _, caKey := range opts.Config.TrustedUserCAs {
		s.addDynamicCA(caKey)
	}
	s.sshConfig = &ssh.ServerConfig{
		MaxAuthTries:  s.cfg.MaxAuthTries,
		ServerVersion: "SSH-2.0-RSCON_" + strings.ReplaceAll(version.Version, " ", "_"),
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return s.authenticate(meta, key)
		},
	}
	for _, signer := range opts.Config.HostSigners {
		s.sshConfig.AddHostKey(signer)
	}
	return s, nil
}

func (s *Server) addDynamicCA(key ssh.PublicKey) {
	if key == nil {
		return
	}
	s.caMu.Lock()
	defer s.caMu.Unlock()
	s.dynamicCA = key
}

func (s *Server) authorities() []ssh.PublicKey {
	s.caMu.RLock()
	defer s.caMu.RUnlock()
	if s.dynamicCA == nil {
		return nil
	}
	return []ssh.PublicKey{s.dynamicCA}
}

// Run listens until ctx is cancelled. It reconciles sessions owned by this
// gateway that were left active by a previous unclean shutdown.
func (s *Server) Run(ctx context.Context) error {
	if err := s.refreshCA(ctx); err != nil {
		s.log.Warn("certificate authority is not reachable at startup", "error", err)
	}
	if n, err := s.store.MarkStaleSessions(ctx, s.cfg.Name, time.Minute); err == nil && n > 0 {
		s.log.Warn("reconciled stale sessions from a previous gateway run", "count", n)
	}
	listener, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("gateway: listen %s: %w", s.cfg.ListenAddress, err)
	}
	s.listener = listener
	s.log.Info("SSH gateway listening", "address", listener.Addr().String(), "server", s.cfg.Name)

	caCtx, caCancel := context.WithCancel(ctx)
	defer caCancel()
	go s.caRefreshLoop(caCtx)
	go s.terminationLoop(caCtx)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.mu.Unlock()
	}()

	var delay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else {
				delay *= 2
			}
			if delay > time.Second {
				delay = time.Second
			}
			s.log.Warn("temporary SSH accept error", "error", err, "retry_in", delay.String())
			select {
			case <-time.After(delay):
			case <-ctx.Done():
			}
			continue
		}
		delay = 0
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
			s.mu.Lock()
			delete(s.conns, conn)
			s.mu.Unlock()
		}()
	}
	s.wg.Wait()
	s.log.Info("SSH gateway stopped")
	return nil
}

// Addr returns the bound address (after Run has started).
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) refreshCA(ctx context.Context) error {
	key, err := s.signer.CA(ctx)
	if err != nil {
		return err
	}
	s.addDynamicCA(key)
	return nil
}

func (s *Server) caRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refreshCA(ctx); err != nil {
				s.log.Warn("failed to refresh certificate authority public key", "error", err)
			}
		}
	}
}

func (s *Server) handleConn(ctx context.Context, netConn net.Conn) {
	_ = netConn.SetDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
	conn, chans, reqs, err := ssh.NewServerConn(netConn, s.sshConfig)
	if err != nil {
		if s.metrics != nil {
			s.metrics.SSHConnections.WithLabelValues("auth_failed").Inc()
		}
		s.log.Debug("SSH handshake failed", "remote", netConn.RemoteAddr().String(), "error", err)
		return
	}
	_ = netConn.SetDeadline(time.Time{})
	defer conn.Close()
	if s.metrics != nil {
		s.metrics.SSHConnections.WithLabelValues("accepted").Inc()
	}
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			go s.handleSession(ctx, conn, newChannel)
		case "direct-tcpip":
			go s.handleDirectTCPIP(ctx, conn, newChannel)
		default:
			_ = newChannel.Reject(ssh.UnknownChannelType, "channel type is not permitted by RSCON")
		}
	}
}

// authenticate is the SSH public key callback. It performs certificate or
// registered-key identity resolution, revocation checks, target binding and a
// fresh authorization decision.
func (s *Server) authenticate(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sourceIP := remoteIP(meta.RemoteAddr())
	username := meta.User()

	account, serverName, err := parseTarget(username)
	if err != nil {
		s.authFailure(ctx, username, sourceIP, err.Error())
		return nil, err
	}
	server, err := s.store.GetServerByName(ctx, serverName)
	if err != nil {
		s.authFailure(ctx, username, sourceIP, "destination is not registered")
		return nil, errors.New("destination is not registered")
	}
	if !server.Enabled {
		s.authFailure(ctx, username, sourceIP, "destination is disabled")
		return nil, errors.New("destination is disabled")
	}

	var (
		user       *model.User
		certID     string
		mfa        bool
		authMethod string
	)
	if cert, ok := key.(*ssh.Certificate); ok {
		authMethod = "certificate"
		if err := s.verifyCertificate(ctx, cert, server, account); err != nil {
			s.authFailure(ctx, username, sourceIP, err.Error())
			return nil, err
		}
		user, err = s.store.GetUserByUsername(ctx, cert.KeyId)
		if err != nil {
			s.authFailure(ctx, username, sourceIP, "certificate identity is not active")
			return nil, errors.New("certificate identity is not active")
		}
		record, err := s.store.GetCertificateBySerial(ctx, cert.Serial)
		if err != nil {
			s.authFailure(ctx, username, sourceIP, "certificate is not registered")
			return nil, errors.New("certificate is not registered or has been revoked")
		}
		if record.RevokedAt != nil {
			s.authFailure(ctx, cert.KeyId, sourceIP, "certificate is revoked")
			return nil, errors.New("certificate is revoked")
		}
		if record.UserID != user.ID || record.ServerID != server.ID || record.Account != account {
			s.authFailure(ctx, cert.KeyId, sourceIP, "certificate target mismatch")
			return nil, errors.New("certificate does not match this destination")
		}
		certID = record.ID
		mfa = cert.Extensions[ExtMFA] == "true"
	} else {
		if !s.cfg.AllowLocalKeyAuth {
			s.authFailure(ctx, username, sourceIP, "public key authentication is disabled")
			return nil, errors.New("only SSH certificates are accepted")
		}
		authMethod = "publickey"
		user, err = s.store.GetUserByKeyFingerprint(ctx, ssh.FingerprintSHA256(key))
		if err != nil {
			s.authFailure(ctx, username, sourceIP, "public key is not registered")
			return nil, errors.New("public key is not registered")
		}
		go func() { _ = s.store.TouchPublicKey(context.Background(), ssh.FingerprintSHA256(key)) }()
	}

	if !user.Active() {
		s.authFailure(ctx, user.Username, sourceIP, "user is not active")
		return nil, errors.New("user is not active")
	}
	groups, err := s.store.ListServerGroupIDs(ctx, server.ID)
	if err != nil {
		return nil, errors.New("authorization lookup failed")
	}
	principal, err := s.authz.LoadPrincipal(ctx, user, mfa)
	if err != nil {
		s.log.Error("load principal failed", "user", user.Username, "error", err)
		return nil, errors.New("authorization lookup failed")
	}
	decision, err := s.authz.Authorize(ctx, principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: server.ID, Name: server.Name, EnvironmentID: server.EnvironmentID,
		GroupIDs: groups, Account: account,
	}, authz.Options{})
	if err != nil {
		s.log.Error("authorization failed", "user", user.Username, "error", err)
		return nil, errors.New("authorization failed")
	}
	if !decision.Allowed {
		if s.metrics != nil {
			s.metrics.AuthzDenied.WithLabelValues(authz.ActionSSHConnect).Inc()
		}
		s.authFailure(ctx, user.Username, sourceIP, decision.Reason)
		return nil, fmt.Errorf("access denied: %s", decision.Reason)
	}
	active, err := s.store.CountActiveSessionsForUser(ctx, user.ID)
	if err == nil && s.cfg.MaxConcurrentSessions > 0 && active >= s.cfg.MaxConcurrentSessions {
		s.authFailure(ctx, user.Username, sourceIP, "concurrent session limit reached")
		return nil, errors.New("concurrent session limit reached")
	}

	correlation := identity.NewID()
	perms := &ssh.Permissions{Extensions: map[string]string{
		PermUserID:        user.ID,
		PermUsername:      user.Username,
		PermServerID:      server.ID,
		PermServerName:    server.Name,
		PermAccount:       account,
		PermCertID:        certID,
		PermAuthMethod:    authMethod,
		PermMFA:           fmt.Sprintf("%t", mfa),
		PermSourceIP:      sourceIP,
		PermEnvironmentID: server.EnvironmentID,
		PermCorrelationID: correlation,
	}}
	if s.audit != nil {
		if err := s.audit.Write(ctx, audit.Event{
			Category: audit.CategoryAuth, Action: "ssh.authenticated", ActorID: user.ID,
			ActorName: user.Username, Result: audit.ResultSuccess, ResourceType: "server",
			ResourceID: server.ID, SourceIP: sourceIP, CorrelationID: correlation,
			Message: "SSH certificate accepted",
			Details: map[string]any{"account": account, "auth_method": authMethod, "mfa": mfa, "certificate_id": certID},
		}); err != nil {
			return nil, errors.New("mandatory audit write failed")
		}
	}
	return perms, nil
}

func (s *Server) verifyCertificate(ctx context.Context, cert *ssh.Certificate, server *model.Server, account string) error {
	if cert.CertType != ssh.UserCert {
		return errors.New("certificate is not a user certificate")
	}
	if cert.KeyId == "" {
		return errors.New("certificate has no key id")
	}
	if cert.Extensions[ExtRole] != RoleUser {
		return errors.New("certificate role is not user")
	}
	expectedTarget := server.ID + ":" + account
	if cert.Extensions[ExtTarget] != expectedTarget {
		return errors.New("certificate is bound to a different destination or account")
	}
	authorities := s.authorities()
	if len(authorities) == 0 {
		return errors.New("no certificate authority is trusted")
	}
	checker := &ssh.CertChecker{
		IsUserAuthority: func(key ssh.PublicKey) bool {
			for _, authority := range authorities {
				if key.Marshal() != nil && string(key.Marshal()) == string(authority.Marshal()) {
					return true
				}
			}
			return false
		},
	}
	if err := checker.CheckCert(cert.KeyId, cert); err != nil {
		return fmt.Errorf("certificate validation failed: %w", err)
	}
	return nil
}

func (s *Server) authFailure(ctx context.Context, username, sourceIP, reason string) {
	if s.metrics != nil {
		s.metrics.SSHAuthFailures.WithLabelValues(sanitiseReason(reason)).Inc()
	}
	if s.audit != nil {
		_ = s.audit.Write(ctx, audit.Event{
			Category: audit.CategoryAuth, Action: "ssh.authentication", ActorName: username,
			Result: audit.ResultDenied, SourceIP: sourceIP, Message: reason,
		})
	}
}

func parseTarget(username string) (account, server string, err error) {
	parts := strings.SplitN(username, "@", 2)
	if len(parts) != 2 {
		return "", "", errors.New("SSH username must use the form account@destination")
	}
	account, server = parts[0], parts[1]
	if !accountPattern.MatchString(account) {
		return "", "", errors.New("invalid Linux account in SSH username")
	}
	if !serverPattern.MatchString(server) {
		return "", "", errors.New("invalid destination in SSH username")
	}
	return account, server, nil
}

func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

func sanitiseReason(reason string) string {
	if len(reason) > 120 {
		return reason[:120]
	}
	return reason
}
