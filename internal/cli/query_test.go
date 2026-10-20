package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gehariharan/wasync/internal/localfs"
	"github.com/gehariharan/wasync/internal/processlock"
	"github.com/gehariharan/wasync/internal/query"
	"github.com/gehariharan/wasync/internal/storage"
)

func queryFixture(t *testing.T) (*App, *bytes.Buffer, *storage.Store, int64, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := localfs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	writer, err := storage.Open(ctx, filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	account, err := writer.UseAccount(ctx, "test@lid")
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.SaveGroups(ctx, account, []storage.Group{{ID: "one@g.us", Name: "First"}, {ID: "two@g.us", Name: "Second"}}); err != nil {
		t.Fatal(err)
	}
	if err = writer.SetEnabled(ctx, account, []string{"one@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_, err = writer.Apply(ctx, account, []storage.Message{{GroupID: "one@g.us", MessageID: fmt.Sprint(i), SenderID: "sender@lid", Timestamp: stamp, Type: "text", Text: "hello archive", Source: "live"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	lock, err := processlock.Acquire(filepath.Join(dir, "device.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	var out bytes.Buffer
	app := &App{Out: &out, Err: &bytes.Buffer{}, OpenClient: func(context.Context, string, *slog.Logger) (Client, error) {
		t.Fatal("query opened WhatsApp")
		return nil, nil
	}}
	return app, &out, writer, account, dir
}
func queryRun(t *testing.T, a *App, out *bytes.Buffer, dir string, args ...string) []byte {
	t.Helper()
	out.Reset()
	root := append([]string{"--data-dir", dir, "query"}, args...)
	if err := a.Run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), out.Bytes()...)
	if !json.Valid(data) {
		t.Fatalf("non-JSON stdout: %s", data)
	}
	return data
}
func TestQueryWhileCaptureOwnsLock(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	// Read-only commands do not depend on HTTP configuration or create credentials.
	t.Setenv("WASYNC_API_ADDR", "invalid API config")
	data := queryRun(t, a, out, dir, "groups", "--enabled", "--json")
	var groups struct {
		Items []query.Group `json:"items"`
	}
	if err := json.Unmarshal(data, &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups.Items) != 1 || groups.Items[0].ID != "one@g.us" || groups.Items[0].MessageCount != 3 {
		t.Fatalf("bad groups %s", data)
	}
	data = queryRun(t, a, out, dir, "status")
	var status storage.ArchiveSnapshot
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatal(err)
	}
	if status.Connection != "unknown" || status.MessageCount != 3 || status.EnabledGroups != 1 {
		t.Fatalf("bad persisted status %s", data)
	}
	if _, err := writer.Apply(context.Background(), account, []storage.Message{{GroupID: "one@g.us", MessageID: "latest", Timestamp: time.Now(), Text: "fresh content", Type: "text", Source: "live"}}); err != nil {
		t.Fatal(err)
	}
	data = queryRun(t, a, out, dir, "search", "fresh", "--group", "one@g.us")
	var page struct {
		Items []query.Message `json:"items"`
		Next  string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].MessageID != "latest" {
		t.Fatalf("new capture not visible %s", data)
	}
	for _, name := range []string{"session.db", "api.token"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("query touched %s", name)
		}
	}
}
func TestQueryPaginationFiltersAndAccountScope(t *testing.T) {
	a, out, writer, _, dir := queryFixture(t)
	var page struct {
		Items []query.Message `json:"items"`
		Next  string          `json:"next_cursor"`
	}
	data := queryRun(t, a, out, dir, "messages", "--group", "one@g.us", "--limit", "1")
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Next == "" {
		t.Fatalf("bad page %s", data)
	}
	first := page.Items[0].ID
	cursor := page.Next
	data = queryRun(t, a, out, dir, "messages", "--group", "one@g.us", "--limit", "1", "--cursor", cursor)
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID >= first {
		t.Fatal("cursor did not advance")
	}
	for _, args := range [][]string{
		{"search", "hello", "--sender", "sender@lid", "--since", "2026-10-01T00:00:00Z", "--until", "2026-10-02T00:00:00Z"},
		{"search", "--group", "one@g.us", "hello"},
	} {
		data = queryRun(t, a, out, dir, args...)
		if err := json.Unmarshal(data, &page); err != nil || len(page.Items) != 3 {
			t.Fatalf("filters failed: %s %v", data, err)
		}
	}
	data = queryRun(t, a, out, dir, "search", "no matches")
	if err := json.Unmarshal(data, &page); err != nil || len(page.Items) != 0 {
		t.Fatal("empty search failed")
	}
	for _, args := range [][]string{
		{"messages", "--group", "one@g.us", "--limit", "201"},
		{"messages"}, {"search"}, {"search", "hello", "extra"},
		{"messages", "--group", "one@g.us", "--since", "bad"},
		{"messages", "--group", "one@g.us", "--since", "2026-10-02T00:00:00Z", "--until", "2026-10-01T00:00:00Z"},
		{"messages", "--group", "two@g.us", "--cursor", cursor},
		{"messages", "--group", "one@g.us", "--cursor", "garbage"},
		{"messages", "--group", "one@g.us", "--sender", "other", "--cursor", cursor},
	} {
		out.Reset()
		if err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...)); err == nil || out.Len() != 0 {
			t.Fatalf("bad query accepted: %v", args)
		}
	}
	if _, err := writer.UseAccount(context.Background(), "another@lid"); err != nil {
		t.Fatal(err)
	}
	data = queryRun(t, a, out, dir, "search", "hello")
	if err := json.Unmarshal(data, &page); err != nil || len(page.Items) != 0 {
		t.Fatal("query crossed account boundary")
	}
}
func TestQueryHelpAndMissingArchiveDoNotCreateFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created")
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	for _, args := range [][]string{{"query"}, {"query", "--help"}, {"query", "messages", "--help"}} {
		if err := a.Run(context.Background(), append([]string{"--data-dir", dir}, args...)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "groups"}); err == nil {
		t.Fatal("missing archive accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("query created data directory")
	}
}

func TestNoCommandStillLaunchesTUI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	var out bytes.Buffer
	expected := errors.New("TUI client opened")
	opened := false
	a := &App{Out: &out, Err: &bytes.Buffer{}, Interactive: true, OpenClient: func(context.Context, string, *slog.Logger) (Client, error) { opened = true; return nil, expected }}
	err := a.Run(context.Background(), []string{"--data-dir", dir, "--no-api"})
	if !opened || !errors.Is(err, expected) {
		t.Fatalf("no-command invocation did not start TUI: %v", err)
	}
}
