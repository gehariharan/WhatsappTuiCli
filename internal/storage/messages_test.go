package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func captureStore(t *testing.T) (*Store, int64) {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	id, err := s.UseAccount(context.Background(), "one@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveGroups(context.Background(), id, []Group{{ID: "one@g.us", Name: "Enabled"}, {ID: "two@g.us", Name: "Disabled"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetEnabled(context.Background(), id, []string{"one@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	return s, id
}
func original(id, text string) Message {
	return Message{GroupID: "one@g.us", MessageID: id, SenderID: "sender@lid", Text: text, Type: "text", Timestamp: time.Unix(1700000000, 0), Source: "live"}
}
func mustApply(t *testing.T, s *Store, account int64, items ...Message) WriteResult {
	t.Helper()
	r, err := s.Apply(context.Background(), account, items)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func page(t *testing.T, s *Store, account int64, q MessageQuery) MessagePage {
	t.Helper()
	p, err := s.Messages(context.Background(), account, "one@g.us", q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCaptureConsentDedupAndRestart(t *testing.T) {
	s, id := captureStore(t)
	a := original("1", "live text")
	disabled := a
	disabled.GroupID = "two@g.us"
	r := mustApply(t, s, id, a, disabled)
	if r.Stored != 1 || r.Skipped != 1 {
		t.Fatalf("bad filtering %+v", r)
	}
	a.Source = "history"
	a.Text = "older historical duplicate"
	r = mustApply(t, s, id, a)
	if r.Stored != 0 {
		t.Fatal("duplicate was stored")
	}
	p := page(t, s, id, MessageQuery{})
	if len(p.Items) != 1 || p.Items[0].Text != "live text" || !p.Items[0].Timestamp.Equal(a.Timestamp) {
		t.Fatalf("dedup failed %+v", p)
	}
	if err := s.SetEnabled(context.Background(), id, []string{"one@g.us"}, false); err != nil {
		t.Fatal(err)
	}
	if r = mustApply(t, s, id, original("2", "already queued")); r.Stored != 0 {
		t.Fatal("disabled group accepted queued write")
	}
}
func TestOutOfOrderEditsAndDeletionCannotResurrect(t *testing.T) {
	s, id := captureStore(t)
	ctx := context.Background()
	stamp := time.Now()
	edit := original("edited", "new text")
	edit.Kind = "edit"
	edit.MutationAt = stamp
	mustApply(t, s, id, edit)
	if len(page(t, s, id, MessageQuery{}).Items) != 0 {
		t.Fatal("pending edit presented as original")
	}
	mustApply(t, s, id, original("edited", "old base"))
	older := edit
	older.MutationAt = stamp.Add(-time.Hour)
	older.Text = "stale edit"
	mustApply(t, s, id, older)
	p := page(t, s, id, MessageQuery{})
	if len(p.Items) != 1 || p.Items[0].Text != "new text" || p.Items[0].EditedAt == nil {
		t.Fatal("old delivery overwrote edit")
	}
	del := original("deleted", "must not survive")
	del.Kind = "delete"
	del.MutationAt = stamp
	mustApply(t, s, id, del)
	base := original("deleted", "secret revoked body")
	base.Media = `{"filename":"secret"}`
	mustApply(t, s, id, base)
	lateEdit := base
	lateEdit.Kind = "edit"
	lateEdit.MutationAt = stamp.Add(time.Hour)
	mustApply(t, s, id, lateEdit)
	p = page(t, s, id, MessageQuery{})
	for _, m := range p.Items {
		if m.MessageID == "deleted" && (m.DeletedAt == nil || m.Text != "" || m.Media != "") {
			t.Fatalf("revoke resurrected %+v", m)
		}
	}
	p = page(t, s, id, MessageQuery{Search: "secret"})
	if len(p.Items) != 0 {
		t.Fatal("revoked text remains searchable")
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE messages SET received_at=0 WHERE message_id='deleted'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	mustApply(t, s, id, base)
	if len(page(t, s, id, MessageQuery{Search: "secret"}).Items) != 0 {
		t.Fatal("old tombstone cleanup allowed resurrection")
	}
}
func TestKeysetPaginationSearchExpiryAndPurge(t *testing.T) {
	s, id := captureStore(t)
	ctx := context.Background()
	mustApply(t, s, id, original("1", "searchable alpha"), original("2", "searchable beta"), original("3", "gamma"))
	first := page(t, s, id, MessageQuery{Limit: 2})
	if len(first.Items) != 2 || first.Next.ID == 0 {
		t.Fatal("missing page cursor")
	}
	mustApply(t, s, id, original("4", "arrives while browsing"))
	second := page(t, s, id, MessageQuery{Limit: 2, Before: first.Next})
	if len(second.Items) != 1 || second.Items[0].MessageID != "1" {
		t.Fatalf("unstable tied-timestamp page %+v", second)
	}
	if len(page(t, s, id, MessageQuery{Search: "searchable"}).Items) != 2 {
		t.Fatal("FTS search failed")
	}
	expired := original("expire", "ephemeral unique")
	future := time.Now().Add(time.Hour)
	expired.ExpiresAt = &future
	mustApply(t, s, id, expired)
	if _, err := s.db.ExecContext(ctx, "UPDATE messages SET expires_at=1 WHERE message_id='expire'"); err != nil {
		t.Fatal(err)
	}
	if len(page(t, s, id, MessageQuery{Search: "ephemeral"}).Items) != 0 {
		t.Fatal("expired body visible before maintenance")
	}
	if err := s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := s.db.QueryRow("SELECT text_content FROM messages WHERE message_id='expire'").Scan(&text); err != nil || text != "" {
		t.Fatal("expiry did not clear stored body")
	}
	if err := s.PurgeGroup(ctx, id, "one@g.us"); err != nil {
		t.Fatal(err)
	}
	if len(page(t, s, id, MessageQuery{}).Items) != 0 {
		t.Fatal("purge retained records")
	}
	if r := mustApply(t, s, id, original("5", "late")); r.Stored != 0 {
		t.Fatal("purge did not disable capture")
	}
}
func TestMigrationFromPhaseOnePreservesPreferences(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "archive.db")
	db, err := sql.Open("sqlite3", DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(migration1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(id,jid,created_at) VALUES(1,'account',1);INSERT INTO app_state VALUES(1,1);INSERT INTO groups VALUES(1,'one@g.us','Existing',1,1,1,1);PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	enabled, err := s.IsEnabled(ctx, 1, "one@g.us")
	if err != nil || !enabled {
		t.Fatal("migration changed existing preference")
	}
	mustApply(t, s, 1, original("1", "after migration"))
	if len(page(t, s, 1, MessageQuery{}).Items) != 1 {
		t.Fatal("upgraded archive cannot capture")
	}
}
func TestCaptureRunAndHistoryRecovery(t *testing.T) {
	s, id := captureStore(t)
	ctx := context.Background()
	run, err := s.StartCapture(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := s.BeginHistory(ctx, id, "RECENT", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EndCapture(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartCapture(ctx, id); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = s.db.QueryRow("SELECT status FROM history_batches WHERE id=?", batch).Scan(&status); err != nil || status != "interrupted" {
		t.Fatal("interrupted history processing not recorded")
	}
	var gaps int
	if err = s.db.QueryRow("SELECT count(*) FROM sync_gaps WHERE account_id=?", id).Scan(&gaps); err != nil || gaps != 1 {
		t.Fatal("downtime was not marked uncertain")
	}
}
