package store

import (
	"context"
	"fmt"
	"time"

	"github.com/rsdenck/rscon/internal/model"
)

// UpdateAccessPolicy replaces the mutable fields of a policy.
func (s *Store) UpdateAccessPolicy(ctx context.Context, p *model.AccessPolicy) error {
	if p.ID == "" || p.Name == "" {
		return fmt.Errorf("%w: policy id and name are required", ErrInvalid)
	}
	normalisePolicy(p)
	windows, err := jsonValue(p.TimeWindows)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE access_policies SET name=$2, description=$3, priority=$4, effect=$5, subjects=$6,
			actions=$7, server_ids=$8, server_group_ids=$9, environment_ids=$10, accounts=$11,
			allowed_channels=$12, require_approval=$13, approvals_required=$14, require_mfa=$15,
			max_duration_seconds=$16, time_windows=$17, enabled=$18, updated_at=now()
		WHERE id=$1`,
		p.ID, p.Name, p.Description, p.Priority, p.Effect, p.Subjects, p.Actions,
		p.ServerIDs, p.ServerGroupIDs, p.EnvironmentIDs, p.Accounts, p.AllowedChannels,
		p.RequireApproval, p.ApprovalsRequired, p.RequireMFA, p.MaxDurationSeconds, windows, p.Enabled)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAccessPolicy removes a policy.
func (s *Store) DeleteAccessPolicy(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM access_policies WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateGroup changes a group's display fields.
func (s *Store) UpdateGroup(ctx context.Context, g *model.Group) error {
	tag, err := s.pool.Exec(ctx, `UPDATE groups SET name=$2, description=$3 WHERE id=$1`, g.ID, g.Name, g.Description)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteGroup removes an empty or non-empty group; memberships cascade.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteServerGroup removes a server group; memberships cascade.
func (s *Store) DeleteServerGroup(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM server_groups WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// BindRoleToSubject is an idempotent variant of BindRole.
func (s *Store) BindRoleToSubject(ctx context.Context, binding *model.RoleBinding) error {
	if binding.ID == "" || binding.RoleID == "" || binding.SubjectType == "" || binding.SubjectID == "" {
		return fmt.Errorf("%w: binding id, role, subject type and subject are required", ErrInvalid)
	}
	if binding.SubjectType != "user" && binding.SubjectType != "group" {
		return fmt.Errorf("%w: subject type must be user or group", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO role_bindings (id, role_id, subject_type, subject_id)
		VALUES ($1,$2,$3,$4) ON CONFLICT (role_id, subject_type, subject_id) DO NOTHING`,
		binding.ID, binding.RoleID, binding.SubjectType, binding.SubjectID)
	return mapError(err)
}

// DeleteRoleBinding removes a role binding.
func (s *Store) DeleteRoleBinding(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM role_bindings WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRoleBindings returns all role bindings.
func (s *Store) ListRoleBindings(ctx context.Context) ([]model.RoleBinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.role_id, r.name, b.subject_type, b.subject_id, b.created_at
		FROM role_bindings b JOIN roles r ON r.id=b.role_id
		ORDER BY r.name, b.subject_type, b.subject_id`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.RoleBinding
	for rows.Next() {
		var b model.RoleBinding
		if err := rows.Scan(&b.ID, &b.RoleID, &b.RoleName, &b.SubjectType, &b.SubjectID, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CancelAccessRequest lets a requester withdraw a pending request.
func (s *Store) CancelAccessRequest(ctx context.Context, id, requesterID string) (*model.AccessRequest, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE access_requests SET status='canceled', decided_at=now(), updated_at=now()
		WHERE id=$1 AND requester_id=$2 AND status='pending'`, id, requesterID)
	if err != nil {
		return nil, mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: pending request not found for requester", ErrNotFound)
	}
	return s.GetAccessRequest(ctx, id)
}

// AutoApproveAccessRequest grants a request that policy does not require an
// approval for. It exists so clients receive a uniform access-request record
// while still allowing immediate access.
func (s *Store) AutoApproveAccessRequest(ctx context.Context, id string, seconds int) (*model.AccessRequest, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE access_requests SET status='approved', approvals_required=0, approvals_count=0,
			starts_at=now(), ends_at=now() + make_interval(secs => $2), decided_at=now(), updated_at=now()
		WHERE id=$1 AND status='pending'`, id, seconds)
	if err != nil {
		return nil, mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetAccessRequest(ctx, id)
}

// ListTokensForUser returns token metadata (never digests) for a user.
func (s *Store) ListTokensForUser(ctx context.Context, userID string) ([]model.APIToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, kind, ''::text, expires_at, revoked_at, last_used_at, created_at
		FROM api_tokens WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.APIToken
	for rows.Next() {
		var t model.APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Kind, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.LastUsed, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteExpiredTokens removes expired tokens and returns the number deleted.
func (s *Store) DeleteExpiredTokens(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_tokens WHERE expires_at < $1`, olderThan)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// DashboardCounts is an aggregate used by the web console.
type DashboardCounts struct {
	Users          int `json:"users"`
	Servers        int `json:"servers"`
	ActiveSessions int `json:"active_sessions"`
	PendingAccess  int `json:"pending_access_requests"`
	Policies       int `json:"policies"`
	Unverified     int `json:"unverified_audit_events"`
}

// CountDashboard collects low-cost counters in one query.
func (s *Store) CountDashboard(ctx context.Context) (DashboardCounts, error) {
	var d DashboardCounts
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users WHERE deleted_at IS NULL),
			(SELECT count(*) FROM servers WHERE deleted_at IS NULL),
			(SELECT count(*) FROM sessions WHERE status='active'),
			(SELECT count(*) FROM access_requests WHERE status='pending'),
			(SELECT count(*) FROM access_policies)`).
		Scan(&d.Users, &d.Servers, &d.ActiveSessions, &d.PendingAccess, &d.Policies)
	return d, mapError(err)
}
