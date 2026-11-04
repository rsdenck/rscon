package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/ca"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/store"
)

type accessRequestInput struct {
	ServerID         string `json:"server_id"`
	ServerGroupID    string `json:"server_group_id"`
	EnvironmentID    string `json:"environment_id"`
	Account          string `json:"account"`
	Reason           string `json:"reason"`
	Ticket           string `json:"ticket"`
	RequestedSeconds int    `json:"requested_seconds"`
}

func (s *Server) listAccessRequests(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	limit, offset := pageParams(r)
	requesterID := r.URL.Query().Get("requester_id")
	// Users without a broad access.read capability stay limited to their own
	// requests even if a future role grants the route accidentally.
	if requesterID == "" && !principal.HasPermission(authz.ActionAccessApprove) {
		requesterID = actor.ID
	}
	requests, total, err := s.store.ListAccessRequests(r.Context(), r.URL.Query().Get("status"), requesterID, limit, offset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	paged(w, requests, total, limit, offset)
}

func (s *Server) createAccessRequest(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	var req accessRequestInput
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Account == "" || !accountPattern.MatchString(req.Account) {
		writeError(w, http.StatusBadRequest, "a valid account is required")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	if req.ServerID == "" && req.ServerGroupID == "" && req.EnvironmentID == "" {
		writeError(w, http.StatusBadRequest, "server_id, server_group_id or environment_id is required")
		return
	}
	resource := authz.Resource{Type: "server", Account: req.Account, EnvironmentID: req.EnvironmentID}
	if req.ServerID != "" {
		server, err := s.store.GetServerByID(r.Context(), req.ServerID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !server.Enabled {
			writeError(w, http.StatusBadRequest, "destination is disabled")
			return
		}
		groups, err := s.store.ListServerGroupIDs(r.Context(), server.ID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		resource.ID, resource.Name, resource.EnvironmentID, resource.GroupIDs = server.ID, server.Name, server.EnvironmentID, groups
	}
	if req.ServerGroupID != "" {
		resource.GroupIDs = append(resource.GroupIDs, req.ServerGroupID)
	}
	if req.RequestedSeconds <= 0 {
		req.RequestedSeconds = 3600
	}
	if req.RequestedSeconds < 60 || req.RequestedSeconds > 7*24*3600 {
		writeError(w, http.StatusBadRequest, "requested_seconds must be between 60 and 604800")
		return
	}
	decision, err := s.authz.Authorize(r.Context(), principal, authz.ActionAccessRequest, resource, authz.Options{SkipApprovalCheck: true})
	if err != nil {
		s.log.Error("access request authorization failed", "error", err)
		writeError(w, http.StatusInternalServerError, "authorization failed")
		return
	}
	if !decision.Allowed {
		s.auditEvent(r, audit.CategoryAuthz, "access.request_denied", audit.ResultDenied, actor, decision.Reason, nil)
		writeError(w, http.StatusForbidden, decision.Reason)
		return
	}
	if decision.MaxDuration > 0 && time.Duration(req.RequestedSeconds)*time.Second > decision.MaxDuration {
		req.RequestedSeconds = int(decision.MaxDuration.Seconds())
	}
	required := decision.ApprovalsRequired
	if required < 1 {
		required = 1
	}
	request := &model.AccessRequest{
		ID: identity.NewID(), RequesterID: actor.ID, ServerID: req.ServerID,
		ServerGroupID: req.ServerGroupID, EnvironmentID: req.EnvironmentID,
		Account: req.Account, Reason: strings.TrimSpace(req.Reason), Ticket: req.Ticket,
		RequestedSeconds: req.RequestedSeconds, ApprovalsRequired: required,
	}
	if err := s.store.CreateAccessRequest(r.Context(), request); err != nil {
		writeStoreError(w, err)
		return
	}
	if !decision.ApprovalRequired {
		request, err = s.store.AutoApproveAccessRequest(r.Context(), request.ID, req.RequestedSeconds)
		if err != nil {
			writeStoreError(w, err)
			return
		}
	}
	s.auditEvent(r, audit.CategoryApproval, "access.requested", audit.ResultSuccess, actor,
		"just-in-time access requested", map[string]any{
			"request_id": request.ID, "server_id": req.ServerID, "account": req.Account,
			"approval_required": decision.ApprovalRequired, "requested_seconds": req.RequestedSeconds,
		})
	writeJSON(w, http.StatusCreated, request)
}

func (s *Server) getAccessRequest(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	request, err := s.store.GetAccessRequest(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if request.RequesterID != actor.ID && !principal.HasPermission(authz.ActionAccessApprove) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	approvals, err := s.store.ListAccessApprovals(r.Context(), request.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": request, "approvals": approvals})
}

func (s *Server) decideAccessRequest(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	request, err := s.store.DecideAccessRequest(r.Context(), r.PathValue("id"), actor.ID,
		req.Decision, req.Comment, s.cfg.AllowSelfApproval)
	if err != nil {
		if errors.Is(err, store.ErrForbidden) {
			s.auditEvent(r, audit.CategoryApproval, "access.self_approval_denied", audit.ResultDenied, actor, err.Error(), nil)
		}
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryApproval, "access."+req.Decision, audit.ResultSuccess, actor,
		"access request "+req.Decision+"d", map[string]any{
			"request_id": request.ID, "status": request.Status, "comment": req.Comment,
		})
	writeJSON(w, http.StatusOK, request)
}

func (s *Server) cancelAccessRequest(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	request, err := s.store.CancelAccessRequest(r.Context(), r.PathValue("id"), actor.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryApproval, "access.canceled", audit.ResultSuccess, actor,
		"access request canceled", map[string]any{"request_id": request.ID})
	writeJSON(w, http.StatusOK, request)
}

func (s *Server) listAccessApprovals(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	approvals, err := s.store.ListAccessApprovals(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": approvals})
}

func (s *Server) listCertificates(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	limit, offset := pageParams(r)
	userID := r.URL.Query().Get("user_id")
	if !principal.HasPermission(authz.ActionCertificateRead) {
		userID = actor.ID
	}
	certs, total, err := s.store.ListCertificates(r.Context(), userID, limit, offset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	paged(w, certs, total, limit, offset)
}

func (s *Server) issueCertificate(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	var req struct {
		ServerID   string `json:"server_id"`
		Account    string `json:"account"`
		PublicKey  string `json:"public_key"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(req.PublicKey)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "public_key must be an OpenSSH public key")
		return
	}
	if _, ok := publicKey.(*ssh.Certificate); ok {
		writeError(w, http.StatusBadRequest, "public_key must not be a certificate")
		return
	}
	if req.Account == "" || !accountPattern.MatchString(req.Account) {
		writeError(w, http.StatusBadRequest, "a valid account is required")
		return
	}
	server, err := s.store.GetServerByID(r.Context(), req.ServerID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !server.Enabled {
		writeError(w, http.StatusBadRequest, "destination is disabled")
		return
	}
	groups, err := s.store.ListServerGroupIDs(r.Context(), server.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	decision, err := s.authz.Authorize(r.Context(), principal, authz.ActionSSHConnect, authz.Resource{
		Type: "server", ID: server.ID, Name: server.Name, EnvironmentID: server.EnvironmentID,
		GroupIDs: groups, Account: req.Account, Channel: "session",
	}, authz.Options{Channel: "session"})
	if err != nil {
		s.log.Error("certificate authorization failed", "error", err)
		writeError(w, http.StatusInternalServerError, "authorization failed")
		return
	}
	if !decision.Allowed {
		s.auditEvent(r, audit.CategoryAuthz, "certificate.denied", audit.ResultDenied, actor, decision.Reason,
			map[string]any{"server_id": server.ID, "account": req.Account})
		writeError(w, http.StatusForbidden, decision.Reason)
		return
	}
	ttl := s.cfg.DefaultCertTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if ttl > s.cfg.MaxCertTTL {
		ttl = s.cfg.MaxCertTTL
	}
	if ttl < time.Minute {
		writeError(w, http.StatusBadRequest, "ttl_seconds must be at least 60")
		return
	}
	if decision.MaxDuration > 0 && ttl > decision.MaxDuration {
		ttl = decision.MaxDuration
	}
	signed, err := s.signer.Sign(r.Context(), ca.SignRequest{
		PublicKey:  publicKey,
		Principals: []string{actor.Username, req.Account},
		CertType:   "user",
		TTL:        ttl,
		KeyID:      actor.Username,
		Extensions: map[string]string{
			"rscon-role": "user", "rscon-target": server.ID + ":" + req.Account,
			"rscon-mfa": boolString(principal.MFA), "permit-pty": "", "permit-user-rc": "",
		},
	})
	if err != nil {
		s.log.Error("certificate signing failed", "error", err)
		writeError(w, http.StatusBadGateway, "certificate authority is unavailable")
		return
	}
	cert := &model.Certificate{
		ID: identity.NewID(), Serial: signed.Serial, UserID: actor.ID, ServerID: server.ID,
		Account: req.Account, KeyID: actor.Username,
		PublicKeyFingerprint: ssh.FingerprintSHA256(publicKey), CertPEM: string(signed.PEM),
		CA: s.signer.Name(), ValidAfter: time.Unix(int64(signed.Certificate.ValidAfter), 0).UTC(),
		ValidBefore: time.Unix(int64(signed.Certificate.ValidBefore), 0).UTC(),
	}
	// The gateway also accepts certificates by serial; persist before use.
	if err := s.store.CreateCertificate(r.Context(), cert); err != nil {
		writeStoreError(w, err)
		return
	}
	if s.metrics != nil {
		s.metrics.CertIssued.Inc()
	}
	s.auditEvent(r, audit.CategoryCertificate, "certificate.issued", audit.ResultSuccess, actor,
		"short-lived SSH certificate issued", map[string]any{
			"certificate_id": cert.ID, "serial": cert.Serial, "server_id": server.ID,
			"account": req.Account, "valid_before": cert.ValidBefore,
		})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": cert.ID, "serial": cert.Serial, "certificate": string(signed.PEM),
		"ca_public_key": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signed.CA))),
		"valid_after":   cert.ValidAfter, "valid_before": cert.ValidBefore,
		"principals": signed.Certificate.ValidPrincipals,
	})
}

func (s *Server) revokeCertificate(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if strings.TrimSpace(req.Reason) == "" {
		req.Reason = "revoked by administrator"
	}
	cert, err := s.store.RevokeCertificate(r.Context(), r.PathValue("id"), req.Reason)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.metrics != nil {
		s.metrics.CertRevoked.Inc()
	}
	s.auditEvent(r, audit.CategoryCertificate, "certificate.revoked", audit.ResultSuccess, actor,
		"SSH certificate revoked", map[string]any{"certificate_id": cert.ID, "serial": cert.Serial, "reason": req.Reason})
	writeJSON(w, http.StatusOK, cert)
}

func (s *Server) revokeUserCertificates(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if strings.TrimSpace(req.Reason) == "" {
		req.Reason = "revoked by administrator"
	}
	userID := r.PathValue("userID")
	count, err := s.store.RevokeUserCertificates(r.Context(), userID, req.Reason)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.metrics != nil {
		s.metrics.CertRevoked.Add(float64(count))
	}
	s.auditEvent(r, audit.CategoryCertificate, "certificate.user_revoked", audit.ResultSuccess, actor,
		"all user certificates revoked", map[string]any{"user_id": userID, "count": count, "reason": req.Reason})
	writeJSON(w, http.StatusOK, map[string]any{"revoked": count})
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
