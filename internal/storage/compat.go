package storage

import (
	"context"
	"database/sql"
)

// schemaCompat is the forward-compatibility marker a future migration writes
// into the single-row table
//
//	schema_compat(id INTEGER PRIMARY KEY CHECK(id=1), min_reader INTEGER NOT NULL, min_writer INTEGER NOT NULL)
//
// An archive whose user_version is newer than this binary may still be opened
// when the binary's schema is at least MinWriter (owner, no migration) or at
// least MinReader (read-only query). Migration 006 creates the table with
// min_reader=5 and min_writer=6; see docs/architecture/migrations.md.
type schemaCompat struct {
	MinReader int
	MinWriter int
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// readSchemaCompat returns nil when the archive declares no compatibility
// marker, in which case a newer archive must be refused.
func readSchemaCompat(ctx context.Context, q rowQuerier) (*schemaCompat, error) {
	var tables int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_compat'").Scan(&tables); err != nil {
		return nil, err
	}
	if tables == 0 {
		return nil, nil
	}
	var c schemaCompat
	err := q.QueryRowContext(ctx, "SELECT min_reader,min_writer FROM schema_compat WHERE id=1").Scan(&c.MinReader, &c.MinWriter)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
