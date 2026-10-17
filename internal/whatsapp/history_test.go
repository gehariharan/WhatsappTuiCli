package whatsapp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"whatsappsync/internal/storage"
)

func historyClient(t *testing.T) (*Client, *storage.Store, int64) {
	t.Helper()
	ctx := context.Background()
	s, err := storage.Open(ctx, filepath.Join(t.TempDir(), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := s.UseAccount(ctx, "own@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveGroups(ctx, account, []storage.Group{{ID: "123@g.us", Name: "enabled"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetEnabled(ctx, account, []string{"123@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	c, err := Open(ctx, filepath.Join(t.TempDir(), "session.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	c.capture = &capture{ctx: ctx, store: s}
	c.capture.account.Store(account)
	t.Cleanup(func() { c.Close(); s.Close() })
	return c, s, account
}
func historyResponse(request string, stamp int64, id string) *events.HistorySync {
	data := &waHistorySync.HistorySync{SyncType: waHistorySync.HistorySync_ON_DEMAND.Enum()}
	if id != "" {
		data.Conversations = []*waHistorySync.Conversation{{ID: proto.String("123@g.us"), Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{Key: &waCommon.MessageKey{RemoteJID: proto.String("123@g.us"), ID: proto.String(id), Participant: proto.String("sender@lid")}, MessageTimestamp: proto.Uint64(uint64(stamp)), Message: &waE2E.Message{Conversation: proto.String("historical text")}}}}}}
	}
	return &events.HistorySync{Data: data, Notification: &waE2E.HistorySyncNotification{PeerDataRequestSessionID: proto.String(request), OriginalMessageID: proto.String("unrelated notification ID")}}
}
func TestAutomaticHistoryUsesOwnDeviceAndPaginates(t *testing.T) {
	c, s, account := historyClient(t)
	ctx := context.Background()
	stamp := time.Unix(1700000000, 0)
	_, err := s.Apply(ctx, account, []storage.Message{{GroupID: "123@g.us", MessageID: "anchor", SenderID: "own@s.whatsapp.net", Timestamp: stamp, FromMe: true, Type: "text", Text: "live", Source: "live"}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	requestID := ""
	c.sendHistory = func(_ context.Context, to types.JID, m *waE2E.Message, extra whatsmeow.SendRequestExtra) error {
		calls++
		requestID = extra.ID
		if to.String() != "own@s.whatsapp.net" || !extra.Peer || extra.ID == "" {
			t.Fatal("history request targeted a conversation instead of our own device")
		}
		p := m.GetProtocolMessage().GetPeerDataOperationRequestMessage()
		req := p.GetHistorySyncOnDemandRequest()
		if p.GetPeerDataOperationRequestType() != waE2E.PeerDataOperationRequestType_HISTORY_SYNC_ON_DEMAND || req.GetChatJID() != "123@g.us" || req.GetOnDemandMsgCount() != 50 || m.GetConversation() != "" {
			t.Fatal("wrong protocol request")
		}
		if calls == 1 && (req.GetOldestMsgID() != "anchor" || !req.GetOldestMsgFromMe() || req.GetOldestMsgTimestampMS() != stamp.Unix()) {
			t.Fatal("anchor or seconds timestamp encoded incorrectly")
		}
		if calls == 2 && (req.GetOldestMsgID() != "older" || req.GetOldestMsgTimestampMS() != stamp.Add(-time.Hour).Unix()) {
			t.Fatal("next request did not use older cursor")
		}
		return nil
	}
	if err = c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("enabled group did not automatically request history")
	}
	if err = c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("duplicate request while waiting")
	}
	if err = c.captureEvent(historyResponse("stale-request", stamp.Add(-time.Hour).Unix(), "stale")); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingHistory(ctx, account)
	if err != nil || pending == nil {
		t.Fatal("stale response completed the active request")
	}
	if err = c.captureEvent(historyResponse(requestID, stamp.Add(-time.Hour).Unix(), "older")); err != nil {
		t.Fatal(err)
	}
	if err = c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("older response did not queue the next page")
	}
	if err = c.captureEvent(historyResponse(requestID, 0, "")); err != nil {
		t.Fatal(err)
	}
	if err = c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("empty page did not stop automatic requests")
	}
}
func TestHistoryWaitsForAnchorThenUsesNewCapture(t *testing.T) {
	c, s, account := historyClient(t)
	ctx := context.Background()
	calls := 0
	c.sendHistory = func(context.Context, types.JID, *waE2E.Message, whatsmeow.SendRequestExtra) error {
		calls++
		return nil
	}
	if err := c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := s.NextHistory(ctx, account)
	if err != nil || job == nil || job.State != "waiting_anchor" || calls != 0 {
		t.Fatal("sent an invented anchor")
	}
	e := fixture(&waE2E.Message{Conversation: proto.String("first captured text")})
	if !c.handleEvent(e) {
		t.Fatal("live capture failed")
	}
	if err = c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("first captured message did not unblock queued history")
	}
}
func TestMemoryCursorDoesNotArchiveDisabledGroups(t *testing.T) {
	c, s, account := historyClient(t)
	ctx := context.Background()
	if err := s.SetEnabled(ctx, account, []string{"123@g.us"}, false); err != nil {
		t.Fatal(err)
	}
	e := fixture(&waE2E.Message{Conversation: proto.String("must never be archived")})
	if !c.handleEvent(e) {
		t.Fatal("event rejected")
	}
	page, err := s.Messages(ctx, account, "123@g.us", storage.MessageQuery{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("disabled content retained")
	}
	if err = s.SetEnabled(ctx, account, []string{"123@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	c.sendHistory = func(_ context.Context, _ types.JID, m *waE2E.Message, _ whatsmeow.SendRequestExtra) error {
		if m.GetProtocolMessage().GetPeerDataOperationRequestMessage().GetHistorySyncOnDemandRequest().GetOldestMsgID() != e.Info.ID {
			t.Fatal("RAM cursor was not used")
		}
		return nil
	}
	if err = c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingHistory(ctx, account)
	if err != nil || pending == nil {
		t.Fatal("enabling did not use known cursor")
	}
}
func TestHistorySendFailureDoesNotStopLiveCapture(t *testing.T) {
	c, s, account := historyClient(t)
	ctx := context.Background()
	e := fixture(&waE2E.Message{Conversation: proto.String("captured")})
	if !c.handleEvent(e) {
		t.Fatal("capture failed")
	}
	c.sendHistory = func(context.Context, types.JID, *waE2E.Message, whatsmeow.SendRequestExtra) error {
		return errors.New("offline phone")
	}
	if err := c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	if c.capture.failed.Load() {
		t.Fatal("history send failure stopped live capture")
	}
	groups, err := s.GroupSummaries(ctx, account)
	if err != nil || groups[0].HistoryRequest != "failed" {
		t.Fatal("request send failure not reported")
	}
}

func TestFastHistoryResponseCannotBeOverwrittenBySendAck(t *testing.T) {
	c, _, _ := historyClient(t)
	ctx := context.Background()
	e := fixture(&waE2E.Message{Conversation: proto.String("live anchor")})
	if !c.handleEvent(e) {
		t.Fatal("capture failed")
	}
	c.sendHistory = func(_ context.Context, _ types.JID, _ *waE2E.Message, extra whatsmeow.SendRequestExtra) error {
		return c.captureEvent(historyResponse(extra.ID, e.Info.Timestamp.Add(-time.Hour).Unix(), "older-fast-response"))
	}
	if err := c.historyStep(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := c.capture.store.NextHistory(ctx, c.capture.account.Load())
	if err != nil || job == nil || job.Anchor.ID != "older-fast-response" {
		t.Fatal("send acknowledgement clobbered an already received page")
	}
}
func TestHistoryWorkerClosesWithoutConnecting(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, filepath.Join(t.TempDir(), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := Open(ctx, filepath.Join(t.TempDir(), "session.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.EnableCapture(ctx, s); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("history worker blocked shutdown")
	}
}
