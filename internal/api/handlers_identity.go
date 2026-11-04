package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
	"github.com/rsdenck/rscon/internal/store"
)

var oidcStateStore = make(map[string]string)

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	clientID := s.cfg.OIDCClientID
	authURLValue := s.cfg.OIDCAuthURL
	tokenURL := s.cfg.OIDCTokenURL
	redirectURL := s.cfg.OIDCRedirectURL

	if clientID == "" || authURLValue == "" || tokenURL == "" || redirectURL == "" {
		writeError(w, http.StatusInternalServerError, "OIDC not configured")
		return
	}

	// Generate PKCE
	verifier, challenge := identity.PKCE()

	// Generate state
	state := identity.NewID()[:16]

	// Store state -> verifier mapping for callback
	oidcStateStore[state] = verifier

	// Build authorization URL
	authURL, err := identity.OIDCAuthorizationURL(
		authURLValue,
		clientID,
		redirectURL,
		state,
		verifier,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build authorization URL")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authorization_url": authURL.String(),
		"state":             state,
		"code_challenge":    challenge,
		"code_challenge_method": "S256",
	})
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Exchange authorization code for tokens
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	// Look up stored verifier for this state
	codeVerifier, ok := oidcStateStore[state]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}
	// Remove used state
	delete(oidcStateStore, state)

	// Exchange code + verifier for tokens
	// TODO: Implement actual token exchange with the OIDC provider
	// body := url.Values{
	//     "grant_type": "authorization_code",
	//     "client_id":  cfg.ClientID,
	//     "code":       code,
	//     "code_verifier": codeVerifier,
	//     "redirect_uri": cfg.RedirectURL,
	// }
	// resp, err := http.PostForm(cfg.TokenURL, body)
	// etc.

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "oidc login in progress",
		"code":     code,
		"state":    state,
		"verifier": codeVerifier,
	})
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{1,63}$`)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	limit, offset := pageParams(r)
	users, total, err := s.store.ListUsers(r.Context(), r.URL.Query().Get("search"), limit, offset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	paged(w, users, total, limit, offset)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Username    string   `json:"username"`
		Email       string   `json:"email"`
		DisplayName string   `json:"display_name"`
		Password    string   `json:"password"`
		RoleNames   []string `json:"role_names"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(req.Username) {
		writeError(w, http.StatusBadRequest, "username must be 2-64 characters of letters, digits, dot, underscore or hyphen")
		return
	}
	if req.Password != "" && len(req.Password) < 12 {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	roles := make([]model.Role, 0, len(req.RoleNames))
	for _, name := range req.RoleNames {
		role, err := s.store.GetRoleByName(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "unknown role: "+name)
			return
		}
		roles = append(roles, *role)
	}
	hash := ""
	if req.Password != "" {
		var err error
		hash, err = identity.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	user := &model.User{
		ID: identity.NewID(), Username: req.Username, Email: req.Email,
		DisplayName: req.DisplayName, PasswordHash: hash, Status: model.StatusActive,
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	if err := s.store.CreateUser(r.Context(), user); err != nil {
		writeStoreError(w, err)
		return
	}
	for _, role := range roles {
		_ = s.store.BindRole(r.Context(), &model.RoleBinding{
			ID: identity.NewID(), RoleID: role.ID, SubjectType: "user", SubjectID: user.ID,
		})
	}
	s.auditEvent(r, audit.CategoryAdmin, "user.created", audit.ResultSuccess, actor, "user created",
		map[string]any{"user_id": user.ID, "username": user.Username, "roles": req.RoleNames})
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	user, err := s.store.GetUserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Email       *string `json:"email"`
		DisplayName *string `json:"display_name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	user, err := s.store.GetUserByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	email, name := user.Email, user.DisplayName
	if req.Email != nil {
		email = *req.Email
	}
	if req.DisplayName != nil {
		name = *req.DisplayName
	}
	if err := s.store.UpdateUserProfile(r.Context(), id, email, name); err != nil {
		writeStoreError(w, err)
		return
	}
	user.Email, user.DisplayName = email, name
	s.auditEvent(r, audit.CategoryAdmin, "user.updated", audit.ResultSuccess, actor, "user updated",
		map[string]any{"user_id": id})
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if id == actor.ID {
		writeError(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "user.deleted", audit.ResultSuccess, actor, "user soft-deleted",
		map[string]any{"user_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) setUserStatus(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if id == actor.ID && req.Status != model.StatusActive {
		writeError(w, http.StatusConflict, "you cannot disable your own account")
		return
	}
	if err := s.store.SetUserStatus(r.Context(), id, req.Status, true); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "user.status_changed", audit.ResultSuccess, actor, "user status changed",
		map[string]any{"user_id": id, "status": req.Status})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": req.Status})
}

func (s *Server) setUserPassword(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Password) < 12 {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	hash, err := identity.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	if err := s.store.SetPasswordHash(r.Context(), id, hash); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = s.store.RevokeAllTokensForUser(r.Context(), id)
	s.auditEvent(r, audit.CategorySecurity, "user.password_reset", audit.ResultSuccess, actor,
		"password reset by administrator", map[string]any{"user_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) getUserGroups(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if _, err := s.store.GetUserByID(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	groups, err := s.store.ListUserGroupIDs(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_ids": groups})
}

func (s *Server) setUserGroups(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		GroupIDs []string `json:"group_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	current, err := s.store.ListUserGroupIDs(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	want := map[string]bool{}
	for _, gid := range req.GroupIDs {
		want[gid] = true
	}
	have := map[string]bool{}
	for _, gid := range current {
		have[gid] = true
		if !want[gid] {
			_ = s.store.RemoveUserFromGroup(r.Context(), id, gid)
		}
	}
	for gid := range want {
		if !have[gid] {
			if err := s.store.AddUserToGroup(r.Context(), id, gid); err != nil {
				writeStoreError(w, err)
				return
			}
		}
	}
	s.auditEvent(r, audit.CategoryAdmin, "user.groups_changed", audit.ResultSuccess, actor,
		"group membership replaced", map[string]any{"user_id": id, "group_ids": req.GroupIDs})
	writeJSON(w, http.StatusOK, map[string]any{"group_ids": req.GroupIDs})
}

func (s *Server) getUserBindings(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	id := r.PathValue("id")
	bindings, err := s.store.ListBindingsForSubjects(r.Context(), id, nil)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": bindings})
}

func (s *Server) listUserKeys(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	keys, err := s.store.ListUserPublicKeys(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *Server) addUserKey(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(req.PublicKey)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid OpenSSH public key")
		return
	}
	if _, ok := key.(*ssh.Certificate); ok {
		writeError(w, http.StatusBadRequest, "certificates cannot be registered as user keys")
		return
	}
	if req.Name == "" {
		req.Name = "key"
	}
	userID := r.PathValue("id")
	record := &model.UserPublicKey{
		ID: identity.NewID(), UserID: userID, Name: req.Name,
		PublicKey: strings.TrimSpace(req.PublicKey), Fingerprint: ssh.FingerprintSHA256(key),
	}
	if err := s.store.AddUserPublicKey(r.Context(), record); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategorySecurity, "user.key_added", audit.ResultSuccess, actor,
		"public key registered", map[string]any{"user_id": userID, "fingerprint": record.Fingerprint})
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) deleteUserKey(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	userID := r.PathValue("id")
	keyID := r.PathValue("keyID")
	if err := s.store.DeleteUserPublicKey(r.Context(), userID, keyID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategorySecurity, "user.key_removed", audit.ResultSuccess, actor,
		"public key removed", map[string]any{"user_id": userID, "key_id": keyID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	groups, err := s.store.ListGroups(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": groups})
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	group := &model.Group{ID: identity.NewID(), Name: req.Name, Description: req.Description}
	if err := s.store.CreateGroup(r.Context(), group); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "group.created", audit.ResultSuccess, actor, "group created",
		map[string]any{"group_id": group.ID, "name": group.Name})
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	group := &model.Group{ID: id, Name: req.Name, Description: req.Description}
	if err := s.store.UpdateGroup(r.Context(), group); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "group.updated", audit.ResultSuccess, actor, "group updated",
		map[string]any{"group_id": id})
	writeJSON(w, http.StatusOK, group)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if err := s.store.DeleteGroup(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "group.deleted", audit.ResultSuccess, actor, "group deleted",
		map[string]any{"group_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listGroupMembers(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	members, err := s.store.ListGroupMemberIDs(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_ids": members})
}

func (s *Server) addGroupMember(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	groupID := r.PathValue("id")
	if err := s.store.AddUserToGroup(r.Context(), req.UserID, groupID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "group.member_added", audit.ResultSuccess, actor,
		"group member added", map[string]any{"group_id": groupID, "user_id": req.UserID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) removeGroupMember(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	groupID := r.PathValue("id")
	userID := r.PathValue("userID")
	if err := s.store.RemoveUserFromGroup(r.Context(), userID, groupID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "group.member_removed", audit.ResultSuccess, actor,
		"group member removed", map[string]any{"group_id": groupID, "user_id": userID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	roles, err := s.store.ListRoles(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": roles})
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	perms, err := s.store.ListPermissions(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": perms})
}

func (s *Server) listRoleBindings(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	bindings, err := s.store.ListRoleBindings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": bindings})
}

func (s *Server) createRoleBinding(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		RoleID      string `json:"role_id"`
		RoleName    string `json:"role_name"`
		SubjectType string `json:"subject_type"`
		SubjectID   string `json:"subject_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	roleID := req.RoleID
	if roleID == "" && req.RoleName != "" {
		role, err := s.store.GetRoleByName(r.Context(), req.RoleName)
		if err != nil {
			writeError(w, http.StatusBadRequest, "unknown role")
			return
		}
		roleID = role.ID
	}
	if roleID == "" {
		writeError(w, http.StatusBadRequest, "role_id or role_name is required")
		return
	}
	if _, err := s.store.GetRoleByID(r.Context(), roleID); err != nil {
		writeError(w, http.StatusBadRequest, "unknown role")
		return
	}
	if err := s.store.EnsureSubjectExists(r.Context(), req.SubjectType, req.SubjectID); err != nil {
		writeStoreError(w, err)
		return
	}
	binding := &model.RoleBinding{
		ID: identity.NewID(), RoleID: roleID, SubjectType: req.SubjectType, SubjectID: req.SubjectID,
	}
	if err := s.store.BindRoleToSubject(r.Context(), binding); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "role.bound", audit.ResultSuccess, actor, "role binding created",
		map[string]any{"role_id": roleID, "subject_type": req.SubjectType, "subject_id": req.SubjectID})
	writeJSON(w, http.StatusCreated, binding)
}

func (s *Server) deleteRoleBinding(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if err := s.store.DeleteRoleBinding(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "role.unbound", audit.ResultSuccess, actor, "role binding removed",
		map[string]any{"binding_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	tokens, err := s.store.ListTokensForUser(r.Context(), actor.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tokens})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		ExpiresInDays int `json:"expires_in_days"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if req.ExpiresInDays == 0 {
		req.ExpiresInDays = 90
	}
	if req.ExpiresInDays < 1 || req.ExpiresInDays > 365 {
		writeError(w, http.StatusBadRequest, "expires_in_days must be between 1 and 365")
		return
	}
	raw, err := identity.NewToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create token")
		return
	}
	token := &model.APIToken{
		ID: identity.NewID(), UserID: actor.ID, Kind: model.TokenAPI,
		TokenHash: identity.HashToken(raw),
		ExpiresAt: time.Now().UTC().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour),
	}
	if err := s.store.CreateToken(r.Context(), token); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategorySecurity, "token.created", audit.ResultSuccess, actor,
		"API token created", map[string]any{"token_id": token.ID, "expires_at": token.ExpiresAt})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": token.ID, "token": raw, "expires_at": token.ExpiresAt,
		"warning": "this token is shown once and cannot be recovered",
	})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request, actor *model.User, principal model.Principal) {
	id := r.PathValue("id")
	userID := actor.ID
	if principal.HasPermission(authz.ActionUserManage) && r.URL.Query().Get("user_id") != "" {
		userID = r.URL.Query().Get("user_id")
	}
	if err := s.store.RevokeToken(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategorySecurity, "token.revoked", audit.ResultSuccess, actor,
		"API token revoked", map[string]any{"token_id": id, "user_id": userID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
