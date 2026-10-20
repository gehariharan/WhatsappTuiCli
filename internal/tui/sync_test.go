package tui

import (
	"context"
	"github.com/gehariharan/wasync/internal/approval"
	"github.com/gehariharan/wasync/internal/localfs"
	"github.com/gehariharan/wasync/internal/storage"
	"github.com/gehariharan/wasync/internal/whatsapp"
	"path/filepath"
	"testing"
	"time"
)

type syncClient struct{ status whatsapp.CaptureStatus }

func (c *syncClient) AccountJID() string                                  { return "synthetic@lid" }
func (c *syncClient) LoginWithQR(context.Context, func(string)) error     { return nil }
func (c *syncClient) EnableCapture(context.Context, *storage.Store) error { return nil }
func (c *syncClient) Groups(context.Context) ([]storage.Group, error) {
	return []storage.Group{{ID: "one@g.us", Name: "One"}}, nil
}
func (c *syncClient) Status() whatsapp.CaptureStatus { return c.status }
func (c *syncClient) Maintain(context.Context) error { return nil }
func (c *syncClient) Logout(context.Context) error   { return nil }
func (c *syncClient) StopCapture(error)              {}
func TestSyncQueuesEnabledHistoryAndHonorsCancellation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := localfs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.UseAccount(ctx, "synthetic@lid")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveGroups(ctx, account, []storage.Group{{ID: "one@g.us", Name: "One"}}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetEnabled(ctx, account, []string{"one@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	client := &syncClient{status: whatsapp.CaptureStatus{Connection: "online", AccountID: account}}
	session := NewSession(ctx, store, client)
	result, err := session.Sync(ctx, whatsapp.SyncInput{History: true, Chat: "one@g.us"})
	if err != nil || len(result.Chats) != 1 || result.Chats[0].HistoryRequest != "queued" || result.Completeness == "complete" {
		t.Fatalf("sync %+v %v", result, err)
	}
	if _, err = session.Sync(ctx, whatsapp.SyncInput{Chat: "missing@g.us"}); err == nil {
		t.Fatal("unknown chat accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	start := time.Now()
	if _, err = session.Sync(canceled, whatsapp.SyncInput{WaitSeconds: 60}); err == nil || time.Since(start) > time.Second {
		t.Fatal("sync ignored cancellation")
	}
	// Paired-account metadata must not flash during a new pairing.
	client.status = whatsapp.CaptureStatus{Connection: "pairing"}
	snapshot, err := session.Snapshot()
	if err != nil || len(snapshot.Groups) != 0 || snapshot.Account != 0 {
		t.Fatalf("stale pairing snapshot %+v %v", snapshot, err)
	}
}

type remoteSyncClient struct {
	syncClient
	revoked, deleted string
}

func (c *remoteSyncClient) RevokeOwnMessage(_ context.Context, _ string, m storage.Message) error {
	c.revoked = m.MessageID
	return nil
}
func (c *remoteSyncClient) DeleteWhatsAppChat(_ context.Context, _ string, m storage.Message) error {
	c.deleted = m.MessageID
	return nil
}
func TestRemoteApprovalWaitsForReadinessAndExecutesOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := localfs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	account, err := store.UseAccount(ctx, "synthetic@lid")
	if err != nil {
		t.Fatal(err)
	}
	chat := "one@g.us"
	if err = store.SaveGroups(ctx, account, []storage.Group{{ID: chat, Name: "One"}}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetEnabled(ctx, account, []string{chat}, true); err != nil {
		t.Fatal(err)
	}
	original := storage.Message{GroupID: chat, MessageID: "own", FromMe: true, SenderID: "synthetic@lid", Text: "synthetic", Timestamp: time.Now(), Type: "text"}
	if _, err = store.Apply(ctx, account, []storage.Message{original}); err != nil {
		t.Fatal(err)
	}
	client := &remoteSyncClient{syncClient: syncClient{status: whatsapp.CaptureStatus{Connection: "online", AccountID: account}}}
	session := NewSession(ctx, store, client)
	session.Approvals = &approval.Queue{}
	request, err := session.Approvals.Add(account, approval.Input{Kind: "revoke-message", Chat: chat, Message: "own"})
	if err != nil {
		t.Fatal(err)
	}
	session.network.Lock()
	if err = session.ApproveAction(account, request.ID); err == nil {
		t.Fatal("busy approval executed")
	}
	session.network.Unlock()
	if session.Approvals.Pending(account) == nil {
		t.Fatal("busy request consumed")
	}
	if err = session.ApproveAction(account, request.ID); err != nil || client.revoked != "own" {
		t.Fatal("revoke failed", err)
	}
	if err = session.ApproveAction(account, request.ID); err == nil {
		t.Fatal("remote approval replayed")
	}
	message, err := store.MessageByWhatsAppID(ctx, account, chat, "own")
	if err != nil || message.DeletedAt == nil || message.Text != "" {
		t.Fatal("revoke local clear failed", err)
	}
	original.MessageID = "newest"
	original.Timestamp = original.Timestamp.Add(time.Minute)
	if _, err = store.Apply(ctx, account, []storage.Message{original}); err != nil {
		t.Fatal(err)
	}
	request, err = session.Approvals.Add(account, approval.Input{Kind: "delete-whatsapp-chat", Chat: chat})
	if err != nil {
		t.Fatal(err)
	}
	if err = session.ApproveAction(account, request.ID); err != nil || client.deleted != "newest" {
		t.Fatal("chat deletion bound wrong", err)
	}
	message, err = store.MessageByWhatsAppID(ctx, account, chat, "newest")
	if err != nil || message.Text == "" {
		t.Fatal("remote chat deletion unexpectedly purged local archive", err)
	}
}
