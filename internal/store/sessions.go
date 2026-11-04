package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rsdenck/rscon/internal/model"
)

const sessionSelect = `SELECT s.id, s.user_id, coalesce(u.username,''), s.server_id, coalesce(sv.name,''),
	s.account, s.source_ip, s.auth_method, coalesce(s.certificate_id,''), s.gateway, s.status,
	s.command, s.started_at, s.ended_at, s.duration_seconds, s.termination_reason,
	s.exit_status, coalesce(s.recording_id,''), s.terminate_requested
	FROM sessions s
	LEFT JOIN users u ON u.id=s.user_id
	LEFT JOIN servers sv ON sv.id=s.server_id`

func scanSession(row pgx.Row) (*model.Session, error) {
	var s model.Session
	var certID string
	err := row.Scan(&s.ID, &s.UserID, &s.Username, &s.ServerID, &s.ServerName, &s.Account,
		&s.SourceIP, &s.AuthMethod, &certID, &s.Gateway, &s.Status, &s.Command,
		&s.StartedAt, &s.EndedAt, &s.DurationSeconds, &s.TerminationReason,
		&s.ExitStatus, &s.RecordingID, &s.TerminateRequested)
	if err != nil {
		return nil, mapError(err)
	}
	s.CertificateID = certID
	return &s, nil
}

// CreateSession registers an active session before proxying begins.
func (s *Store) CreateSession(ctx context.Context, sess *model.Session) error {
	if sess.ID == "" || sess.UserID == "" || sess.ServerID == "" || sess.Account == "" {
		return fmt.Errorf("%w: session id, user, server and account are required", ErrInvalid)
	}
	if sess.StartedAt.IsZero() {
		sess.StartedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, server_id, account, source_ip, auth_method,
			certificate_id, gateway, status, command, started_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		sess.ID, sess.UserID, sess.ServerID, sess.Account, sess.SourceIP, sess.AuthMethod,
		textOrNil(sess.CertificateID), sess.Gateway, model.SessionActive, sess.Command, sess.StartedAt)
	return mapError(err)
}

// GetSession returns session metadata.
func (s *Store) GetSession(ctx context.Context, id string) (*model.Session, error) {
	return scanSession(s.pool.QueryRow(ctx, sessionSelect+` WHERE s.id=$1`, id))
}

// ListSessions returns a filtered page and total count.
func (s *Store) ListSessions(ctx context.Context, userID, serverID, status string, limit, offset int) ([]model.Session, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sessions
		WHERE ($1='' OR user_id=$1) AND ($2='' OR server_id=$2) AND ($3='' OR status=$3)`,
		userID, serverID, status).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, sessionSelect+`
		WHERE ($1='' OR s.user_id=$1) AND ($2='' OR s.server_id=$2) AND ($3='' OR s.status=$3)
		ORDER BY s.started_at DESC LIMIT $4 OFFSET $5`, userID, serverID, status, limit, offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *sess)
	}
	return out, total, rows.Err()
}

// CloseSession finalises a session row.
func (s *Store) CloseSession(ctx context.Context, id, status, reason string, exitStatus *int, recordingID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET
			status=$2,
			ended_at=now(),
			duration_seconds=GREATEST(0, EXTRACT(EPOCH FROM (now()-started_at))::int),
			termination_reason=$3,
			exit_status=$4,
			recording_id=COALESCE(NULLIF($5,''), recording_id),
			updated_at=now()
		WHERE id=$1`, id, status, reason, exitStatus, recordingID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSessionRecording attaches a recording to a session.
func (s *Store) SetSessionRecording(ctx context.Context, sessionID, recordingID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET recording_id=$2, updated_at=now() WHERE id=$1`, sessionID, recordingID)
	return mapError(err)
}

// CountActiveSessionsForUser returns the number of live sessions.
func (s *Store) CountActiveSessionsForUser(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND status='active'`, userID).Scan(&n)
	return n, mapError(err)
}

// ActiveSessionCount returns the global number of live sessions.
func (s *Store) ActiveSessionCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE status='active'`).Scan(&n)
	return n, mapError(err)
}

// MarkStaleSessions marks sessions owned by a gateway as interrupted. It is
// used at startup to reconcile sessions left behind by an unclean shutdown.
func (s *Store) MarkStaleSessions(ctx context.Context, gateway string, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET status='interrupted', ended_at=now(),
			duration_seconds=GREATEST(0, EXTRACT(EPOCH FROM (now()-started_at))::int),
			termination_reason='gateway restarted or failed; session outcome unknown',
			updated_at=now()
		WHERE gateway=$1 AND status='active' AND started_at < now() - $2::interval`,
		gateway, olderThan.String())
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// RequestTermination flags an active session for cancellation.
func (s *Store) RequestTermination(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET terminate_requested=true, updated_at=now()
		WHERE id=$1 AND status='active'`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: active session not found", ErrNotFound)
	}
	return nil
}

// ClaimTerminationRequests returns sessions this gateway must cancel.
func (s *Store) ClaimTerminationRequests(ctx context.Context, gateway string) ([]model.Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, server_id, account FROM sessions
		WHERE gateway=$1 AND status='active' AND terminate_requested`, gateway)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		var sess model.Session
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.ServerID, &sess.Account); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// CreateRecording persists recording metadata.
func (s *Store) CreateRecording(ctx context.Context, r *model.Recording) error {
	if r.ID == "" || r.SessionID == "" || r.ObjectKey == "" {
		return fmt.Errorf("%w: recording id, session and object key are required", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO session_recordings (id, session_id, backend, object_key, format, size_bytes, sha256)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, r.ID, r.SessionID, r.Backend, r.ObjectKey, r.Format, r.SizeBytes, r.SHA256)
	return mapError(err)
}

// GetRecording returns one recording by ID.
func (s *Store) GetRecording(ctx context.Context, id string) (*model.Recording, error) {
	var r model.Recording
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, backend, object_key, format, size_bytes, sha256, created_at
		FROM session_recordings WHERE id=$1`, id).
		Scan(&r.ID, &r.SessionID, &r.Backend, &r.ObjectKey, &r.Format, &r.SizeBytes, &r.SHA256, &r.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &r, nil
}

// ListRecordings returns recordings, newest first.
func (s *Store) ListRecordings(ctx context.Context, sessionID string, limit, offset int) ([]model.Recording, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM session_recordings WHERE ($1='' OR session_id=$1)`, sessionID).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, backend, object_key, format, size_bytes, sha256, created_at
		FROM session_recordings WHERE ($1='' OR session_id=$1)
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, sessionID, limit, offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var out []model.Recording
	for rows.Next() {
		var r model.Recording
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Backend, &r.ObjectKey, &r.Format, &r.SizeBytes, &r.SHA256, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
