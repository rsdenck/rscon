package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/ca"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/recording"
	"golang.org/x/crypto/ssh"
)

type ptyRequest struct {
	Term          string
	Columns, Rows uint32
	Width, Height uint32
	Modes         string
}

type windowChange struct {
	Columns, Rows uint32
	Width, Height uint32
}

type execRequest struct {
	Command string
}

type subsystemRequest struct {
	Subsystem string
}

type signalRequest struct {
	Signal string
}

type directTCPIP struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type activeSession struct {
	terminate func(reason string)
}

func (s *Server) registerSession(id string, terminate func(string)) {
	s.mu.Lock()
	s.sessions[id] = &activeSession{terminate: terminate}
	s.mu.Unlock()
}

func (s *Server) unregisterSession(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// TerminateSession cancels a locally hosted session. It returns false when the
// session is not owned by this gateway; the database termination flag remains
// set and will be handled by the owning gateway.
func (s *Server) TerminateSession(id string) bool {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if ok {
		sess.terminate("terminated by administrator")
	}
	return ok
}

func (s *Server) terminationLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reqs, err := s.store.ClaimTerminationRequests(ctx, s.cfg.Name)
			if err != nil {
				s.log.Warn("failed to poll session termination requests", "error", err)
				continue
			}
			for _, sess := range reqs {
				if s.TerminateSession(sess.ID) {
					s.log.Info("session terminated by administrative request", "session_id", sess.ID)
				}
			}
		}
	}
}

func (s *Server) handleSession(ctx context.Context, conn *ssh.ServerConn, newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()

	var pty *ptyRequest
	started := false
	replyNo := func(req *ssh.Request) {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
	for req := range requests {
		switch req.Type {
		case "pty-req":
			var payload ptyRequest
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				replyNo(req)
				continue
			}
			if payload.Columns == 0 {
				payload.Columns = 80
			}
			if payload.Rows == 0 {
				payload.Rows = 24
			}
			pty = &payload
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		case "env":
			// Forwarding arbitrary environment variables to destinations can
			// be abused (for example LD_PRELOAD). RSCON deliberately drops them.
			replyNo(req)
		case "window-change":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		case "shell":
			if started {
				replyNo(req)
				continue
			}
			started = true
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			s.runSession(ctx, conn, channel, requests, pty, "", "")
			return
		case "exec":
			if started {
				replyNo(req)
				continue
			}
			var payload execRequest
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil || payload.Command == "" {
				replyNo(req)
				continue
			}
			started = true
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			s.runSession(ctx, conn, channel, requests, pty, payload.Command, "")
			return
		case "subsystem":
			if started {
				replyNo(req)
				continue
			}
			var payload subsystemRequest
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				replyNo(req)
				continue
			}
			if payload.Subsystem != "sftp" || !s.cfg.AllowSFTP {
				replyNo(req)
				continue
			}
			started = true
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			s.runSession(ctx, conn, channel, requests, pty, "", payload.Subsystem)
			return
		case "auth-agent-req@openssh.com", "x11-req":
			replyNo(req)
		default:
			replyNo(req)
		}
	}
}

func (s *Server) runSession(ctx context.Context, conn *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request, pty *ptyRequest, command, subsystem string) {
	perms := conn.Permissions
	if perms == nil {
		return
	}
	sourceIP := perms.Extensions[PermSourceIP]
	user, err := s.store.GetUserByID(ctx, perms.Extensions[PermUserID])
	if err != nil || !user.Active() {
		s.denySession(ctx, perms, sourceIP, "user is no longer active")
		return
	}
	server, err := s.store.GetServerByID(ctx, perms.Extensions[PermServerID])
	if err != nil || !server.Enabled {
		s.denySession(ctx, perms, sourceIP, "destination is no longer available")
		return
	}
	account := perms.Extensions[PermAccount]
	groups, err := s.store.ListServerGroupIDs(ctx, server.ID)
	if err != nil {
		s.denySession(ctx, perms, sourceIP, "authorization lookup failed")
		return
	}
	principal, err := s.authz.LoadPrincipal(ctx, user, perms.Extensions[PermMFA] == "true")
	if err != nil {
		s.denySession(ctx, perms, sourceIP, "authorization lookup failed")
		return
	}
	channelName := "session"
	if subsystem == "sftp" {
		channelName = "sftp"
	}
	decision, err := s.authz.Authorize(ctx, principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: server.ID, Name: server.Name, EnvironmentID: server.EnvironmentID,
		GroupIDs: groups, Account: account, Channel: channelName,
	}, authz.Options{Channel: channelName})
	if err != nil || !decision.Allowed {
		reason := "authorization failed"
		if decision.Reason != "" {
			reason = decision.Reason
		}
		s.denySession(ctx, perms, sourceIP, reason)
		return
	}

	sessionID := identity.NewID()
	sess := &model.Session{
		ID: sessionID, UserID: user.ID, ServerID: server.ID, Account: account,
		SourceIP: sourceIP, AuthMethod: perms.Extensions[PermAuthMethod],
		CertificateID: perms.Extensions[PermCertID], Gateway: s.cfg.Name,
		Command: command,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		s.log.Error("failed to register session", "error", err)
		s.denySession(ctx, perms, sourceIP, "session registration failed")
		return
	}

	var rec recording.Writer
	if s.recs != nil {
		width, height := 80, 24
		term := "xterm-256color"
		if pty != nil {
			width, height, term = int(pty.Columns), int(pty.Rows), pty.Term
		}
		rec, err = s.recs.Create(ctx, recording.Meta{
			SessionID: sessionID, User: user.Username, Server: server.Name,
			Account: account, Term: term, Width: width, Height: height,
		})
		if err != nil {
			if s.metrics != nil {
				s.metrics.RecordingErrors.Inc()
			}
			_ = s.store.CloseSession(ctx, sessionID, model.SessionFailed, "recording could not start", nil, "")
			if s.cfg.RecordingFailClosed {
				s.denySession(ctx, perms, sourceIP, "session recording is unavailable")
				return
			}
			s.log.Warn("recording disabled for session", "session_id", sessionID, "error", err)
			rec = nil
		}
	}

	if s.audit != nil {
		if err := s.audit.Write(ctx, audit.Event{
			Category: audit.CategorySession, Action: "session.started", ActorID: user.ID,
			ActorName: user.Username, Result: audit.ResultSuccess, ResourceType: "server",
			ResourceID: server.ID, SourceIP: sourceIP,
			CorrelationID: perms.Extensions[PermCorrelationID],
			Details:       map[string]any{"session_id": sessionID, "account": account, "command": command, "subsystem": subsystem},
		}); err != nil {
			if rec != nil {
				rec.Abort()
			}
			_ = s.store.CloseSession(ctx, sessionID, model.SessionFailed, "mandatory audit write failed", nil, "")
			s.denySession(ctx, perms, sourceIP, "audit is unavailable")
			return
		}
	}

	client, err := s.dialDestination(ctx, server, account)
	if err != nil {
		if rec != nil {
			rec.Abort()
		}
		_ = s.store.CloseSession(ctx, sessionID, model.SessionFailed, "destination connection failed", nil, "")
		s.log.Warn("destination connection failed", "server", server.Name, "error", err)
		s.denySession(ctx, perms, sourceIP, "destination connection failed")
		return
	}
	defer client.Close()

	dst, err := client.NewSession()
	if err != nil {
		s.abortSession(ctx, sessionID, rec, perms, sourceIP, "destination session failed")
		return
	}
	defer dst.Close()

	// Pipes must be created before the destination command starts.
	stdin, err := dst.StdinPipe()
	if err != nil {
		s.abortSession(ctx, sessionID, rec, perms, sourceIP, "destination input pipe failed")
		return
	}
	stdout, err := dst.StdoutPipe()
	if err != nil {
		s.abortSession(ctx, sessionID, rec, perms, sourceIP, "destination output pipe failed")
		return
	}
	stderr, err := dst.StderrPipe()
	if err != nil {
		s.abortSession(ctx, sessionID, rec, perms, sourceIP, "destination error pipe failed")
		return
	}

	if pty != nil {
		modes := ssh.TerminalModes{}
		if err := dst.RequestPty(pty.Term, int(pty.Rows), int(pty.Columns), modes); err != nil {
			s.abortSession(ctx, sessionID, rec, perms, sourceIP, "destination rejected PTY allocation")
			return
		}
	}
	switch {
	case subsystem != "":
		err = dst.RequestSubsystem(subsystem)
	case command != "":
		err = dst.Start(command)
	default:
		err = dst.Shell()
	}
	if err != nil {
		s.abortSession(ctx, sessionID, rec, perms, sourceIP, "destination rejected session start")
		return
	}

	if s.metrics != nil {
		s.metrics.SSHActive.Inc()
		defer s.metrics.SSHActive.Dec()
	}

	state := &sessionRuntime{}
	state.terminateFn = func(reason string) {
		state.once.Do(func() {
			state.setReason(reason)
			client.Close()
			dst.Close()
			channel.Close()
		})
	}
	s.registerSession(sessionID, state.terminateFn)
	defer s.unregisterSession(sessionID)

	lastActivity := &atomic.Int64{}
	lastActivity.Store(time.Now().UnixNano())
	touch := func() { lastActivity.Store(time.Now().UnixNano()) }

	sessionDone := make(chan struct{})
	defer close(sessionDone)

	maxTimer := time.AfterFunc(s.cfg.MaxSessionDuration, func() {
		state.terminateFn("maximum session duration reached")
	})
	defer maxTimer.Stop()
	go func() {
		select {
		case <-ctx.Done():
			state.terminateFn("gateway shutting down")
		case <-sessionDone:
		}
	}()
	if s.cfg.IdleTimeout > 0 {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-sessionDone:
					return
				case <-ticker.C:
					idle := time.Duration(time.Now().UnixNano() - lastActivity.Load())
					if idle >= s.cfg.IdleTimeout {
						state.terminateFn("idle timeout")
						return
					}
				}
			}
		}()
	}

	// The client's request channel is handed to this function once the session
	// starts, so window changes and signals must be forwarded from here.
	go func() {
		for req := range requests {
			switch req.Type {
			case "window-change":
				var wc windowChange
				if err := ssh.Unmarshal(req.Payload, &wc); err == nil {
					_ = dst.WindowChange(int(wc.Rows), int(wc.Columns))
				}
			case "signal":
				var sig signalRequest
				if err := ssh.Unmarshal(req.Payload, &sig); err == nil {
					_ = dst.Signal(ssh.Signal(sig.Signal))
				}
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		}
	}()

	recorder := &recorderSink{w: rec}
	writer := &lockedChannelWriter{channel: channel, recorder: recorder}
	var copyWG sync.WaitGroup
	copyWG.Add(2)
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(writer, touchReader{reader: stdout, touch: touch})
	}()
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(writer, touchReader{reader: stderr, touch: touch})
	}()
	go func() {
		_, _ = io.Copy(&recordingWriter{dst: stdin, rec: recorder}, touchReader{reader: channel, touch: touch})
		_ = stdin.Close()
	}()

	waitErr := dst.Wait()
	close(sessionDone)
	_ = channel.CloseWrite()
	copyWG.Wait()

	exitStatus, failed := exitStatusFrom(waitErr)
	reason := state.reason()
	if reason == "" {
		if failed {
			reason = "destination session failed"
		} else {
			reason = "session completed"
		}
	}
	status := model.SessionClosed
	if failed {
		status = model.SessionFailed
	}

	// Stop the recording before returning; the sink guards against a late
	// input copy still holding a reference to the writer.
	recordingID := ""
	if rec != nil {
		obj, err := recorder.Close(context.Background())
		if err != nil {
			if s.metrics != nil {
				s.metrics.RecordingErrors.Inc()
			}
			s.log.Error("failed to finalise recording", "session_id", sessionID, "error", err)
		} else {
			recordingID = identity.NewID()
			record := &model.Recording{
				ID: recordingID, SessionID: sessionID, Backend: obj.Backend,
				ObjectKey: obj.Key, Format: obj.Format, SizeBytes: obj.Size, SHA256: obj.SHA256,
			}
			if err := s.store.CreateRecording(context.Background(), record); err != nil {
				s.log.Error("failed to persist recording metadata", "session_id", sessionID, "error", err)
				recordingID = ""
			} else if obj.Truncated {
				s.log.Warn("recording truncated at size limit", "session_id", sessionID)
			}
			if err := s.store.SetSessionRecording(context.Background(), sessionID, recordingID); err != nil {
				s.log.Warn("failed to attach recording to session", "error", err)
			}
		}
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = s.store.CloseSession(closeCtx, sessionID, status, reason, exitStatus, recordingID)
	closeCancel()

	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(derefStatus(exitStatus))}))
	if s.metrics != nil {
		s.metrics.SSHSessions.WithLabelValues(status).Inc()
	}
	if s.audit != nil {
		details := map[string]any{
			"session_id": sessionID, "account": account, "status": status,
			"duration_seconds": time.Since(sess.StartedAt).Seconds(), "exit_status": exitStatus,
		}
		if recordingID != "" {
			details["recording_id"] = recordingID
		}
		_ = s.audit.Write(context.Background(), audit.Event{
			Category: audit.CategorySession, Action: "session.closed", ActorID: user.ID,
			ActorName: user.Username, Result: audit.ResultSuccess, ResourceType: "server",
			ResourceID: server.ID, SourceIP: sourceIP,
			CorrelationID: perms.Extensions[PermCorrelationID],
			Message:       reason, Details: details,
		})
	}
}

func (s *Server) abortSession(ctx context.Context, sessionID string, rec recording.Writer, perms *ssh.Permissions, sourceIP, reason string) {
	if rec != nil {
		rec.Abort()
	}
	_ = s.store.CloseSession(ctx, sessionID, model.SessionFailed, reason, nil, "")
	s.denySession(ctx, perms, sourceIP, reason)
}

func (s *Server) denySession(ctx context.Context, perms *ssh.Permissions, sourceIP, reason string) {
	if s.audit != nil {
		_ = s.audit.Write(ctx, audit.Event{
			Category: audit.CategoryAuthz, Action: "ssh.session_denied",
			ActorID: perms.Extensions[PermUserID], ActorName: perms.Extensions[PermUsername],
			Result: audit.ResultDenied, ResourceType: "server",
			ResourceID: perms.Extensions[PermServerID], SourceIP: sourceIP, Message: reason,
		})
	}
	if s.metrics != nil {
		s.metrics.AuthzDenied.WithLabelValues(authz.ActionSSHConnect).Inc()
	}
}

func (s *Server) handleDirectTCPIP(ctx context.Context, conn *ssh.ServerConn, newChannel ssh.NewChannel) {
	if !s.cfg.AllowTCPForwarding {
		_ = newChannel.Reject(ssh.Prohibited, "TCP forwarding is disabled")
		return
	}
	perms := conn.Permissions
	if perms == nil {
		_ = newChannel.Reject(ssh.Prohibited, "unauthenticated")
		return
	}
	var payload directTCPIP
	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
		_ = newChannel.Reject(ssh.Prohibited, "invalid direct-tcpip request")
		return
	}
	server, err := s.store.GetServerByAddress(ctx, payload.DestAddr)
	if err != nil {
		_ = newChannel.Reject(ssh.Prohibited, "destination is not registered")
		return
	}
	if int(payload.DestPort) != server.Port {
		_ = newChannel.Reject(ssh.Prohibited, "destination port is not permitted")
		return
	}
	if !server.Enabled {
		_ = newChannel.Reject(ssh.Prohibited, "destination is disabled")
		return
	}
	user, err := s.store.GetUserByID(ctx, perms.Extensions[PermUserID])
	if err != nil || !user.Active() {
		_ = newChannel.Reject(ssh.Prohibited, "user is not active")
		return
	}
	groups, _ := s.store.ListServerGroupIDs(ctx, server.ID)
	principal, err := s.authz.LoadPrincipal(ctx, user, perms.Extensions[PermMFA] == "true")
	if err != nil {
		_ = newChannel.Reject(ssh.Prohibited, "authorization failed")
		return
	}
	decision, err := s.authz.Authorize(ctx, principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: server.ID, Name: server.Name, EnvironmentID: server.EnvironmentID,
		GroupIDs: groups, Account: perms.Extensions[PermAccount], Channel: "tcpip",
	}, authz.Options{Channel: "tcpip"})
	if err != nil || !decision.Allowed {
		_ = newChannel.Reject(ssh.Prohibited, "access denied")
		if s.metrics != nil {
			s.metrics.AuthzDenied.WithLabelValues(authz.ActionSSHConnect).Inc()
		}
		return
	}
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()
	go ssh.DiscardRequests(requests)
	dialer := net.Dialer{Timeout: s.cfg.DialTimeout}
	target := net.JoinHostPort(server.Address, fmt.Sprint(server.Port))
	netConn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return
	}
	defer netConn.Close()
	if s.audit != nil {
		_ = s.audit.Write(ctx, audit.Event{
			Category: audit.CategorySSH, Action: "ssh.tcpip", ActorID: user.ID, ActorName: user.Username,
			Result: audit.ResultSuccess, ResourceType: "server", ResourceID: server.ID,
			SourceIP: perms.Extensions[PermSourceIP],
			Details:  map[string]any{"destination": target},
		})
	}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(netConn, channel)
		if tcp, ok := netConn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(channel, netConn)
		_ = channel.CloseWrite()
		done <- struct{}{}
	}()
	<-done
	<-done
}

func (s *Server) dialDestination(ctx context.Context, server *model.Server, account string) (*ssh.Client, error) {
	signed, err := s.signer.Sign(ctx, ca.SignRequest{
		PublicKey:  s.cfg.GatewaySigner.PublicKey(),
		Principals: []string{account},
		CertType:   "user",
		TTL:        s.cfg.DestinationCertTTL,
		KeyID:      "rscon-gateway:" + server.Name,
		Extensions: map[string]string{
			ExtRole:   RoleGateway,
			ExtTarget: server.ID + ":" + account,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("obtain gateway certificate: %w", err)
	}
	certSigner, err := ssh.NewCertSigner(signed.Certificate, s.cfg.GatewaySigner)
	if err != nil {
		return nil, fmt.Errorf("build gateway certificate signer: %w", err)
	}
	addr := net.JoinHostPort(server.Address, fmt.Sprint(server.Port))
	dialer := net.Dialer{Timeout: s.cfg.DialTimeout}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial destination: %w", err)
	}
	_ = netConn.SetDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
	clientConfig := &ssh.ClientConfig{
		User:            account,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(certSigner)},
		HostKeyCallback: s.hosts.Callback(server.HostKeyFingerprint),
		Timeout:         s.cfg.DialTimeout,
		ClientVersion:   "SSH-2.0-RSCON",
	}
	conn, chans, reqs, err := ssh.NewClientConn(netConn, addr, clientConfig)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("SSH handshake with destination: %w", err)
	}
	_ = netConn.SetDeadline(time.Time{})
	return ssh.NewClient(conn, chans, reqs), nil
}

type sessionRuntime struct {
	once        sync.Once
	mu          sync.Mutex
	reasonText  string
	terminateFn func(string)
}

func (r *sessionRuntime) setReason(reason string) {
	r.mu.Lock()
	r.reasonText = reason
	r.mu.Unlock()
}

func (r *sessionRuntime) reason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reasonText
}

func exitStatusFrom(err error) (*int, bool) {
	if err == nil {
		zero := 0
		return &zero, false
	}
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitStatus()
		return &code, false
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return nil, false
	}
	return nil, true
}

func derefStatus(status *int) int {
	if status == nil {
		return 0
	}
	return *status
}

type recProxy interface {
	Input(p []byte) error
	Output(p []byte) error
}

type recorderSink struct {
	mu     sync.Mutex
	w      recording.Writer
	failed bool
	closed bool
}

func (r *recorderSink) Input(p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil || r.failed || r.closed {
		return nil
	}
	if err := r.w.Input(p); err != nil {
		r.failed = true
		return err
	}
	return nil
}

func (r *recorderSink) Output(p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil || r.failed || r.closed {
		return nil
	}
	if err := r.w.Output(p); err != nil {
		r.failed = true
		return err
	}
	return nil
}

func (r *recorderSink) Close(ctx context.Context) (recording.Object, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.w == nil {
		return recording.Object{}, errors.New("recording was not started")
	}
	return r.w.Close(ctx)
}

type lockedChannelWriter struct {
	mu       sync.Mutex
	channel  ssh.Channel
	recorder *recorderSink
}

func (w *lockedChannelWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.channel.Write(p)
	if n > 0 {
		_ = w.recorder.Output(p[:n])
	}
	return n, err
}

type recordingWriter struct {
	dst io.Writer
	rec *recorderSink
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	_ = w.rec.Input(p)
	return w.dst.Write(p)
}

type touchReader struct {
	reader io.Reader
	touch  func()
}

func (r touchReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.touch()
	}
	return n, err
}
