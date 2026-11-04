package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsdenck/rscon/internal/model"
)

// auditLockKey serialises chain appends within PostgreSQL.
const auditLockKey int64 = 0x5253434f4e

const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

type canonicalAudit struct {
	EventID       string `json:"event_id"`
	Category      string `json:"category"`
	Action        string `json:"action"`
	ActorID       string `json:"actor_id"`
	ActorName     string `json:"actor_name"`
	Result        string `json:"result"`
	ResourceType  string `json:"resource_type"`
	ResourceID    string `json:"resource_id"`
	SourceIP      string `json:"source_ip"`
	CorrelationID string `json:"correlation_id"`
	Message       string `json:"message"`
	CreatedAt     string `json:"created_at"`
	Details       string `json:"details"`
}

func auditPayload(e model.AuditEvent, createdAt time.Time, detailsText string) ([]byte, error) {
	return json.Marshal(canonicalAudit{
		EventID:       e.EventID,
		Category:      e.Category,
		Action:        e.Action,
		ActorID:       e.ActorID,
		ActorName:     e.ActorName,
		Result:        e.Result,
		ResourceType:  e.ResourceType,
		ResourceID:    e.ResourceID,
		SourceIP:      e.SourceIP,
		CorrelationID: e.CorrelationID,
		Message:       e.Message,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339Nano),
		Details:       detailsText,
	})
}

func chainHash(prev string, payload []byte) string {
	sum := sha256.Sum256(append([]byte(prev+"\n"), payload...))
	return hex.EncodeToString(sum[:])
}

// AppendAudit writes an event to the hash chain. The write is serialised with
// a transaction-scoped advisory lock so concurrent gateways cannot fork it.
func (s *Store) AppendAudit(ctx context.Context, e *model.AuditEvent) error {
	if e.EventID == "" || e.Category == "" || e.Action == "" || e.Result == "" {
		return fmt.Errorf("%w: event id, category, action and result are required", ErrInvalid)
	}
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode audit details: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditLockKey); err != nil {
		return fmt.Errorf("lock audit chain: %w", err)
	}
	var prev string
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&prev)
	if err != nil && mapError(err) != ErrNotFound {
		return fmt.Errorf("read audit head: %w", err)
	}
	if prev == "" {
		prev = genesisHash
	}
	var detailsText string
	if err := tx.QueryRow(ctx, `SELECT ($1::jsonb)::text`, string(detailsJSON)).Scan(&detailsText); err != nil {
		return fmt.Errorf("normalise audit details: %w", err)
	}
	payload, err := auditPayload(*e, now, detailsText)
	if err != nil {
		return err
	}
	e.PrevHash = prev
	e.Hash = chainHash(prev, payload)
	e.CreatedAt = now
	if err := tx.QueryRow(ctx, `
		INSERT INTO audit_events (event_id, category, action, actor_id, actor_name, result,
			resource_type, resource_id, source_ip, correlation_id, message, details,
			prev_hash, hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id`,
		e.EventID, e.Category, e.Action, textOrNil(e.ActorID), e.ActorName, e.Result,
		e.ResourceType, e.ResourceID, e.SourceIP, e.CorrelationID, e.Message, detailsText,
		e.PrevHash, e.Hash, now).Scan(&e.ID); err != nil {
		return fmt.Errorf("append audit event: %w", mapError(err))
	}
	return tx.Commit(ctx)
}

// AuditFilter selects audit events.
type AuditFilter struct {
	Category string
	Action   string
	ActorID  string
	Result   string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

// ListAudit returns a filtered page and total count.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]model.AuditEvent, int, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE ($1='' OR category=$1) AND ($2='' OR action=$2) AND ($3='' OR actor_id=$3)
		  AND ($4='' OR result=$4) AND ($5::timestamptz IS NULL OR created_at >= $5)
		  AND ($6::timestamptz IS NULL OR created_at <= $6)`,
		f.Category, f.Action, f.ActorID, f.Result, f.From, f.To).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, category, action, coalesce(actor_id,''), actor_name, result,
			resource_type, resource_id, source_ip, correlation_id, message, details::text,
			prev_hash, hash, created_at
		FROM audit_events
		WHERE ($1='' OR category=$1) AND ($2='' OR action=$2) AND ($3='' OR actor_id=$3)
		  AND ($4='' OR result=$4) AND ($5::timestamptz IS NULL OR created_at >= $5)
		  AND ($6::timestamptz IS NULL OR created_at <= $6)
		ORDER BY id DESC LIMIT $7 OFFSET $8`,
		f.Category, f.Action, f.ActorID, f.Result, f.From, f.To, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var out []model.AuditEvent
	for rows.Next() {
		e, err := scanAudit(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *e)
	}
	return out, total, rows.Err()
}

func scanAudit(row interface{ Scan(...any) error }) (*model.AuditEvent, error) {
	var e model.AuditEvent
	var details string
	if err := row.Scan(&e.ID, &e.EventID, &e.Category, &e.Action, &e.ActorID, &e.ActorName,
		&e.Result, &e.ResourceType, &e.ResourceID, &e.SourceIP, &e.CorrelationID, &e.Message,
		&details, &e.PrevHash, &e.Hash, &e.CreatedAt); err != nil {
		return nil, mapError(err)
	}
	if err := json.Unmarshal([]byte(details), &e.Details); err != nil {
		return nil, fmt.Errorf("decode audit details: %w", err)
	}
	return &e, nil
}

// AuditVerification is the result of chain verification.
type AuditVerification struct {
	Events         int       `json:"events"`
	Valid          bool      `json:"valid"`
	FirstInvalidID int64     `json:"first_invalid_id,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	VerifiedAt     time.Time `json:"verified_at"`
}

// VerifyAuditChain recomputes the complete chain. It records the first
// invalid event without stopping the scan so operators can see the extent of
// damage.
func (s *Store) VerifyAuditChain(ctx context.Context) (AuditVerification, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, category, action, coalesce(actor_id,''), actor_name, result,
			resource_type, resource_id, source_ip, correlation_id, message, details::text,
			prev_hash, hash, created_at
		FROM audit_events ORDER BY id`)
	if err != nil {
		return AuditVerification{}, mapError(err)
	}
	defer rows.Close()
	prev := genesisHash
	var result AuditVerification
	for rows.Next() {
		var e model.AuditEvent
		var details string
		if err := rows.Scan(&e.ID, &e.EventID, &e.Category, &e.Action, &e.ActorID, &e.ActorName,
			&e.Result, &e.ResourceType, &e.ResourceID, &e.SourceIP, &e.CorrelationID, &e.Message,
			&details, &e.PrevHash, &e.Hash, &e.CreatedAt); err != nil {
			return result, mapError(err)
		}
		result.Events++
		payload, err := auditPayload(e, e.CreatedAt.UTC().Truncate(time.Microsecond), details)
		if err != nil {
			return result, err
		}
		if e.PrevHash != prev || chainHash(prev, payload) != e.Hash {
			if result.FirstInvalidID == 0 {
				result.FirstInvalidID = e.ID
				result.Reason = "hash chain mismatch"
			}
		}
		prev = e.Hash
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Valid = result.FirstInvalidID == 0
	result.VerifiedAt = time.Now().UTC()
	return result, nil
}

// TrimAuditOlderThan deletes audit rows older than cutoff, breaking the chain
// unless the caller archives and anchors the new head. It returns an error if
// the chain would be left in an unverifiable state without explicit consent.
func (s *Store) TrimAuditOlderThan(ctx context.Context, cutoff time.Time, allowBreak bool) (int64, error) {
	if !allowBreak {
		return 0, fmt.Errorf("%w: audit trimming breaks the local hash chain; archive and anchor the head first", ErrForbidden)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM audit_events WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}
