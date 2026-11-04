// Package database owns the PostgreSQL pool and embedded schema migrations.
package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rsdenck/rscon/migrations"
)

// DB wraps a pgx connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Open creates and verifies a PostgreSQL pool.
func Open(ctx context.Context, url string, maxConns, minConns int32, connectTimeout time.Duration) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if minConns >= 0 {
		cfg.MinConns = minConns
	}
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["application_name"] = "rscond"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases all pooled connections.
func (db *DB) Close() { db.Pool.Close() }

// Ping checks database connectivity.
func (db *DB) Ping(ctx context.Context) error { return db.Pool.Ping(ctx) }

// Migration describes one embedded migration.
type Migration struct {
	Version  int
	Name     string
	UpSQL    string
	DownSQL  string
	Checksum string
}

// MigrationStatus is a row of the migration history.
type MigrationStatus struct {
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	Checksum  string    `json:"checksum"`
	AppliedAt time.Time `json:"applied_at"`
}

// LoadMigrations parses the embedded migration files.
func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	byVersion := map[int]*Migration{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration %q: expected NNNN_name.[up|down].sql", name)
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("migration %q: invalid version: %w", name, err)
		}
		rest := parts[1]
		direction := ""
		switch {
		case strings.HasSuffix(rest, ".up.sql"):
			direction = "up"
			rest = strings.TrimSuffix(rest, ".up.sql")
		case strings.HasSuffix(rest, ".down.sql"):
			direction = "down"
			rest = strings.TrimSuffix(rest, ".down.sql")
		default:
			return nil, fmt.Errorf("migration %q: expected .up.sql or .down.sql", name)
		}
		data, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		m, ok := byVersion[version]
		if !ok {
			m = &Migration{Version: version, Name: rest}
			byVersion[version] = m
		}
		if direction == "up" {
			m.UpSQL = string(data)
			sum := sha256.Sum256(data)
			m.Checksum = hex.EncodeToString(sum[:])
			m.Name = rest
		} else {
			m.DownSQL = string(data)
		}
	}
	result := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.UpSQL == "" {
			return nil, fmt.Errorf("migration %d: missing up file", m.Version)
		}
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for i := 1; i < len(result); i++ {
		if result[i].Version == result[i-1].Version {
			return nil, fmt.Errorf("duplicate migration version %d", result[i].Version)
		}
	}
	return result, nil
}

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    integer PRIMARY KEY,
    name       text NOT NULL,
    checksum   text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

// Migrate applies all pending migrations. Applied migrations are checksum
// verified; a mismatch aborts before any change is made.
func Migrate(ctx context.Context, db *DB, logger *slog.Logger) error {
	if _, err := db.Pool.Exec(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	list, err := LoadMigrations()
	if err != nil {
		return err
	}
	applied := map[int]string{}
	rows, err := db.Pool.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[version] = checksum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range list {
		if checksum, ok := applied[m.Version]; ok {
			if checksum != m.Checksum {
				return fmt.Errorf("migration %04d_%s checksum mismatch: database was migrated with different SQL", m.Version, m.Name)
			}
			continue
		}
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(ctx, m.UpSQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %04d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
			m.Version, m.Name, m.Checksum); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
		if logger != nil {
			logger.Info("database migration applied", "version", m.Version, "name", m.Name)
		}
	}
	return nil
}

// Rollback reverts the most recent n applied migrations.
func Rollback(ctx context.Context, db *DB, steps int, logger *slog.Logger) error {
	if steps < 1 {
		return fmt.Errorf("steps must be >= 1")
	}
	if _, err := db.Pool.Exec(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	list, err := LoadMigrations()
	if err != nil {
		return err
	}
	byVersion := map[int]Migration{}
	for _, m := range list {
		byVersion[m.Version] = m
	}
	rows, err := db.Pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT $1`, steps)
	if err != nil {
		return err
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		versions = append(versions, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("no applied migrations to roll back")
	}
	for _, version := range versions {
		m, ok := byVersion[version]
		if !ok || m.DownSQL == "" {
			return fmt.Errorf("migration %d has no down file", version)
		}
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, m.DownSQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("rollback migration %04d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		if logger != nil {
			logger.Info("database migration rolled back", "version", m.Version, "name", m.Name)
		}
	}
	return nil
}

// MigrationHistory returns applied migrations, newest first.
func MigrationHistory(ctx context.Context, db *DB) ([]MigrationStatus, error) {
	if _, err := db.Pool.Exec(ctx, createMigrationsTable); err != nil {
		return nil, err
	}
	rows, err := db.Pool.Query(ctx, `SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationStatus
	for rows.Next() {
		var m MigrationStatus
		if err := rows.Scan(&m.Version, &m.Name, &m.Checksum, &m.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
