package store

import (
	"context"
	"fmt"
	"time"

	"github.com/rsdenck/rscon/internal/model"
)

// CreateToken stores an opaque token digest.
func (s *Store) CreateToken(ctx context.Context, t *model.APIToken) error {
	if t.ID == "" || t.UserID == "" || t.TokenHash == "" || t.Kind == "" {
		return fmt.Errorf("%w: token id, user, hash and kind are required", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO api_tokens (id, user_id, kind, token_hash, expires_at)
		VALUES ($1,$2,$3,$4,$5)`, t.ID, t.UserID, t.Kind, t.TokenHash, t.ExpiresAt)
	return mapError(err)
}

// GetTokenByHash returns an unexpired, unrevoked token by digest.
func (s *Store) GetTokenByHash(ctx context.Context, hash string) (*model.APIToken, error) {
	var t model.APIToken
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, kind, token_hash, expires_at, revoked_at, last_used_at, created_at
		FROM api_tokens WHERE token_hash=$1`, hash).
		Scan(&t.ID, &t.UserID, &t.Kind, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.LastUsed, &t.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	if t.RevokedAt != nil || time.Now().After(t.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &t, nil
}

// RevokeToken revokes one token belonging to a user.
func (s *Store) RevokeToken(ctx context.Context, tokenID, userID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, tokenID, userID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllTokensForUser invalidates every token for a user.
func (s *Store) RevokeAllTokensForUser(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return mapError(err)
}

// TouchToken records last use; callers throttle writes.
func (s *Store) TouchToken(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at=now() WHERE id=$1`, id)
	return mapError(err)
}

// CreateGroup inserts a group.
func (s *Store) CreateGroup(ctx context.Context, g *model.Group) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO groups (id, name, description) VALUES ($1,$2,$3)`, g.ID, g.Name, g.Description)
	return mapError(err)
}

// ListGroups returns groups with member counts, newest first.
func (s *Store) ListGroups(ctx context.Context) ([]model.Group, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, description, created_at FROM groups ORDER BY name`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.Group
	for rows.Next() {
		var g model.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// AddUserToGroup creates a membership (idempotent).
func (s *Store) AddUserToGroup(ctx context.Context, userID, groupID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO user_groups (user_id, group_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, groupID)
	return mapError(err)
}

// RemoveUserFromGroup removes a membership.
func (s *Store) RemoveUserFromGroup(ctx context.Context, userID, groupID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND group_id=$2`, userID, groupID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGroupMemberIDs returns user IDs in a group.
func (s *Store) ListGroupMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM user_groups WHERE group_id=$1 ORDER BY user_id`, groupID)
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

// ListRoles returns roles.
func (s *Store) ListRoles(ctx context.Context) ([]model.Role, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, description, system, created_at FROM roles ORDER BY name`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.Role
	for rows.Next() {
		var r model.Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.System, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRoleByName returns a role by unique name.
func (s *Store) GetRoleByName(ctx context.Context, name string) (*model.Role, error) {
	var r model.Role
	err := s.pool.QueryRow(ctx, `SELECT id, name, description, system, created_at FROM roles WHERE name=$1`, name).
		Scan(&r.ID, &r.Name, &r.Description, &r.System, &r.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &r, nil
}

// ListPermissions returns the permission catalogue.
func (s *Store) ListPermissions(ctx context.Context) ([]model.Permission, error) {
	rows, err := s.pool.Query(ctx, `SELECT action, description FROM permissions ORDER BY action`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.Permission
	for rows.Next() {
		var p model.Permission
		if err := rows.Scan(&p.Action, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// BindRole grants a role to a user or group (idempotent).
func (s *Store) BindRole(ctx context.Context, binding *model.RoleBinding) error {
	if binding.SubjectType != "user" && binding.SubjectType != "group" {
		return fmt.Errorf("%w: subject_type must be user or group", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO role_bindings (id, role_id, subject_type, subject_id)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (role_id, subject_type, subject_id) DO NOTHING`,
		binding.ID, binding.RoleID, binding.SubjectType, binding.SubjectID)
	return mapError(err)
}

// AddUserPublicKey registers a public key identity.
func (s *Store) AddUserPublicKey(ctx context.Context, k *model.UserPublicKey) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_public_keys (id, user_id, name, public_key, fingerprint)
		VALUES ($1,$2,$3,$4,$5)`, k.ID, k.UserID, k.Name, k.PublicKey, k.Fingerprint)
	return mapError(err)
}

// ListUserPublicKeys returns a user's registered keys.
func (s *Store) ListUserPublicKeys(ctx context.Context, userID string) ([]model.UserPublicKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, public_key, fingerprint, created_at, last_used_at
		FROM user_public_keys WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.UserPublicKey
	for rows.Next() {
		var k model.UserPublicKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// FindUserByKeyFingerprint resolves a registered public key to its user.
func (s *Store) FindUserByKeyFingerprint(ctx context.Context, fingerprint string) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT `+prefixedUserColumns("u")+`
		FROM user_public_keys k JOIN users u ON u.id=k.user_id
		WHERE k.fingerprint=$1 AND u.deleted_at IS NULL`, fingerprint))
}

// TouchPublicKey records key use.
func (s *Store) TouchPublicKey(ctx context.Context, fingerprint string) error {
	_, err := s.pool.Exec(ctx, `UPDATE user_public_keys SET last_used_at=now() WHERE fingerprint=$1`, fingerprint)
	return mapError(err)
}

func prefixedUserColumns(alias string) string {
	return fmt.Sprintf(`%[1]s.id, %[1]s.username, coalesce(%[1]s.email,''), %[1]s.display_name,
		coalesce(%[1]s.password_hash,''), %[1]s.status, %[1]s.mfa_enrolled, coalesce(%[1]s.idp_id,''),
		%[1]s.last_login_at, %[1]s.failed_logins, %[1]s.locked_until, %[1]s.created_at, %[1]s.updated_at, %[1]s.deleted_at`, alias)
}

// GetUserByKeyFingerprint is an alias for FindUserByKeyFingerprint used by the
// SSH gateway.
func (s *Store) GetUserByKeyFingerprint(ctx context.Context, fingerprint string) (*model.User, error) {
	return s.FindUserByKeyFingerprint(ctx, fingerprint)
}
