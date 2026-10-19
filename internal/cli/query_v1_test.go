package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"whatsappsync/internal/golden"
	"whatsappsync/internal/localfs"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

var cliRequestID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestQueryVersionWithoutArchive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var out, stderr bytes.Buffer
	a := &App{Out: &out, Err: &stderr, OpenClient: func(context.Context, string, *slog.Logger) (Client, error) {
		t.Fatal("query opened WhatsApp")
		return nil, nil
	}}
	for _, args := range [][]string{{"version"}, {"version", "--json"}, {"version", "--v1"}} {
		out.Reset()
		if err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...)); err != nil {
			t.Fatal(err)
		}
		var got struct {
			RequestID string         `json:"request_id"`
			Data      map[string]any `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !cliRequestID.MatchString(got.RequestID) || got.Data["api_version"] != float64(1) || got.Data["schema_version"] != float64(storage.SchemaVersion) {
			t.Fatalf("version output %s", out.Bytes())
		}
		if _, ok := got.Data["archive_schema_version"]; ok {
			t.Fatalf("archive schema reported without archive: %s", out.Bytes())
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("query version created the data directory")
	}
	out.Reset()
	err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "version", "extra"})
	var reported *ReportedError
	var e query.V1Error
	if !errors.As(err, &reported) || json.Unmarshal(out.Bytes(), &e) != nil || e.ErrorCode != query.CodeInvalidArgument {
		t.Fatalf("bad version args: %v %s", err, out.Bytes())
	}
	out.Reset()
	if err = a.Run(context.Background(), []string{"--data-dir", dir, "query", "version", "--help"}); err != nil || !bytes.Contains(out.Bytes(), []byte("Usage: wasync query version")) {
		t.Fatalf("version help: %v %s", err, out.Bytes())
	}
}

func TestQueryVersionWithArchiveDoesNotMigrateOrCreate(t *testing.T) {
	a, out, writer, _, dir := queryFixture(t)
	before := dirEntries(t, dir)
	data := queryRun(t, a, out, dir, "version")
	var got query.V1Response
	var info query.VersionInfo
	got.Data = &info
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if info.ArchiveSchemaVersion == nil || *info.ArchiveSchemaVersion != storage.SchemaVersion {
		t.Fatalf("archive schema: %s", data)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	closed := dirEntries(t, dir)
	data = queryRun(t, a, out, dir, "version")
	if !bytes.Contains(data, []byte(`"archive_schema_version":5`)) {
		t.Fatalf("closed archive schema: %s", data)
	}
	if after := dirEntries(t, dir); len(after) != len(closed) {
		t.Fatalf("query version created files: %v -> %v (before %v)", closed, after, before)
	}
}

func TestQueryV1Envelopes(t *testing.T) {
	a, out, _, _, dir := queryFixture(t)
	for _, args := range [][]string{{"groups", "--v1"}, {"chats", "--v1", "--enabled"}, {"status", "--v1"}, {"messages", "--chat", "one@g.us", "--v1"}, {"search", "hello", "--v1"}} {
		data := queryRun(t, a, out, dir, args...)
		var got struct {
			RequestID string                     `json:"request_id"`
			Data      map[string]json.RawMessage `json:"data"`
			Page      *query.V1Page              `json:"page"`
		}
		if err := json.Unmarshal(data, &got); err != nil || !cliRequestID.MatchString(got.RequestID) || got.Data == nil {
			t.Fatalf("%v: %v %s", args, err, data)
		}
		if _, ok := got.Data["next_cursor"]; ok {
			t.Fatalf("%v: cursor left in data: %s", args, data)
		}
		if (args[0] == "messages" || args[0] == "search") != (got.Page != nil) {
			t.Fatalf("%v: page %s", args, data)
		}
	}
	data := queryRun(t, a, out, dir, "messages", "--chat", "one@g.us", "--limit", "2", "--v1")
	var page struct {
		Data struct{ Items []query.Message } `json:"data"`
		Page query.V1Page                    `json:"page"`
	}
	if err := json.Unmarshal(data, &page); err != nil || len(page.Data.Items) != 2 || !page.Page.HasMore || page.Page.NextCursor == "" {
		t.Fatalf("first page %s", data)
	}
	data = queryRun(t, a, out, dir, "messages", "--chat", "one@g.us", "--limit", "2", "--v1", "--cursor", page.Page.NextCursor)
	if err := json.Unmarshal(data, &page); err != nil || len(page.Data.Items) != 1 || page.Page.HasMore || page.Page.NextCursor != "" {
		t.Fatalf("last page %s", data)
	}
	// Without --v1 the legacy shape is untouched.
	data = queryRun(t, a, out, dir, "messages", "--chat", "one@g.us", "--limit", "2")
	if bytes.Contains(data, []byte("request_id")) || !bytes.Contains(data, []byte(`"next_cursor":"`)) {
		t.Fatalf("legacy output changed: %s", data)
	}
	for _, tc := range []struct {
		code string
		args []string
	}{
		{query.CodeInvalidArgument, []string{"unknown", "--v1"}},
		{query.CodeInvalidArgument, []string{"groups", "--v1", "--bogus"}},
		{query.CodeInvalidArgument, []string{"chats", "--v1", "extra"}},
		{query.CodeInvalidArgument, []string{"status", "--v1", "--json=false"}},
		{query.CodeInvalidArgument, []string{"messages", "--v1"}},
		{query.CodeInvalidArgument, []string{"messages", "--v1", "--chat", "one@g.us", "--limit", "0"}},
		{query.CodeInvalidArgument, []string{"search", "--v1", "--since", "bad", "hello"}},
		{query.CodeInvalidCursor, []string{"messages", "--v1", "--chat", "one@g.us", "--cursor", "garbage"}},
		{query.CodeNotFound, []string{"search", "hello", "--v1", "--chat", "missing@g.us"}},
	} {
		out.Reset()
		err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, tc.args...))
		var reported *ReportedError
		var e query.V1Error
		if !errors.As(err, &reported) || json.Unmarshal(out.Bytes(), &e) != nil || e.ErrorCode != tc.code || e.Error != err.Error() || !cliRequestID.MatchString(e.RequestID) {
			t.Fatalf("%v: %v %s", tc.args, err, out.Bytes())
		}
	}
}

func TestQueryV1ArchiveErrors(t *testing.T) {
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	run := func(dir, code string, args ...string) {
		t.Helper()
		out.Reset()
		err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
		var e query.V1Error
		if !errors.As(err, new(*ReportedError)) || json.Unmarshal(out.Bytes(), &e) != nil || e.ErrorCode != code {
			t.Fatalf("%v: %v %s", args, err, out.Bytes())
		}
	}
	missing := filepath.Join(t.TempDir(), "absent")
	for _, command := range []string{"groups", "chats", "status"} {
		run(missing, query.CodeArchiveUnavailable, command, "--v1")
	}
	run(missing, query.CodeArchiveUnavailable, "messages", "--chat", "one@g.us", "--v1")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("query created the data directory")
	}
	ctx := context.Background()
	empty := filepath.Join(t.TempDir(), "empty")
	if err := localfs.EnsureDir(empty); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, filepath.Join(empty, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run(empty, query.CodeNoAccount, "status", "--v1")
	old := filepath.Join(t.TempDir(), "old")
	if err = localfs.EnsureDir(old); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(old, "archive.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	run(old, query.CodeSchemaIncompatible, "groups", "--v1")
}

// TestQueryV1Snapshots freezes the v1 envelopes emitted by wasync query.
func TestQueryV1Snapshots(t *testing.T) {
	a, out, _, _, dir := queryFixture(t)
	snapshots := make([]any, 0)
	record := func(args ...string) {
		out.Reset()
		err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
		entry := map[string]any{"args": args, "exit_error": err != nil}
		entry["stdout"] = golden.Redact(golden.Decode(t, out.Bytes()), "request_id", "wasync_version", "features", "next_cursor", "received_at", "last_received_at", "last_discovery_at", "last_history_sync_at", "last_capture_heartbeat_at", "last_capture_finished_at")
		snapshots = append(snapshots, entry)
	}
	record("version")
	record("groups", "--v1")
	record("chats", "--v1", "--enabled")
	record("status", "--v1")
	record("messages", "--chat", "one@g.us", "--limit", "2", "--v1")
	record("search", "hello", "--v1")
	record("search", "no matches", "--v1")
	record("messages", "--v1")
	record("messages", "--chat", "missing@g.us", "--v1")
	record("messages", "--chat", "one@g.us", "--cursor", "garbage", "--v1")
	record("unknown", "--v1")
	golden.Compare(t, "v1/query.json", snapshots)
}
