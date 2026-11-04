// Package store contains the PostgreSQL repositories. It is the only package
// in RSCON that executes SQL.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rsdenck/rscon/internal/database"
	"github.com/rsdenck/rscon/internal/model"
)

// Sentinel errors returned by repositories.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrInvalid   = errors.New("invalid argument")
	ErrForbidden = errors.New("forbidden")
)

// Store provides PostgreSQL persistence.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a store backed by db.
func New(db *database.DB) *Store { return &Store{pool: db.Pool} }

// NewFromPool is used by integration tests.
func NewFromPool(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the pool for health checks and migrations.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: referenced record does not exist (%s)", ErrInvalid, pgErr.ConstraintName)
		case "23514":
			return fmt.Errorf("%w: %s", ErrInvalid, pgErr.ConstraintName)
		}
	}
	return err
}

func textOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func jsonValue(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

func scanJSON(data []byte, out any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// CountUsers returns the number of non-deleted users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&n)
	return n, mapError(err)
}

const userColumns = `id, username, coalesce(email,''), display_name, coalesce(password_hash,''),
	status, mfa_enrolled, coalesce(idp_id,''), last_login_at, failed_logins, locked_until,
	created_at, updated_at, deleted_at`

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash,
		&u.Status, &u.MFAEnrolled, &u.IdentityProvider, &u.LastLoginAt, &u.FailedLogins,
		&u.LockedUntil, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &u, nil
}

// CreateUser inserts a user.
func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	if u.ID == "" || u.Username == "" {
		return fmt.Errorf("%w: user id and username are required", ErrInvalid)
	}
	if u.Status == "" {
		u.Status = model.StatusActive
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, username, email, display_name, password_hash, status, mfa_enrolled, idp_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		u.ID, u.Username, textOrNil(u.Email), u.DisplayName, textOrNil(u.PasswordHash),
		u.Status, u.MFAEnrolled, textOrNil(u.IdentityProvider))
	if err != nil {
		return fmt.Errorf("create user: %w", mapError(err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("create user: no row inserted")
	}
	return nil
}

// GetUserByID returns a user by identifier.
func (s *Store) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 AND deleted_at IS NULL`, id))
}

// GetUserByUsername performs a case-insensitive lookup.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(username)=lower($1) AND deleted_at IS NULL`, username))
}

// GetUserByEmail performs a case-insensitive lookup.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email)=lower($1) AND deleted_at IS NULL`, email))
}

// ListUsers returns a page of users and the total count.
func (s *Store) ListUsers(ctx context.Context, search string, limit, offset int) ([]model.User, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM users
		WHERE deleted_at IS NULL AND ($1='%%' OR lower(username) LIKE $1 OR lower(email) LIKE $1 OR lower(display_name) LIKE $1)`,
		pattern).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+userColumns+` FROM users
		WHERE deleted_at IS NULL AND ($1='%%' OR lower(username) LIKE $1 OR lower(email) LIKE $1 OR lower(display_name) LIKE $1)
		ORDER BY username LIMIT $2 OFFSET $3`, pattern, limit, offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, *u)
	}
	return users, total, rows.Err()
}

// SetUserStatus updates account status. Disabling a user revokes tokens.
func (s *Store) SetUserStatus(ctx context.Context, id, status string, revokeTokens bool) error {
	if status != model.StatusActive && status != model.StatusDisabled && status != model.StatusLocked {
		return fmt.Errorf("%w: invalid status", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE users SET status=$2, updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, id, status)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if status != model.StatusActive && revokeTokens {
		if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RecordLoginSuccess clears failures and locks.
func (s *Store) RecordLoginSuccess(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at=now(), failed_logins=0, locked_until=NULL, updated_at=now() WHERE id=$1`, id)
	return mapError(err)
}

// RecordLoginFailure increments the failure counter and locks the account
// once threshold is reached.
func (s *Store) RecordLoginFailure(ctx context.Context, id string, threshold int, lockFor time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET
			failed_logins = failed_logins + 1,
			locked_until = CASE WHEN failed_logins + 1 >= $2 THEN now() + $3::interval ELSE locked_until END,
			updated_at = now()
		WHERE id=$1`, id, threshold, lockFor.String())
	return mapError(err)
}

// SetPasswordHash updates the Argon2id hash.
func (s *Store) SetPasswordHash(ctx context.Context, id, hash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, id, hash)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListUserGroupIDs returns the groups a user belongs to.
func (s *Store) ListUserGroupIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT group_id FROM user_groups WHERE user_id=$1 ORDER BY group_id`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListBindingsForSubjects returns role bindings for a user and their groups.
func (s *Store) ListBindingsForSubjects(ctx context.Context, userID string, groupIDs []string) ([]model.RoleBinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.role_id, r.name, b.subject_type, b.subject_id, b.created_at
		FROM role_bindings b JOIN roles r ON r.id=b.role_id
		WHERE (b.subject_type='user' AND b.subject_id=$1)
		   OR (b.subject_type='group' AND b.subject_id = ANY($2))
		ORDER BY r.name`, userID, groupIDs)
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

// ListPermissionsByRoleIDs returns the union of permissions for roles.
func (s *Store) ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT action FROM role_permissions WHERE role_id = ANY($1)`, roleIDs)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, err
		}
		out = append(out, action)
	}
	return out, rows.Err()
}

// GetUser is an alias for GetUserByID used by the policy engine.
func (s *Store) GetUser(ctx context.Context, id string) (*model.User, error) {
	return s.GetUserByID(ctx, id)
}
