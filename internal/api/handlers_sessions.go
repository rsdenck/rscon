package api

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/store"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func canViewAllSessions(p model.Principal) bool {
	return p.Permissions["*"] || p.HasPermission(authz.ActionSessionTerminate) || p.HasPermission(authz.ActionAuditRead)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	limit, offset := pageParams(r)
	userID := r.URL.Query().Get("user_id")
	if !canViewAllSessions(principal) {
		userID = actor.ID
	}
	sessions, total, err := s.store.ListSessions(r.Context(), userID,
		r.URL.Query().Get("server_id"), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	paged(w, sessions, total, limit, offset)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	session, err := s.store.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if session.UserID != actor.ID && !canViewAllSessions(principal) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) terminateSession(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	session, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if session.Status != model.SessionActive {
		writeError(w, http.StatusConflict, "session is not active")
		return
	}
	// The database flag covers distributed gateways; the local call short
	// circuits the usual polling delay when this process owns the session.
	if err := s.store.RequestTermination(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	local := false
	if s.gateway != nil {
		local = s.gateway.TerminateSession(id)
	}
	s.auditEvent(r, audit.CategorySession, "session.terminate_requested", audit.ResultSuccess, actor,
		"session termination requested", map[string]any{"session_id": id, "local": local, "owner": session.Gateway})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "local": local})
}

func (s *Server) terminalWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error("terminal WebSocket upgrade failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusBadRequest, "websocket upgrade failed")
		return
	}
	defer conn.Close()

	s.log.Info("terminal WebSocket connected", "session_id", sessionID)

	// TODO: Stream SSH output from the session gateway
	// For now, send a placeholder until the gateway integration is complete
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			// Poll for session status/output
			session, err := s.store.GetSession(r.Context(), sessionID)
			if err != nil {
				s.log.Error("get session failed", "session_id", sessionID, "error", err)
				conn.WriteMessage(1, []byte("error: unable to fetch session"))
				return
			}
			if session.Status != model.SessionActive {
				conn.WriteMessage(1, []byte("session ended"))
				return
			}
			// Send a keepalive/prompt message
			conn.WriteMessage(1, []byte("\n$ "))
			// Wait a bit before next poll
			time.Sleep(2 * time.Second)
		}
	}
}

func (s *Server) downloadRecording(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	session, err := s.store.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if session.UserID != actor.ID && !canViewAllSessions(principal) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if session.RecordingID == "" {
		writeError(w, http.StatusNotFound, "this session has no recording")
		return
	}
	recording, err := s.store.GetRecording(r.Context(), session.RecordingID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.recs == nil {
		writeError(w, http.StatusServiceUnavailable, "recording storage is not configured")
		return
	}
	body, err := s.recs.Open(r.Context(), recording.ObjectKey)
	if err != nil {
		s.log.Error("recording open failed", "recording_id", recording.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "recording is unavailable")
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/x-asciicast")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "session-"+session.ID+".cast"))
	w.Header().Set("X-RSCON-Recording-SHA256", recording.SHA256)
	if recording.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(recording.SizeBytes, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, body); err != nil {
		s.log.Warn("recording stream interrupted", "recording_id", recording.ID, "error", err)
	}
	s.auditEvent(r, audit.CategoryAudit, "recording.downloaded", audit.ResultSuccess, actor,
		"session recording downloaded", map[string]any{
			"session_id": session.ID, "recording_id": recording.ID, "sha256": recording.SHA256,
		})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	limit, offset := pageParams(r)
	filter := store.AuditFilter{
		Category: r.URL.Query().Get("category"),
		Action:   r.URL.Query().Get("action"),
		ActorID:  r.URL.Query().Get("actor_id"),
		Result:   r.URL.Query().Get("result"),
		Limit:    limit, Offset: offset,
	}
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be RFC3339")
			return
		}
		filter.From = &t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
		filter.To = &t
	}
	events, total, err := s.store.ListAudit(r.Context(), filter)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	paged(w, events, total, limit, offset)
}

func (s *Server) verifyAudit(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	verification, err := s.store.VerifyAuditChain(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	result := audit.ResultSuccess
	if !verification.Valid {
		result = audit.ResultFailure
	}
	s.auditEvent(r, audit.CategoryAudit, "audit.chain_verified", result, actor,
		"audit hash chain verified", map[string]any{
			"events": verification.Events, "valid": verification.Valid,
			"first_invalid_id": verification.FirstInvalidID,
		})
	writeJSON(w, http.StatusOK, verification)
}
