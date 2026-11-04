package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rsdenck/rscon/internal/model"
)

const certificateSelect = `SELECT c.id, c.serial, c.user_id, coalesce(u.username,''), c.server_id,
	coalesce(s.name,''), c.account, c.key_id, c.public_key_fingerprint, c.cert_pem,
	c.ca_name, c.valid_after, c.valid_before, c.revoked_at, c.revoked_reason, c.created_at
	FROM ssh_certificates c
	LEFT JOIN users u ON u.id=c.user_id
	LEFT JOIN servers s ON s.id=c.server_id`

func scanCertificate(row pgx.Row) (*model.Certificate, error) {
	var c model.Certificate
	err := row.Scan(&c.ID, &c.Serial, &c.UserID, &c.Username, &c.ServerID, &c.ServerName,
		&c.Account, &c.KeyID, &c.PublicKeyFingerprint, &c.CertPEM, &c.CA,
		&c.ValidAfter, &c.ValidBefore, &c.RevokedAt, &c.RevokedReason, &c.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &c, nil
}

// CreateCertificate persists a signed public certificate.
func (s *Store) CreateCertificate(ctx context.Context, c *model.Certificate) error {
	if c.ID == "" || c.UserID == "" || c.ServerID == "" || c.CertPEM == "" {
		return fmt.Errorf("%w: certificate id, user, server and PEM are required", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ssh_certificates (id, serial, user_id, server_id, account, key_id,
			public_key_fingerprint, cert_pem, ca_name, valid_after, valid_before)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		c.ID, c.Serial, c.UserID, c.ServerID, c.Account, c.KeyID, c.PublicKeyFingerprint,
		c.CertPEM, c.CA, c.ValidAfter, c.ValidBefore)
	return mapError(err)
}

// GetCertificateByID returns certificate metadata.
func (s *Store) GetCertificateByID(ctx context.Context, id string) (*model.Certificate, error) {
	return scanCertificate(s.pool.QueryRow(ctx, certificateSelect+` WHERE c.id=$1`, id))
}

// GetCertificateBySerial returns the certificate record for a serial number.
func (s *Store) GetCertificateBySerial(ctx context.Context, serial uint64) (*model.Certificate, error) {
	return scanCertificate(s.pool.QueryRow(ctx, certificateSelect+` WHERE c.serial=$1`, int64(serial)))
}

// ListCertificates returns a filtered page of certificates.
func (s *Store) ListCertificates(ctx context.Context, userID string, limit, offset int) ([]model.Certificate, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ssh_certificates WHERE ($1='' OR user_id=$1)`, userID).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, certificateSelect+`
		WHERE ($1='' OR c.user_id=$1) ORDER BY c.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var out []model.Certificate
	for rows.Next() {
		c, err := scanCertificate(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// RevokeCertificate marks a certificate revoked.
func (s *Store) RevokeCertificate(ctx context.Context, id, reason string) (*model.Certificate, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE ssh_certificates SET revoked_at=now(), revoked_reason=$2
		WHERE id=$1 AND revoked_at IS NULL`, id, reason)
	if err != nil {
		return nil, mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("%w: certificate not found or already revoked", ErrNotFound)
	}
	return s.GetCertificateByID(ctx, id)
}

// RevokeUserCertificates revokes every active certificate for a user.
func (s *Store) RevokeUserCertificates(ctx context.Context, userID, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE ssh_certificates SET revoked_at=now(), revoked_reason=$2
		WHERE user_id=$1 AND revoked_at IS NULL`, userID, reason)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// ListRevokedSerialNumbers returns revoked certificate serials (for KRL
// export tooling and diagnostics).
func (s *Store) ListRevokedSerialNumbers(ctx context.Context, since time.Time) ([]uint64, error) {
	rows, err := s.pool.Query(ctx, `SELECT serial FROM ssh_certificates
		WHERE revoked_at IS NOT NULL AND revoked_at >= $1 ORDER BY serial`, since)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []uint64
	for rows.Next() {
		var serial int64
		if err := rows.Scan(&serial); err != nil {
			return nil, err
		}
		out = append(out, uint64(serial))
	}
	return out, rows.Err()
}
