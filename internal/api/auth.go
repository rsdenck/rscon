package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/store"
)

const (
	sessionCookie = "rscon_session"
	csrfCookie    = "rscon_csrf"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status == http.StatusNoContent {
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// dummyPasswordHash returns a valid Argon2id hash used to equalise login timing
// for unknown usernames. It contains no secret and is never accepted.
func dummyPasswordHash() string {
	dummyOnce.Do(func() {
		dummyHash, _ = identity.HashPassword("rscon-not-a-real-password")
	})
	return dummyHash
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func pageParams(r *http.Request) (int, int) {
	limit := parseInt(r.URL.Query().Get("limit"), 50)
	offset := parseInt(r.URL.Query().Get("offset"), 0)
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func paged(w http.ResponseWriter, items any, total, limit, offset int) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "limit": limit, "offset": offset,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.Allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", strconv.Itoa(int(s.cfg.LoginRateWindow.Seconds())))
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil || user.PasswordHash == "" {
		// Spend comparable time on unknown users to reduce enumeration.
		identity.VerifyPassword(req.Password, dummyPasswordHash())
		s.auditEvent(r, audit.CategoryAuth, "auth.login", audit.ResultDenied, nil, "invalid credentials", map[string]any{"username": req.Username})
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		s.auditEvent(r, audit.CategoryAuth, "auth.login", audit.ResultDenied, user, "account locked", nil)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.Active() || !identity.VerifyPassword(req.Password, user.PasswordHash) {
		_ = s.store.RecordLoginFailure(r.Context(), user.ID, s.cfg.LockoutThreshold, s.cfg.LockoutDuration)
		s.auditEvent(r, audit.CategoryAuth, "auth.login", audit.ResultDenied, user, "invalid credentials", nil)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if user.MFAEnrolled {
		// MFA enrollment is metadata only until a second-factor authenticator
		// is implemented; refusing here prevents a false sense of security.
		s.auditEvent(r, audit.CategoryAuth, "auth.login", audit.ResultDenied, user, "second factor is not implemented", nil)
		writeError(w, http.StatusForbidden, "this account requires a second factor, which is not yet implemented")
		return
	}
	if err := s.store.RecordLoginSuccess(r.Context(), user.ID); err != nil {
		s.log.Error("record login success failed", "error", err)
	}
	raw, err := identity.NewToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	csrf, err := identity.NewToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	expires := time.Now().UTC().Add(s.cfg.SessionTTL)
	token := &model.APIToken{
		ID: identity.NewID(), UserID: user.ID, Kind: model.TokenSession,
		TokenHash: identity.HashToken(raw), ExpiresAt: expires,
	}
	if err := s.store.CreateToken(r.Context(), token); err != nil {
		s.log.Error("create session token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	s.setCookie(w, sessionCookie, raw, expires, true)
	s.setCookie(w, csrfCookie, csrf, expires, false)
	principal, err := s.authz.LoadPrincipal(r.Context(), user, user.MFAEnrolled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authorization lookup failed")
		return
	}
	s.auditEvent(r, audit.CategoryAuth, "auth.login", audit.ResultSuccess, user, "local password login", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user, "permissions": permissionList(principal), "roles": principal.Roles,
		"csrf_token": csrf, "expires_at": expires,
	})
}

func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	user, principal, ok := s.authenticateNoWrite(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true, "user": user, "permissions": permissionList(principal),
		"roles": principal.Roles,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if token, err := s.store.GetTokenByHash(r.Context(), identity.HashToken(cookie.Value)); err == nil {
			_ = s.store.RevokeToken(r.Context(), token.ID, token.UserID)
			if user, err := s.store.GetUserByID(r.Context(), token.UserID); err == nil {
				s.auditEvent(r, audit.CategoryAuth, "auth.logout", audit.ResultSuccess, user, "session ended", nil)
			}
		}
	}
	// Clearing both cookies is idempotent, so logout succeeds even when the
	// session had already expired.
	s.clearCookie(w, sessionCookie)
	s.clearCookie(w, csrfCookie)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, user *model.User, principal model.Principal) {
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user, "permissions": permissionList(principal), "roles": principal.Roles,
		"groups": principal.GroupIDs, "mfa": principal.MFA,
	})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	counts, err := s.store.CountDashboard(r.Context())
	if err != nil {
		s.log.Error("dashboard query failed", "error", err)
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func permissionList(p model.Principal) []string {
	out := make([]string, 0, len(p.Permissions))
	for perm := range p.Permissions {
		out = append(out, perm)
	}
	sort.Strings(out)
	return out
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, expires time.Time, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
		HttpOnly: httpOnly, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: name == sessionCookie, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) authenticateNoWrite(r *http.Request) (*model.User, model.Principal, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, model.Principal{}, false
	}
	token, err := s.store.GetTokenByHash(r.Context(), identity.HashToken(cookie.Value))
	if err != nil || token.Kind != model.TokenSession {
		return nil, model.Principal{}, false
	}
	user, err := s.store.GetUserByID(r.Context(), token.UserID)
	if err != nil || !user.Active() {
		return nil, model.Principal{}, false
	}
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		return nil, model.Principal{}, false
	}
	principal, err := s.authz.LoadPrincipal(r.Context(), user, user.MFAEnrolled)
	if err != nil {
		return nil, model.Principal{}, false
	}
	return user, principal, true
}
