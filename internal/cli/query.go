package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"whatsappsync/internal/config"
	"whatsappsync/internal/msgref"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

const queryUsage = `Usage: wasync [--data-dir PATH] query COMMAND

  groups [--enabled] [--json]
  chats [--enabled] [--json]
  messages --chat CHAT_ID [--since RFC3339] [--until RFC3339]
           [--sender SENDER_ID] [--limit 50] [--cursor CURSOR] [--json]
  search "phrase" [--chat CHAT_ID] [--since RFC3339] [--until RFC3339]
           [--sender SENDER_ID] [--limit 50] [--cursor CURSOR] [--json]
  status [--json]

All results are JSON. Queries read the existing archive without connecting to
WhatsApp, requiring an API, or stopping the TUI. Dates are inclusive --since and
exclusive --until; results are newest first. Pass next_cursor to --cursor with
the same filters for the next page. An empty items array is a successful result.
Status describes persisted observations; current network connectivity is unknown.
Global flags precede query. Exit codes: 0 success, 1 error, 130 cancellation.
`

// queryMessageUsage documents `query message`, which is intentionally not yet
// listed in queryUsage or the command catalog (their output is snapshotted).
const queryMessageUsage = `Usage: wasync [--data-dir PATH] query message --ref REF [--json]

Prints one archived message by its stable, account-scoped reference (m1_...).
References survive restarts, purge and re-capture; local row ids do not.
Expired content is never returned; revoked messages return their tombstone.
`

func (a *App) runQuery(ctx context.Context, cfg config.Config, args []string) (result error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(a.Out, queryUsage)
		return err
	}
	command := args[0]
	if command == "version" {
		return a.runQueryVersion(ctx, cfg, args[1:])
	}
	// --v1 is detected before parsing so argument errors also use the envelope.
	v1 := slices.Contains(args[1:], "--v1") || slices.Contains(args[1:], "--v1=true")
	defer func() { result = a.v1Failure(v1, result) }()
	if command != "groups" && command != "chats" && command != "messages" && command != "search" && command != "status" && command != "message" {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("unknown query command %q; use wasync query --help", command))
	}
	fs := a.flags("query " + command)
	fs.Usage = func() { _ = a.writeHelp(fs.Output(), "query "+command) }
	if command == "message" {
		fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), queryMessageUsage) }
	}
	jsonOutput := fs.Bool("json", true, "JSON output (default; required for query commands)")
	fs.BoolVar(&v1, "v1", v1, "emit the versioned v1 envelope")
	var enabled bool
	var group, since, until, sender, cursor string
	limit := 50
	var refFlag string
	if command == "message" {
		fs.StringVar(&refFlag, "ref", "", "stable message reference (m1_...)")
	}
	if command == "groups" || command == "chats" {
		fs.BoolVar(&enabled, "enabled", false, "only groups with capture enabled")
	}
	if command == "messages" || command == "search" {
		fs.StringVar(&group, "group", "", "WhatsApp group ID (legacy alias for --chat)")
		fs.StringVar(&group, "chat", "", "group or direct chat ID")
		fs.StringVar(&since, "since", "", "inclusive RFC3339 timestamp")
		fs.StringVar(&until, "until", "", "exclusive RFC3339 timestamp")
		fs.StringVar(&sender, "sender", "", "exact sender ID")
		fs.StringVar(&cursor, "cursor", "", "cursor from a previous result page")
		fs.IntVar(&limit, "limit", 50, "page size, 1 to 200")
	}
	rest := args[1:]
	search := ""
	// Accept both search "phrase" --flags and search --flags "phrase".
	if command == "search" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		search = rest[0]
		rest = rest[1:]
	}
	if err := a.parseFlags(fs, rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return queryErr(query.CodeInvalidArgument, err)
	}
	positional := fs.Args()
	if command == "search" && search == "" && len(positional) == 1 {
		search = positional[0]
		positional = nil
	}
	if len(positional) != 0 {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("unexpected arguments; use wasync query --help"))
	}
	if !*jsonOutput {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("query commands always return JSON; omit --json=false"))
	}
	if command == "search" && (strings.TrimSpace(search) == "" || len(search) > 1024) {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("search requires a phrase of 1 to 1024 bytes"))
	}
	if command == "messages" && group == "" {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("messages requires --chat CHAT_ID (or --group); list IDs with wasync query chats"))
	}
	var ref msgref.Ref
	if command == "message" {
		if refFlag == "" {
			return queryErr(query.CodeInvalidArgument, fmt.Errorf("message requires --ref REF"))
		}
		var err error
		if ref, err = msgref.Decode(refFlag); err != nil {
			return queryErr(query.CodeInvalidArgument, err)
		}
	}
	if limit < 1 || limit > 200 {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("--limit must be between 1 and 200"))
	}
	q := storage.MessageQuery{Limit: limit, Search: strings.TrimSpace(search), Sender: sender}
	for name, entry := range map[string]struct {
		value  string
		target **time.Time
	}{"--since": {since, &q.From}, "--until": {until, &q.To}} {
		if entry.value != "" {
			t, err := time.Parse(time.RFC3339Nano, entry.value)
			if err != nil {
				return queryErr(query.CodeInvalidArgument, fmt.Errorf("%s must be RFC3339, for example 2026-10-01T00:00:00Z", name))
			}
			t = t.UTC()
			*entry.target = &t
		}
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("--since must be before --until"))
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
	if command == "message" {
		m, err := store.MessageByRef(ctx, account.ID, account.JID, ref)
		if errors.Is(err, storage.ErrMessageNotFound) {
			return queryErr(query.CodeNotFound, fmt.Errorf("message is not in the current account's archive; it may have expired, been purged, or belong to another account"))
		}
		if err != nil {
			return fmt.Errorf("query message: %w", err)
		}
		return a.queryOutput(v1, query.MessageByRef{AccountID: account.ID, Ref: query.RefFor(account.JID, m), Item: query.FormatMessage(account.ID, m)}, nil)
	}
	if command == "groups" || command == "chats" || command == "status" {
		groups, err := store.GroupSummaries(ctx, account.ID)
		if err != nil {
			return err
		}
		if command == "groups" || command == "chats" {
			items := make([]query.Group, 0, len(groups))
			for _, g := range groups {
				if (command == "chats" || storage.ChatKind(g.ID) == "group") && (!enabled || g.Enabled) {
					items = append(items, query.FormatGroup(g))
				}
			}
			return a.queryOutput(v1, map[string]any{"account_id": account.ID, "items": items}, nil)
		}
		status, err := store.ArchiveStatus(ctx, account.ID)
		if err != nil {
			return err
		}
		status.ChatCount = len(groups)
		for _, g := range groups {
			if g.Enabled {
				status.EnabledChats++
			}
			if storage.ChatKind(g.ID) == "group" {
				status.GroupCount++
				if g.Enabled {
					status.EnabledGroups++
				}
			}
			status.MessageCount += g.MessageCount
		}
		return a.queryOutput(v1, status, nil)
	}
	if group != "" {
		groups, err := store.Groups(ctx, account.ID)
		if err != nil {
			return err
		}
		found := false
		for _, g := range groups {
			if g.ID == group {
				found = true
				break
			}
		}
		if !found {
			return queryErr(query.CodeNotFound, fmt.Errorf("group is not in the current account's archive; list IDs with wasync query groups"))
		}
	}
	scope := queryScope(account.ID, group, q)
	if cursor != "" {
		q.Before, err = decodeQueryCursor(cursor, scope)
		if err != nil {
			return queryErr(query.CodeInvalidCursor, err)
		}
	}
	page, err := store.Messages(ctx, account.ID, group, q)
	if err != nil {
		return fmt.Errorf("query messages: %w", err)
	}
	items := make([]query.Message, 0, len(page.Items))
	for _, m := range page.Items {
		items = append(items, query.FormatMessage(account.ID, m))
	}
	next := ""
	if page.Next.ID != 0 {
		next = encodeQueryCursor(page.Next, scope)
	}
	if v1 {
		return a.queryOutput(v1, map[string]any{"account_id": account.ID, "items": items}, &query.V1Page{NextCursor: next, HasMore: next != ""})
	}
	return json.NewEncoder(a.Out).Encode(map[string]any{"account_id": account.ID, "items": items, "next_cursor": next})
}

// queryError carries a v1 error code; its message is the legacy error text.
type queryError struct {
	code string
	err  error
}

func (e *queryError) Error() string         { return e.err.Error() }
func (e *queryError) Unwrap() error         { return e.err }
func queryErr(code string, err error) error { return &queryError{code, err} }

// ReportedError marks a failure already written to stdout as a v1 envelope;
// callers exit with status 1 without printing it again.
type ReportedError struct{ Err error }

func (e *ReportedError) Error() string { return e.Err.Error() }
func (e *ReportedError) Unwrap() error { return e.Err }

func (a *App) queryOutput(v1 bool, data any, page *query.V1Page) error {
	if !v1 {
		return json.NewEncoder(a.Out).Encode(data)
	}
	return json.NewEncoder(a.Out).Encode(query.V1Response{RequestID: query.NewRequestID(), Data: data, Page: page})
}

func (a *App) v1Failure(v1 bool, err error) error {
	if !v1 || err == nil || errors.Is(err, flag.ErrHelp) {
		return err
	}
	code := query.CodeInternal
	var coded *queryError
	if errors.As(err, &coded) {
		code = coded.code
	}
	if writeErr := json.NewEncoder(a.Out).Encode(query.V1Error{RequestID: query.NewRequestID(), ErrorCode: code, Error: err.Error()}); writeErr != nil {
		return errors.Join(err, writeErr)
	}
	return &ReportedError{err}
}

func (a *App) runQueryVersion(ctx context.Context, cfg config.Config, args []string) (result error) {
	defer func() { result = a.v1Failure(true, result) }()
	fs := a.flags("query version")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "Usage: wasync query version [--json]\n\nReport API, build and schema versions as a v1 envelope without opening the archive for writes.\n")
	}
	jsonOutput := fs.Bool("json", true, "JSON output (default)")
	fs.Bool("v1", true, "v1 envelope (always)")
	if err := a.parseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return queryErr(query.CodeInvalidArgument, err)
	}
	if fs.NArg() != 0 || !*jsonOutput {
		return queryErr(query.CodeInvalidArgument, fmt.Errorf("usage: wasync query version [--json]"))
	}
	info := query.Version()
	version, exists, err := storage.ArchiveSchemaVersion(ctx, filepath.Join(cfg.DataDir, "archive.db"))
	if err != nil {
		return queryErr(query.CodeArchiveUnavailable, err)
	}
	if exists {
		info.ArchiveSchemaVersion = &version
	}
	return a.queryOutput(true, info, nil)
}

type queryCursor struct {
	Version int            `json:"v"`
	Before  storage.Cursor `json:"before"`
	Scope   string         `json:"scope"`
}

func queryScope(account int64, group string, q storage.MessageQuery) string {
	data, _ := json.Marshal([]any{account, group, q.Search, q.Sender, q.From, q.To})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func encodeQueryCursor(before storage.Cursor, scope string) string {
	data, _ := json.Marshal(queryCursor{1, before, scope})
	return base64.RawURLEncoding.EncodeToString(data)
}
func decodeQueryCursor(value, scope string) (storage.Cursor, error) {
	var c queryCursor
	if len(value) > 1024 {
		return storage.Cursor{}, fmt.Errorf("invalid cursor or filters changed; start again without --cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Before.ID <= 0 || c.Scope != scope {
		return storage.Cursor{}, fmt.Errorf("invalid cursor or filters changed; start again without --cursor")
	}
	return c.Before, nil
}
