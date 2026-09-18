// Package db manages the sqlite connection and schema.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure-Go sqlite driver (no cgo)
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps the sql.DB handle.
type DB struct {
	*sql.DB
}

// Open opens (creating if needed) the sqlite database at path, enables
// foreign keys and WAL, and brings the schema up to date by applying every
// pending migration under migrations/. This runs on every startup — a fresh
// database gets every migration from 00001 onward, and an existing database
// (from any prior version of Netra, including pre-migration-framework
// installs) only gets whatever's new since it was last run. See
// migrations/00001_baseline.sql for how the very first upgrade is handled
// safely.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// sqlite is single-writer; keep the pool small to avoid "database is locked".
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load embedded migrations: %w", err)
	}
	if err := migrate(sqlDB, migrations); err != nil {
		return nil, err
	}
	return &DB{sqlDB}, nil
}

// migrate applies every pending migration in fsys to sqlDB, in order. It's a
// thin wrapper around goose's Provider so the migrations filesystem can be
// swapped out in tests without touching the real embedded migrations.
func migrate(sqlDB *sql.DB, fsys fs.FS) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
