package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/rsdenck/rscon/internal/model"
)

const serverColumns = `s.id, s.name, s.hostname, s.address, s.port, s.environment_id,
	coalesce(e.name,''), s.os_family, s.location, s.tags, s.default_account,
	s.host_key_fingerprint, s.enabled, s.created_at, s.updated_at, s.deleted_at`

func scanServer(row pgx.Row) (*model.Server, error) {
	var srv model.Server
	err := row.Scan(&srv.ID, &srv.Name, &srv.Hostname, &srv.Address, &srv.Port, &srv.EnvironmentID,
		&srv.Environment, &srv.OSFamily, &srv.Location, &srv.Tags, &srv.DefaultAccount,
		&srv.HostKeyFingerprint, &srv.Enabled, &srv.CreatedAt, &srv.UpdatedAt, &srv.DeletedAt)
	if err != nil {
		return nil, mapError(err)
	}
	if srv.Tags == nil {
		srv.Tags = []string{}
	}
	return &srv, nil
}

// CreateServer inserts a server and its group memberships.
func (s *Store) CreateServer(ctx context.Context, srv *model.Server, groupIDs []string) error {
	if srv.ID == "" || srv.Name == "" || srv.Address == "" {
		return fmt.Errorf("%w: server id, name and address are required", ErrInvalid)
	}
	if srv.Port == 0 {
		srv.Port = 22
	}
	if srv.Tags == nil {
		srv.Tags = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO servers (id, name, hostname, address, port, environment_id, os_family,
			location, tags, default_account, host_key_fingerprint, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		srv.ID, srv.Name, srv.Hostname, srv.Address, srv.Port, srv.EnvironmentID,
		srv.OSFamily, srv.Location, srv.Tags, srv.DefaultAccount, srv.HostKeyFingerprint, srv.Enabled)
	if err != nil {
		return fmt.Errorf("create server: %w", mapError(err))
	}
	for _, gid := range groupIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO server_group_members (server_id, group_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, srv.ID, gid); err != nil {
			return fmt.Errorf("add server group: %w", mapError(err))
		}
	}
	return tx.Commit(ctx)
}

// GetServerByID returns a server with its environment name.
func (s *Store) GetServerByID(ctx context.Context, id string) (*model.Server, error) {
	return scanServer(s.pool.QueryRow(ctx, `SELECT `+serverColumns+` FROM servers s LEFT JOIN environments e ON e.id=s.environment_id WHERE s.id=$1 AND s.deleted_at IS NULL`, id))
}

// GetServerByName performs a case-insensitive lookup.
func (s *Store) GetServerByName(ctx context.Context, name string) (*model.Server, error) {
	return scanServer(s.pool.QueryRow(ctx, `SELECT `+serverColumns+` FROM servers s LEFT JOIN environments e ON e.id=s.environment_id WHERE lower(s.name)=lower($1) AND s.deleted_at IS NULL`, name))
}

// GetServerByAddress resolves a registered destination by address or hostname.
func (s *Store) GetServerByAddress(ctx context.Context, address string) (*model.Server, error) {
	return scanServer(s.pool.QueryRow(ctx, `SELECT `+serverColumns+` FROM servers s LEFT JOIN environments e ON e.id=s.environment_id
		WHERE (lower(s.address)=lower($1) OR lower(s.hostname)=lower($1)) AND s.deleted_at IS NULL`, address))
}

// ListServers returns a filtered page and total count.
func (s *Store) ListServers(ctx context.Context, environmentID, groupID, search string, enabledOnly bool, limit, offset int) ([]model.Server, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
	const where = `s.deleted_at IS NULL
		AND ($1='' OR s.environment_id=$1)
		AND ($2='' OR EXISTS (SELECT 1 FROM server_group_members m WHERE m.server_id=s.id AND m.group_id=$2))
		AND ($3='%%' OR lower(s.name) LIKE $3 OR lower(s.hostname) LIKE $3 OR s.address LIKE $3)
		AND (NOT $4 OR s.enabled)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM servers s WHERE `+where, environmentID, groupID, pattern, enabledOnly).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+serverColumns+`
		FROM servers s LEFT JOIN environments e ON e.id=s.environment_id
		WHERE `+where+` ORDER BY s.name LIMIT $5 OFFSET $6`, environmentID, groupID, pattern, enabledOnly, limit, offset)
	if err != nil {
		return nil, 0, mapError(err)
	}
	defer rows.Close()
	var out []model.Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *srv)
	}
	return out, total, rows.Err()
}

// UpdateServer applies mutable fields.
func (s *Store) UpdateServer(ctx context.Context, srv *model.Server) error {
	if srv.Port == 0 {
		srv.Port = 22
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE servers SET name=$2, hostname=$3, address=$4, port=$5, environment_id=$6,
			os_family=$7, location=$8, tags=$9, default_account=$10, host_key_fingerprint=$11,
			enabled=$12, updated_at=now()
		WHERE id=$1 AND deleted_at IS NULL`,
		srv.ID, srv.Name, srv.Hostname, srv.Address, srv.Port, srv.EnvironmentID,
		srv.OSFamily, srv.Location, srv.Tags, srv.DefaultAccount, srv.HostKeyFingerprint, srv.Enabled)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteServer soft-deletes a server.
func (s *Store) DeleteServer(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE servers SET deleted_at=now(), enabled=false, updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetServerGroups replaces group memberships.
func (s *Store) SetServerGroups(ctx context.Context, serverID string, groupIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM server_group_members WHERE server_id=$1`, serverID); err != nil {
		return err
	}
	for _, gid := range groupIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO server_group_members (server_id, group_id) VALUES ($1,$2)`, serverID, gid); err != nil {
			return mapError(err)
		}
	}
	return tx.Commit(ctx)
}

// ListServerGroupIDs returns group IDs for a server.
func (s *Store) ListServerGroupIDs(ctx context.Context, serverID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT group_id FROM server_group_members WHERE server_id=$1 ORDER BY group_id`, serverID)
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

// ListEnvironments returns all environments.
func (s *Store) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, description, critical, created_at FROM environments ORDER BY name`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.Environment
	for rows.Next() {
		var e model.Environment
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.Critical, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateEnvironment inserts an environment.
func (s *Store) CreateEnvironment(ctx context.Context, e *model.Environment) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO environments (id, name, description, critical) VALUES ($1,$2,$3,$4)`, e.ID, e.Name, e.Description, e.Critical)
	return mapError(err)
}

// ListServerGroups returns all server groups.
func (s *Store) ListServerGroups(ctx context.Context) ([]model.ServerGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, description, created_at FROM server_groups ORDER BY name`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []model.ServerGroup
	for rows.Next() {
		var g model.ServerGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CreateServerGroup inserts a server group.
func (s *Store) CreateServerGroup(ctx context.Context, g *model.ServerGroup) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO server_groups (id, name, description) VALUES ($1,$2,$3)`, g.ID, g.Name, g.Description)
	return mapError(err)
}
