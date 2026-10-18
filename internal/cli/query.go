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
	"strings"
	"time"

	"whatsappsync/internal/config"
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

func (a *App) runQuery(ctx context.Context, cfg config.Config, args []string) (result error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(a.Out, queryUsage)
		return err
	}
	command := args[0]
	if command != "groups" && command != "chats" && command != "messages" && command != "search" && command != "status" {
		return fmt.Errorf("unknown query command %q; use wasync query --help", command)
	}
	fs := a.flags("query " + command)
	fs.Usage = func() { _ = a.writeHelp(fs.Output(), "query "+command) }
	jsonOutput := fs.Bool("json", true, "JSON output (default; required for query commands)")
	var enabled bool
	var group, since, until, sender, cursor string
	limit := 50
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
		return err
	}
	positional := fs.Args()
	if command == "search" && search == "" && len(positional) == 1 {
		search = positional[0]
		positional = nil
	}
	if len(positional) != 0 {
		return fmt.Errorf("unexpected arguments; use wasync query --help")
	}
	if !*jsonOutput {
		return fmt.Errorf("query commands always return JSON; omit --json=false")
	}
	if command == "search" && (strings.TrimSpace(search) == "" || len(search) > 1024) {
		return fmt.Errorf("search requires a phrase of 1 to 1024 bytes")
	}
	if command == "messages" && group == "" {
		return fmt.Errorf("messages requires --chat CHAT_ID (or --group); list IDs with wasync query chats")
	}
	if limit < 1 || limit > 200 {
		return fmt.Errorf("--limit must be between 1 and 200")
	}
	q := storage.MessageQuery{Limit: limit, Search: strings.TrimSpace(search), Sender: sender}
	for name, entry := range map[string]struct {
		value  string
		target **time.Time
	}{"--since": {since, &q.From}, "--until": {until, &q.To}} {
		if entry.value != "" {
			t, err := time.Parse(time.RFC3339Nano, entry.value)
			if err != nil {
				return fmt.Errorf("%s must be RFC3339, for example 2026-10-01T00:00:00Z", name)
			}
			t = t.UTC()
			*entry.target = &t
		}
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return fmt.Errorf("--since must be before --until")
	}
	store, err := storage.OpenReadOnly(ctx, filepath.Join(cfg.DataDir, "archive.db"))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, store.Close()) }()
	account, err := store.Account(ctx)
	if err != nil {
		return fmt.Errorf("read archive account: %w", err)
	}
	if account == nil {
		return fmt.Errorf("no account in the archive; start wasync and pair first")
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
			return json.NewEncoder(a.Out).Encode(map[string]any{"account_id": account.ID, "items": items})
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
		return json.NewEncoder(a.Out).Encode(status)
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
			return fmt.Errorf("group is not in the current account's archive; list IDs with wasync query groups")
		}
	}
	scope := queryScope(account.ID, group, q)
	if cursor != "" {
		q.Before, err = decodeQueryCursor(cursor, scope)
		if err != nil {
			return err
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
	return json.NewEncoder(a.Out).Encode(map[string]any{"account_id": account.ID, "items": items, "next_cursor": next})
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
