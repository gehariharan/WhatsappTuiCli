package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"whatsappsync/internal/config"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

// Full references for the journal commands, appended to their catalog help.
const queryChangesUsage = `Usage: wasync [--data-dir PATH] query changes [--after CURSOR] [--limit N] [--v1]

Reads the change journal of the current account as a v1 envelope, oldest
first: data.changes lists sequence, cursor, op, chat_id, message_id and a
message ref, never message content. Pass page.next_cursor to --after to
continue; omit --after to start from the oldest retained change. --limit is
1 to 1000 (default 100). A cursor_expired error means the cursor is older than
the 30-day retention or from another archive or account: resnapshot.
Offline reads cannot commit checkpoints; commit through the running owner's
API (PUT /api/v1/checkpoints/{consumer}, scope observe).
`

const queryCheckpointUsage = `Usage: wasync [--data-dir PATH] query checkpoint --consumer NAME [--v1]

Reads a consumer's committed change-journal checkpoint as a v1 envelope.
Read only: committing is a write and goes through the running owner's API
(PUT /api/v1/checkpoints/{consumer}, scope observe).
`

func (a *App) runQueryJournal(ctx context.Context, cfg config.Config, command string, args []string) (result error) {
	defer func() { result = a.v1FailureCoded(result) }()
	fs := a.flags("query " + command)
	jsonOutput := fs.Bool("json", true, "JSON output (default)")
	fs.Bool("v1", true, "v1 envelope (always)")
	var after, consumer string
	limit := query.DefaultChangeLimit
	if command == "changes" {
		fs.StringVar(&after, "after", "", "cursor from a previous page (c1_...)")
		fs.IntVar(&limit, "limit", query.DefaultChangeLimit, "page size, 1 to 1000")
	} else {
		fs.StringVar(&consumer, "consumer", "", "consumer name ([a-z0-9-]{1,64})")
	}
	if err := a.parseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return queryErr(query.CodeInvalidArgument, err)
	}
	if fs.NArg() != 0 || !*jsonOutput {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("unexpected arguments; use wasync query %s --help", command))
	}
	if command == "changes" && (limit < 1 || limit > query.MaxChangeLimit) {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("--limit must be between 1 and %d", query.MaxChangeLimit))
	}
	if command == "checkpoint" {
		if consumer == "" {
			return queryErr(query.CodeInvalidArgument, fmt.Errorf("checkpoint requires --consumer NAME"))
		}
		if err := query.ValidConsumer(consumer); err != nil {
			return err
		}
	}
	store, err := storage.OpenReadOnly(ctx, filepath.Join(cfg.DataDir, "archive.db"))
	if err != nil {
		var schema *storage.SchemaError
		if errors.As(err, &schema) {
			return queryErr(query.CodeSchemaIncompatible, err)
		}
		return queryErr(query.CodeArchiveUnavailable, err)
	}
	defer func() { result = errors.Join(result, store.Close()) }()
	account, err := store.Account(ctx)
	if err != nil {
		return fmt.Errorf("read archive account: %w", err)
	}
	if account == nil {
		return queryErr(query.CodeNoAccount, fmt.Errorf("no account in the archive; start wasync and pair first"))
	}
	if command == "checkpoint" {
		cp, err := query.ReadCheckpoint(ctx, store, account, consumer)
		if err != nil {
			return err
		}
		return a.queryOutput(true, cp, nil)
	}
	data, page, err := query.ReadChanges(ctx, store, account, after, limit, nil)
	if err != nil {
		return err
	}
	return a.queryOutput(true, data, page)
}

// v1FailureCoded writes a v1 error envelope, including help from a
// query.CodedError.
func (a *App) v1FailureCoded(err error) error {
	var c *query.CodedError
	if err == nil || !errors.As(err, &c) {
		return a.v1Failure(true, err)
	}
	if writeErr := json.NewEncoder(a.Out).Encode(query.V1Error{RequestID: query.NewRequestID(), ErrorCode: c.Code, Error: err.Error(), Help: c.Help}); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	return &ReportedError{err}
}
