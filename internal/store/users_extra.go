package store

import (
	"context"
	"fmt"

	"github.com/rsdenck/rscon/internal/model"
)

// UpdateUserProfile changes display fields only.
func (s *Store) UpdateUserProfile(ctx context.Context, id, email, displayName string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET email=$2, display_name=$3, updated_at=now()
		WHERE id=$1 AND deleted_at IS NULL`, id, textOrNil(email), displayName)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser soft-deletes an account and revokes all of its tokens.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE users SET deleted_at=now(), status='disabled', updated_at=now()
		WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteUserPublicKey removes a registered public key scoped to its owner.
func (s *Store) DeleteUserPublicKey(ctx context.Context, userID, keyID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM user_public_keys WHERE id=$1 AND user_id=$2`, keyID, userID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserMFA changes the MFA-enrolled marker. It does not enrol a factor:
// authenticators are not implemented in v0.1.
func (s *Store) UpdateUserMFA(ctx context.Context, id string, enrolled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET mfa_enrolled=$2, updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, id, enrolled)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetRoleByID returns a role by identifier.
func (s *Store) GetRoleByID(ctx context.Context, id string) (*model.Role, error) {
	var r model.Role
	err := s.pool.QueryRow(ctx, `SELECT id, name, COALESCE(description,''), system, created_at FROM roles WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.System, &r.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &r, nil
}

// EnsureSubjectExists validates a role binding subject.
func (s *Store) EnsureSubjectExists(ctx context.Context, subjectType, subjectID string) error {
	var exists bool
	var query string
	switch subjectType {
	case "user":
		query = `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL)`
	case "group":
		query = `SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1)`
	default:
		return fmt.Errorf("%w: subject type must be user or group", ErrInvalid)
	}
	if err := s.pool.QueryRow(ctx, query, subjectID).Scan(&exists); err != nil {
		return mapError(err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
