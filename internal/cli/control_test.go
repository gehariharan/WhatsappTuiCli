package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"whatsappsync/internal/api"
	"whatsappsync/internal/approval"
	"whatsappsync/internal/processlock"
	"whatsappsync/internal/storage"
	"whatsappsync/internal/tui"
	"whatsappsync/internal/whatsapp"
)

func TestCLIControlsReuseOwnerAndRequireTypedApproval(t *testing.T) {
	app, output, store, account, dir := queryFixture(t)
	ctx := context.Background()
	queue := &approval.Queue{}
	session := tui.NewSession(ctx, store, nil)
	session.Approvals = queue
	var syncs atomic.Int32
	server, err := api.StartControlled("127.0.0.1:0", dir, store, nil, queue, session.ApproveAction, func(_ context.Context, in whatsapp.SyncInput) (whatsapp.SyncResult, error) {
		syncs.Add(1)
		return whatsapp.SyncResult{Connection: "online", Completeness: "unknown"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	flags := []string{"--data-dir", dir, "--api-addr", strings.TrimPrefix(server.URL, "http://")}
	run := func(args ...string) error {
		output.Reset()
		return app.Run(ctx, append(append([]string{}, flags...), args...))
	}
	if err = run("groups", "list", "--json"); err != nil || !strings.Contains(output.String(), "one@g.us") {
		t.Fatalf("list: %v %s", err, output.String())
	}
	if err = run("groups", "disable", "--all"); err != nil {
		t.Fatal(err)
	}
	groups, err := store.Groups(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if storage.ChatKind(group.ID) == "group" && group.Enabled {
			t.Fatal("bulk disable not applied")
		}
	}
	if err = run("groups", "enable", "one@g.us"); err != nil {
		t.Fatal(err)
	}
	if err = run("sync", "--wait", "0s"); err != nil || syncs.Load() != 1 {
		t.Fatalf("sync: %v count=%d", err, syncs.Load())
	}
	if err = run("sync", "--wait", "61s"); err == nil || syncs.Load() != 1 {
		t.Fatal("invalid sync reached owner")
	}
	if err = run("clean", "--chat", "one@g.us", "--all"); err != nil {
		t.Fatal(err)
	}
	pending := queue.Pending(account)
	if pending == nil {
		t.Fatal("no approval request")
	}
	if err = run("actions", "approve", pending.ID); err == nil {
		t.Fatal("noninteractive approval allowed")
	}
	app.Interactive = true
	app.In = strings.NewReader("yes\n")
	if err = run("actions", "approve", pending.ID); err == nil {
		t.Fatal("weak confirmation accepted")
	}
	page, err := store.Messages(ctx, account, "one@g.us", storage.MessageQuery{Limit: 5})
	if err != nil || len(page.Items) != 3 {
		t.Fatal("rejected confirmation changed data")
	}
	app.In = strings.NewReader(fmt.Sprintf("DELETE %s\n", pending.ID[len(pending.ID)-6:]))
	app.Err = &bytes.Buffer{}
	if err = run("actions", "approve", pending.ID); err != nil {
		t.Fatal(err)
	}
	page, err = store.Messages(ctx, account, "one@g.us", storage.MessageQuery{Limit: 5})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("approved local purge failed")
	}
	if err = run("actions", "approve", pending.ID); err == nil {
		t.Fatal("approval replay accepted")
	}
}

type boundedSyncClient struct {
	fakeClient
	account int64
}

func (c *boundedSyncClient) EnableCapture(ctx context.Context, s *storage.Store) error {
	var err error
	c.account, err = s.UseAccount(ctx, c.AccountJID())
	return err
}
func (c *boundedSyncClient) LoginWithQR(context.Context, func(string)) error { return nil }
func (c *boundedSyncClient) Status() whatsapp.CaptureStatus {
	state := "offline"
	if c.connect {
		state = "online"
	}
	return whatsapp.CaptureStatus{Connection: state, AccountID: c.account}
}
func (c *boundedSyncClient) Maintain(context.Context) error { return nil }
func (c *boundedSyncClient) StopCapture(error)              {}
func TestStandaloneSyncConnectsOnceAndReleasesDevice(t *testing.T) {
	var out bytes.Buffer
	client := &boundedSyncClient{}
	app := &App{Out: &out, Err: &out, OpenClient: func(context.Context, string, *slog.Logger) (Client, error) { return client, nil }}
	dir := filepath.Join(t.TempDir(), "private")
	if err := app.Run(context.Background(), []string{"--data-dir", dir, "sync", "--wait", "0s"}); err != nil {
		t.Fatal(err)
	}
	if !client.connect || !client.closed || !strings.Contains(out.String(), "unknown") {
		t.Fatalf("sync lifecycle %+v %s", client, out.String())
	}
	lock, err := processlock.Acquire(filepath.Join(dir, "device.lock"))
	if err != nil {
		t.Fatal("sync left device locked", err)
	}
	lock.Close()
}

func TestOwnerSyncReturnsLargeSelectionAsOneJSONDocument(t *testing.T) {
	app, output, store, _, dir := queryFixture(t)
	chats := make([]storage.Group, 500)
	for i := range chats {
		chats[i] = storage.Group{ID: fmt.Sprintf("%d@g.us", i+1), Name: strings.Repeat("Synthetic group ", 16), Enabled: true, HistoryState: "unknown"}
	}
	server, err := api.StartControlled("127.0.0.1:0", dir, store, nil, nil, nil, func(context.Context, whatsapp.SyncInput) (whatsapp.SyncResult, error) {
		return whatsapp.SyncResult{Connection: "online", Chats: chats, Completeness: "unknown"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err = app.Run(context.Background(), []string{"--data-dir", dir, "--api-addr", strings.TrimPrefix(server.URL, "http://"), "sync", "--wait", "0s"}); err != nil {
		t.Fatal(err)
	}
	if output.Len() <= 64<<10 {
		t.Fatal("fixture did not exercise large response")
	}
	var response whatsapp.SyncResult
	if err = json.Unmarshal(output.Bytes(), &response); err != nil || len(response.Chats) != len(chats) {
		t.Fatalf("large selection lost: %d %v", len(response.Chats), err)
	}
}
func TestCommandResponseRejectsOversizeExplicitly(t *testing.T) {
	var out bytes.Buffer
	err := printCommandJSON(&out, strings.NewReader(strings.Repeat(" ", (4<<20)+1)))
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") || out.Len() != 0 {
		t.Fatalf("oversize response: %v %q", err, out.String())
	}
}
