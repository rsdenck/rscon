package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rsdenck/rscon/internal/identity"
	"github.com/rsdenck/rscon/internal/model"
)

const policyColumns = `id, name, description, priority, effect, subjects, actions, server_ids,
	server_group_ids, environment_ids, accounts, allowed_channels, require_approval,
	approvals_required, require_mfa, max_duration_seconds, time_windows, enabled, created_at, updated_at`

func scanPolicy(row pgx.Row) (*model.AccessPolicy, error) {
	var p model.AccessPolicy
	var windows []byte
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Priority, &p.Effect, &p.Subjects, &p.Actions,
		&p.ServerIDs, &p.ServerGroupIDs, &p.EnvironmentIDs, &p.Accounts, &p.AllowedChannels,
		&p.RequireApproval, &p.ApprovalsRequired, &p.RequireMFA, &p.MaxDurationSeconds,
		&windows, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	if err := scanJSON(windows, &p.TimeWindows); err != nil {
		return nil, fmt.Errorf("decode policy time windows: %w", err)
	}
	if p.TimeWindows == nil {
		p.TimeWindows = []model.TimeWindow{}
	}
	return &p, nil
}

// normalisePolicy turns nil slices into empty slices so PostgreSQL array and
// jsonb columns receive a concrete value rather than SQL NULL.
func normalisePolicy(p *model.AccessPolicy) {
	if p.Subjects == nil {
		p.Subjects = []string{}
	}
	if p.Actions == nil {
		p.Actions = []string{}
	}
	if p.ServerIDs == nil {
		p.ServerIDs = []string{}
	}
	if p.ServerGroupIDs == nil {
		p.ServerGroupIDs = []string{}
	}
	if p.EnvironmentIDs == nil {
		p.EnvironmentIDs = []string{}
	}
	if p.Accounts == nil {
		p.Accounts = []string{}
	}
	if p.AllowedChannels == nil {
		p.AllowedChannels = []string{}
	}
	if p.TimeWindows == nil {
		p.TimeWindows = []model.TimeWindow{}
	}
}

// CreateAccessPolicy inserts a policy.
func (s *Store) CreateAccessPolicy(ctx context.Context, p *model.AccessPolicy) error {
	if p.ID == "" || p.Name == "" {
		return fmt.Errorf("%w: policy id and name are required", ErrInvalid)
	}
	if p.Priority == 0 {
		p.Priority = 100
	}
	normalisePolicy(p)
	windows, err := jsonValue(p.TimeWindows)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO access_policies (id, name, description, priority, effect, subjects, actions,
			server_ids, server_group_ids, environment_ids, accounts, allowed_channels,
			require_approval, approvals_required, require_mfa, max_duration_seconds, time_windows, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		p.ID, p.Name, p.Description, p.Priority, p.Effect, p.Subjects, p.Actions,
		p.ServerIDs, p.ServerGroupIDs, p.EnvironmentIDs, p.Accounts, p.AllowedChannels,
		p.RequireApproval, p.ApprovalsRequired, p.RequireMFA, p.MaxDurationSeconds, windows, p.Enabled)
	return mapError(err)
}

// ListAccessPolicies returns all policies ordered by priority.
func (s *Store) ListAccessPolicies(ctx context.Context) ([]model.AccessPolicy, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+policyColumns+` FROM access_policies ORDER BY priority, name`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.AccessPolicy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetAccessPolicy returns one policy.
func (s *Store) GetAccessPolicy(ctx context.Context, id string) (*model.AccessPolicy, error) {
	return scanPolicy(s.pool.QueryRow(ctx, `SELECT `+policyColumns+` FROM access_policies WHERE id=$1`, id))
}

// SetPolicyEnabled enables or disables a policy.
func (s *Store) SetPolicyEnabled(ctx context.Context, id string, enabled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE access_policies SET enabled=$2, updated_at=now() WHERE id=$1`, id, enabled)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const requestSelect = `SELECT r.id, r.requester_id, coalesce(u.username,''), r.server_id, coalesce(s.name,''),
	r.server_group_id, r.environment_id, r.account, r.reason, r.ticket, r.requested_seconds,
	r.status, r.starts_at, r.ends_at, r.approvals_required, r.approvals_count, r.created_at, r.decided_at
	FROM access_requests r
	LEFT JOIN users u ON u.id=r.requester_id
	LEFT JOIN servers s ON s.id=r.server_id`

func scanRequest(row pgx.Row) (*model.AccessRequest, error) {
	var r model.AccessRequest
	var serverID, groupID, envID *string
	err := row.Scan(&r.ID, &r.RequesterID, &r.RequesterName, &serverID, &r.ServerName,
		&groupID, &envID, &r.Account, &r.Reason, &r.Ticket, &r.RequestedSeconds,
		&r.Status, &r.StartsAt, &r.EndsAt, &r.ApprovalsRequired, &r.ApprovalsCount, &r.CreatedAt, &r.DecidedAt)
	if err != nil {
		return nil, mapError(err)
	}
	if serverID != nil {
		r.ServerID = *serverID
	}
	if groupID != nil {
		r.ServerGroupID = *groupID
	}
	if envID != nil {
		r.EnvironmentID = *envID
	}
	return &r, nil
}

// CreateAccessRequest inserts a pending request.
func (s *Store) CreateAccessRequest(ctx context.Context, r *model.AccessRequest) error {
	if r.ID == "" || r.RequesterID == "" || r.Account == "" || r.Reason == "" {
		return fmt.Errorf("%w: requester, account and reason are required", ErrInvalid)
	}
	if r.RequestedSeconds <= 0 {
		return fmt.Errorf("%w: requested duration must be positive", ErrInvalid)
	}
	if r.ApprovalsRequired < 1 {
		r.ApprovalsRequired = 1
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO access_requests (id, requester_id, server_id, server_group_id, environment_id,
			account, reason, ticket, requested_seconds, status, approvals_required)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		r.ID, r.RequesterID, textOrNil(r.ServerID), textOrNil(r.ServerGroupID), textOrNil(r.EnvironmentID),
		r.Account, r.Reason, r.Ticket, r.RequestedSeconds, model.RequestPending, r.ApprovalsRequired)
	return mapError(err)
}

// GetAccessRequest returns one request with display names.
func (s *Store) GetAccessRequest(ctx context.Context, id string) (*model.AccessRequest, error) {
	return scanRequest(s.pool.QueryRow(ctx, requestSelect+` WHERE r.id=$1`, id))
}

// ListAccessRequests returns a filtered page.
func (s *Store) ListAccessRequests(ctx context.Context, status, requesterID string, limit, offset int) ([]model.AccessRequest, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM access_requests
		WHERE ($1='' OR status=$1) AND ($2='' OR requester_id=$2)`, status, requesterID).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, requestSelect+`
		WHERE ($1='' OR r.status=$1) AND ($2='' OR r.requester_id=$2)
		ORDER BY r.created_at DESC LIMIT $3 OFFSET $4`, status, requesterID, limit, offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var out []model.AccessRequest
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

// DecideAccessRequest atomically records an approval decision. Self-approval
// is rejected unless allowSelf is explicitly enabled.
func (s *Store) DecideAccessRequest(ctx context.Context, requestID, approverID, decision, comment string, allowSelf bool) (*model.AccessRequest, error) {
	if decision != model.DecisionApprove && decision != model.DecisionDeny {
		return nil, fmt.Errorf("%w: decision must be approve or deny", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var requesterID, status string
	var requestedSeconds, approvalsRequired int
	if err := tx.QueryRow(ctx, `
		SELECT requester_id, status, requested_seconds, approvals_required
		FROM access_requests WHERE id=$1 FOR UPDATE`, requestID).
		Scan(&requesterID, &status, &requestedSeconds, &approvalsRequired); err != nil {
		return nil, mapError(err)
	}
	if status != model.RequestPending {
		return nil, fmt.Errorf("%w: request is not pending", ErrConflict)
	}
	if requesterID == approverID && !allowSelf {
		return nil, fmt.Errorf("%w: self-approval is not allowed", ErrForbidden)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO access_approvals (id, request_id, approver_id, decision, comment)
		VALUES ($1,$2,$3,$4,$5)`, identity.NewID(), requestID, approverID, decision, comment); err != nil {
		return nil, mapError(err)
	}

	if decision == model.DecisionDeny {
		if _, err := tx.Exec(ctx, `
			UPDATE access_requests SET status=$2, decided_at=now(), updated_at=now(),
				approvals_count=approvals_count+1 WHERE id=$1`, requestID, model.RequestDenied); err != nil {
			return nil, err
		}
	} else {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM access_approvals WHERE request_id=$1 AND decision='approve'`, requestID).Scan(&count); err != nil {
			return nil, err
		}
		if count >= approvalsRequired {
			if _, err := tx.Exec(ctx, `
				UPDATE access_requests SET status=$2, approvals_count=$3, decided_at=now(),
					starts_at=now(), ends_at=now() + make_interval(secs => $4), updated_at=now()
				WHERE id=$1`, requestID, model.RequestApproved, count, requestedSeconds); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE access_requests SET approvals_count=$2, updated_at=now() WHERE id=$1`, requestID, count); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetAccessRequest(ctx, requestID)
}

// ListAccessApprovals returns decisions for a request.
func (s *Store) ListAccessApprovals(ctx context.Context, requestID string) ([]model.AccessApproval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.request_id, a.approver_id, coalesce(u.username,''), a.decision, a.comment, a.created_at
		FROM access_approvals a LEFT JOIN users u ON u.id=a.approver_id
		WHERE a.request_id=$1 ORDER BY a.created_at`, requestID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.AccessApproval
	for rows.Next() {
		var a model.AccessApproval
		if err := rows.Scan(&a.ID, &a.RequestID, &a.ApproverID, &a.ApproverName, &a.Decision, &a.Comment, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// HasActiveGrant reports whether an approved, unexpired grant covers the
// target resource and account.
func (s *Store) HasActiveGrant(ctx context.Context, userID, serverID string, groupIDs []string, environmentID, account string, at time.Time) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM access_requests ar
			WHERE ar.requester_id=$1 AND ar.status='approved'
			  AND ar.starts_at <= $6 AND ar.ends_at > $6
			  AND (ar.account=$5 OR ar.account='')
			  AND (
				(ar.server_id IS NOT NULL AND ar.server_id=$2)
				OR (ar.server_group_id IS NOT NULL AND ar.server_group_id = ANY($3))
				OR (ar.environment_id IS NOT NULL AND ar.environment_id=$4)
			  )
		)`, userID, serverID, groupIDs, environmentID, account, at).Scan(&exists)
	return exists, mapError(err)
}

// ExpireAccessRequests marks approved grants past their end time as expired.
func (s *Store) ExpireAccessRequests(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE access_requests SET status='expired', updated_at=now()
		WHERE status='approved' AND ends_at IS NOT NULL AND ends_at <= now()`)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}
