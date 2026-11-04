package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/rsdenck/rscon/internal/audit"
	"github.com/rsdenck/rscon/internal/authz"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
)

var (
	serverNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	accountPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	fingerprintPattern = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
)

func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	envs, err := s.store.ListEnvironments(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": envs})
}

func (s *Server) createEnvironment(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Critical    bool   `json:"critical"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	env := &model.Environment{ID: identity.NewID(), Name: req.Name, Description: req.Description, Critical: req.Critical}
	if err := s.store.CreateEnvironment(r.Context(), env); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "environment.created", audit.ResultSuccess, actor,
		"environment created", map[string]any{"environment_id": env.ID, "name": env.Name})
	writeJSON(w, http.StatusCreated, env)
}

func (s *Server) listServerGroups(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	groups, err := s.store.ListServerGroups(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": groups})
}

func (s *Server) createServerGroup(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
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
	group := &model.ServerGroup{ID: identity.NewID(), Name: req.Name, Description: req.Description}
	if err := s.store.CreateServerGroup(r.Context(), group); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "server_group.created", audit.ResultSuccess, actor,
		"server group created", map[string]any{"server_group_id": group.ID, "name": group.Name})
	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) deleteServerGroup(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if err := s.store.DeleteServerGroup(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "server_group.deleted", audit.ResultSuccess, actor,
		"server group deleted", map[string]any{"server_group_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	limit, offset := pageParams(r)
	enabledOnly := r.URL.Query().Get("enabled") == "true"
	servers, total, err := s.store.ListServers(r.Context(),
		r.URL.Query().Get("environment_id"), r.URL.Query().Get("group_id"),
		r.URL.Query().Get("search"), enabledOnly, limit, offset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	paged(w, servers, total, limit, offset)
}

type serverRequest struct {
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	Address            string   `json:"address"`
	Port               int      `json:"port"`
	EnvironmentID      string   `json:"environment_id"`
	OSFamily           string   `json:"os_family"`
	Location           string   `json:"location"`
	Tags               []string `json:"tags"`
	DefaultAccount     string   `json:"default_account"`
	HostKeyFingerprint string   `json:"host_key_fingerprint"`
	Enabled            *bool    `json:"enabled"`
	GroupIDs           []string `json:"group_ids"`
}

func validateServerFields(name, address, account, fingerprint string, port int) string {
	if !serverNamePattern.MatchString(name) {
		return "name must be 1-128 characters of letters, digits, dot, underscore or hyphen"
	}
	if address == "" {
		return "address is required"
	}
	if port < 1 || port > 65535 {
		return "port must be between 1 and 65535"
	}
	if account != "" && !accountPattern.MatchString(account) {
		return "default_account is invalid"
	}
	if fingerprint != "" && !fingerprintPattern.MatchString(fingerprint) {
		return "host_key_fingerprint must look like SHA256:..."
	}
	return ""
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req serverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	if req.Address == "" {
		req.Address = req.Hostname
	}
	if msg := validateServerFields(req.Name, req.Address, req.DefaultAccount, req.HostKeyFingerprint, req.Port); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.EnvironmentID == "" {
		writeError(w, http.StatusBadRequest, "environment_id is required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	server := &model.Server{
		ID: identity.NewID(), Name: req.Name, Hostname: req.Hostname, Address: req.Address,
		Port: req.Port, EnvironmentID: req.EnvironmentID, OSFamily: req.OSFamily,
		Location: req.Location, Tags: req.Tags, DefaultAccount: req.DefaultAccount,
		HostKeyFingerprint: req.HostKeyFingerprint, Enabled: enabled,
	}
	if err := s.store.CreateServer(r.Context(), server, req.GroupIDs); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "server.created", audit.ResultSuccess, actor,
		"server registered", map[string]any{"server_id": server.ID, "name": server.Name, "address": server.Address})
	writeJSON(w, http.StatusCreated, server)
}

func (s *Server) getServer(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	server, err := s.store.GetServerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	groups, err := s.store.ListServerGroupIDs(r.Context(), server.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": server, "group_ids": groups})
}

func (s *Server) updateServer(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	server, err := s.store.GetServerByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req serverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != "" {
		server.Name = req.Name
	}
	if req.Hostname != "" {
		server.Hostname = req.Hostname
	}
	if req.Address != "" {
		server.Address = req.Address
	}
	if req.Port != 0 {
		server.Port = req.Port
	}
	if req.EnvironmentID != "" {
		server.EnvironmentID = req.EnvironmentID
	}
	if req.OSFamily != "" {
		server.OSFamily = req.OSFamily
	}
	if req.Location != "" {
		server.Location = req.Location
	}
	if req.Tags != nil {
		server.Tags = req.Tags
	}
	if req.DefaultAccount != "" {
		server.DefaultAccount = req.DefaultAccount
	}
	if req.HostKeyFingerprint != "" {
		server.HostKeyFingerprint = req.HostKeyFingerprint
	}
	if req.Enabled != nil {
		server.Enabled = *req.Enabled
	}
	if msg := validateServerFields(server.Name, server.Address, server.DefaultAccount, server.HostKeyFingerprint, server.Port); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateServer(r.Context(), server); err != nil {
		writeStoreError(w, err)
		return
	}
	if req.GroupIDs != nil {
		if err := s.store.SetServerGroups(r.Context(), server.ID, req.GroupIDs); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	s.auditEvent(r, audit.CategoryAdmin, "server.updated", audit.ResultSuccess, actor,
		"server updated", map[string]any{"server_id": server.ID, "name": server.Name})
	writeJSON(w, http.StatusOK, server)
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if err := s.store.DeleteServer(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "server.deleted", audit.ResultSuccess, actor,
		"server soft-deleted", map[string]any{"server_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) setServerGroups(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var req struct {
		GroupIDs []string `json:"group_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if err := s.store.SetServerGroups(r.Context(), id, req.GroupIDs); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "server.groups_changed", audit.ResultSuccess, actor,
		"server groups replaced", map[string]any{"server_id": id, "group_ids": req.GroupIDs})
	writeJSON(w, http.StatusOK, map[string]any{"group_ids": req.GroupIDs})
}

func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	policies, err := s.store.ListAccessPolicies(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": policies})
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request, _ *model.User, _ model.Principal) {
	policy, err := s.store.GetAccessPolicy(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func validatePolicy(policy *model.AccessPolicy) string {
	policy.Name = strings.TrimSpace(policy.Name)
	if err := authz.ValidatePolicy(policy); err != nil {
		return err.Error()
	}
	for _, subject := range policy.Subjects {
		if subject == "*" || strings.HasPrefix(subject, "user:") || strings.HasPrefix(subject, "group:") || strings.HasPrefix(subject, "role:") {
			continue
		}
		if strings.Contains(subject, ":") {
			return "invalid subject " + subject
		}
	}
	for _, channel := range policy.AllowedChannels {
		switch channel {
		case "session", "sftp", "tcpip":
		default:
			return "allowed_channels supports session, sftp and tcpip"
		}
	}
	return ""
}

func (s *Server) createPolicy(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	var policy model.AccessPolicy
	if !decodeJSON(w, r, &policy) {
		return
	}
	if msg := validatePolicy(&policy); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	policy.ID = identity.NewID()
	if policy.Priority == 0 {
		policy.Priority = 100
	}
	policy.Enabled = true
	if err := s.store.CreateAccessPolicy(r.Context(), &policy); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "policy.created", audit.ResultSuccess, actor,
		"access policy created", map[string]any{"policy_id": policy.ID, "name": policy.Name, "effect": policy.Effect})
	writeJSON(w, http.StatusCreated, policy)
}

func (s *Server) updatePolicy(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	policy, err := s.store.GetAccessPolicy(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	id, created := policy.ID, policy.CreatedAt
	if !decodeJSON(w, r, &policy) {
		return
	}
	policy.ID, policy.CreatedAt = id, created
	if msg := validatePolicy(policy); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateAccessPolicy(r.Context(), policy); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "policy.updated", audit.ResultSuccess, actor,
		"access policy updated", map[string]any{"policy_id": id, "name": policy.Name})
	writeJSON(w, http.StatusOK, policy)
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request, actor *model.User, _ model.Principal) {
	id := r.PathValue("id")
	if err := s.store.DeleteAccessPolicy(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.auditEvent(r, audit.CategoryAdmin, "policy.deleted", audit.ResultSuccess, actor,
		"access policy deleted", map[string]any{"policy_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
