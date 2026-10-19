package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"whatsappsync/internal/agentauth"
	"whatsappsync/internal/changecursor"
	"whatsappsync/internal/msgref"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

func (f fixture) grantObserver(t *testing.T, name string, chats ...string) string {
	t.Helper()
	credential, _, err := agentauth.Grant(f.dir, agentauth.GrantRequest{Name: name, Scopes: []string{agentauth.ScopeObserve}, Chats: chats, RatePerMinute: agentauth.MaxRatePerMinute}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

func (f fixture) send(t *testing.T, method, path, token, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, data
}

type changesResponse struct {
	RequestID string          `json:"request_id"`
	Data      query.V1Changes `json:"data"`
	Page      *query.V1Page   `json:"page"`
}

func (f fixture) changes(t *testing.T, token, params string) changesResponse {
	t.Helper()
	status, body := f.send(t, "GET", "/api/v1/changes"+params, token, "")
	var got changesResponse
	if status != 200 || json.Unmarshal(body, &got) != nil || got.Page == nil || !requestIDPattern.MatchString(got.RequestID) {
		t.Fatalf("changes%s: %d %s", params, status, body)
	}
	if strings.Contains(string(body), "hello archive") {
		t.Fatalf("changes leaked content: %s", body)
	}
	return got
}

func wantV1Error(t *testing.T, status int, body []byte, wantStatus int, code string) query.V1Error {
	t.Helper()
	var e query.V1Error
	if status != wantStatus || json.Unmarshal(body, &e) != nil || e.ErrorCode != code || !requestIDPattern.MatchString(e.RequestID) {
		t.Fatalf("want %d %s, got %d %s", wantStatus, code, status, body)
	}
	return e
}

func TestV1ChangesFeed(t *testing.T) {
	f := setup(t)
	observer := f.grantObserver(t, "observer")
	got := f.changes(t, observer, "")
	type op struct{ op, chat, message string }
	var ops []op
	for i, c := range got.Data.Changes {
		ops = append(ops, op{c.Op, c.ChatID, c.MessageID})
		cursor, err := changecursor.Decode(c.Cursor)
		if err != nil || cursor.Account != "test@lid" || cursor.Sequence != c.Sequence || c.OpVersion != 1 || c.CommittedAt.IsZero() || c.CommittedAt.Location() != time.UTC {
			t.Fatalf("change %d: %+v %v", i, c, err)
		}
		if strings.HasPrefix(c.Op, "message.") {
			ref, err := msgref.Decode(c.Ref)
			if err != nil || ref != (msgref.Ref{Account: "test@lid", Chat: c.ChatID, Message: c.MessageID}) {
				t.Fatalf("ref %+v %v", c, err)
			}
		} else if c.Ref != "" {
			t.Fatalf("ref on %s", c.Op)
		}
	}
	want := []op{{"capture.enabled", "one@g.us", ""}, {"capture.enabled", "two@g.us", ""}, {"message.created", "one@g.us", "a"}, {"message.created", "one@g.us", "b"}, {"message.created", "two@g.us", "c"}}
	if len(ops) != len(want) {
		t.Fatalf("ops %v", ops)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("ops %v, want %v", ops, want)
		}
	}
	last := got.Data.Changes[len(got.Data.Changes)-1].Cursor
	if got.Page.HasMore || got.Page.NextCursor != last {
		t.Fatalf("page %+v", got.Page)
	}
	// Paging with limit continues exactly where the previous page ended.
	var seen []string
	params := "?limit=2"
	for {
		page := f.changes(t, observer, params)
		for _, c := range page.Data.Changes {
			seen = append(seen, c.MessageID+"/"+c.ChatID)
		}
		if !page.Page.HasMore {
			if page.Page.NextCursor != last {
				t.Fatalf("final cursor %q", page.Page.NextCursor)
			}
			break
		}
		params = "?limit=2&after=" + url.QueryEscape(page.Page.NextCursor)
	}
	if len(seen) != 5 {
		t.Fatalf("paged %v", seen)
	}
	// Polling at the head returns an empty page with the same cursor.
	empty := f.changes(t, observer, "?after="+last)
	if len(empty.Data.Changes) != 0 || empty.Page.NextCursor != last || empty.Page.HasMore {
		t.Fatalf("head page %+v", empty)
	}
	// New mutations appear after the cursor.
	if err := f.store.SetEnabled(context.Background(), f.account, []string{"two@g.us"}, false); err != nil {
		t.Fatal(err)
	}
	next := f.changes(t, observer, "?after="+last)
	if len(next.Data.Changes) != 1 || next.Data.Changes[0].Op != storage.OpCaptureDisabled || next.Data.Changes[0].ChatID != "two@g.us" {
		t.Fatalf("after disable %+v", next.Data)
	}
}

func TestV1ChangesErrors(t *testing.T) {
	f := setup(t)
	observer := f.grantObserver(t, "observer")
	epoch, err := f.store.JournalEpoch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []string{"?limit=0", "?limit=1001", "?limit=x"} {
		status, body := f.send(t, "GET", "/api/v1/changes"+params, observer, "")
		wantV1Error(t, status, body, 400, query.CodeInvalidArgument)
	}
	for _, cursor := range []string{"garbage", "m1_abc", changecursor.Encode(changecursor.Cursor{Account: "test@lid", Epoch: "XYZ", Sequence: 1})} {
		status, body := f.send(t, "GET", "/api/v1/changes?after="+url.QueryEscape(cursor), observer, "")
		if e := wantV1Error(t, status, body, 400, query.CodeInvalidCursor); e.Help == "" {
			t.Fatal("no help")
		}
	}
	for _, c := range []changecursor.Cursor{
		{Account: "other@lid", Epoch: epoch, Sequence: 1},                  // another account
		{Account: "test@lid", Epoch: strings.Repeat("0", 32), Sequence: 1}, // another archive
		{Account: "test@lid", Epoch: epoch, Sequence: 999},                 // ahead of the journal (restored backup)
	} {
		status, body := f.send(t, "GET", "/api/v1/changes?after="+changecursor.Encode(c), observer, "")
		if e := wantV1Error(t, status, body, 410, query.CodeCursorExpired); !strings.Contains(e.Help, "Resnapshot") {
			t.Fatalf("help %q", e.Help)
		}
	}
	// A cursor from a recreated archive (new epoch) is expired, never silently reused.
	other := setup(t)
	cursor := other.changes(t, other.grantObserver(t, "observer"), "").Page.NextCursor
	status, body := f.send(t, "GET", "/api/v1/changes?after="+cursor, observer, "")
	wantV1Error(t, status, body, 410, query.CodeCursorExpired)
	status, body = f.send(t, "PUT", "/api/v1/checkpoints/replica", observer, `{"cursor":"`+cursor+`"}`)
	wantV1Error(t, status, body, 410, query.CodeCursorExpired)
	// Wrong methods keep the v1 envelope.
	status, body = f.send(t, "POST", "/api/v1/changes", observer, "")
	wantV1Error(t, status, body, 405, query.CodeInvalidArgument)
	status, body = f.send(t, "DELETE", "/api/v1/checkpoints/replica", observer, "")
	wantV1Error(t, status, body, 405, query.CodeInvalidArgument)
}

func TestV1ChangesRequireObserveScope(t *testing.T) {
	f := setup(t)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/v1/changes", ""},
		{"GET", "/api/v1/checkpoints/replica", ""},
		{"PUT", "/api/v1/checkpoints/replica", `{"cursor":"x"}`},
	} {
		status, body := f.send(t, c.method, c.path, f.agent, c.body)
		e := wantV1Error(t, status, body, 403, query.CodeForbidden)
		if !strings.Contains(e.Help, "observe") {
			t.Fatalf("help %q", e.Help)
		}
		status, body = f.send(t, c.method, c.path, f.token, c.body)
		wantV1Error(t, status, body, 401, query.CodeUnauthorized)
	}
	if _, found, err := f.store.Checkpoint(context.Background(), f.account, "replica"); err != nil || found {
		t.Fatalf("denied PUT stored a checkpoint: %v %v", found, err)
	}
}

func TestV1ChangesFilterByChatAllowlist(t *testing.T) {
	f := setup(t)
	all := f.changes(t, f.grantObserver(t, "observer"), "")
	limited := f.grantObserver(t, "limited", "one@g.us")
	got := f.changes(t, limited, "")
	if len(got.Data.Changes) != 3 {
		t.Fatalf("filtered %+v", got.Data.Changes)
	}
	for _, c := range got.Data.Changes {
		if c.ChatID != "one@g.us" {
			t.Fatalf("allowlist leaked %+v", c)
		}
	}
	// The cursor still advances past hidden chats.
	if got.Page.NextCursor != all.Page.NextCursor {
		t.Fatalf("cursor %q, want %q", got.Page.NextCursor, all.Page.NextCursor)
	}
	// A page of only hidden changes is empty but moves on.
	page := f.changes(t, limited, "?limit=1&after="+all.Data.Changes[0].Cursor)
	if len(page.Data.Changes) != 0 || !page.Page.HasMore || page.Page.NextCursor != all.Data.Changes[1].Cursor {
		t.Fatalf("hidden page %+v", page)
	}
}

func TestV1Checkpoints(t *testing.T) {
	f := setup(t)
	observer := f.grantObserver(t, "observer")
	read := func(consumer string) query.V1Checkpoint {
		t.Helper()
		status, body := f.send(t, "GET", "/api/v1/checkpoints/"+consumer, observer, "")
		var got struct {
			Data query.V1Checkpoint `json:"data"`
		}
		if status != 200 || json.Unmarshal(body, &got) != nil {
			t.Fatalf("get checkpoint: %d %s", status, body)
		}
		return got.Data
	}
	if cp := read("replica"); cp.Consumer != "replica" || cp.Cursor != nil || cp.UpdatedAt != nil {
		t.Fatalf("unset checkpoint %+v", cp)
	}
	feed := f.changes(t, observer, "")
	middle, last := feed.Data.Changes[2].Cursor, feed.Page.NextCursor
	status, body := f.send(t, "PUT", "/api/v1/checkpoints/replica", observer, `{"cursor":"`+middle+`"}`)
	if status != 200 || !strings.Contains(string(body), middle) {
		t.Fatalf("put: %d %s", status, body)
	}
	if cp := read("replica"); cp.Cursor == nil || *cp.Cursor != middle || cp.UpdatedAt == nil {
		t.Fatalf("stored checkpoint %+v", cp)
	}
	// Resume from the checkpoint.
	if rest := f.changes(t, observer, "?after="+*read("replica").Cursor); len(rest.Data.Changes) != 2 {
		t.Fatalf("resume %+v", rest.Data)
	}
	// Forward and idempotent commits succeed; backwards is refused.
	for _, cursor := range []string{last, last} {
		if status, body = f.send(t, "PUT", "/api/v1/checkpoints/replica", observer, `{"cursor":"`+cursor+`"}`); status != 200 {
			t.Fatalf("put %d %s", status, body)
		}
	}
	status, body = f.send(t, "PUT", "/api/v1/checkpoints/replica", observer, `{"cursor":"`+middle+`"}`)
	wantV1Error(t, status, body, 409, query.CodeInvalidArgument)
	if cp := read("replica"); *cp.Cursor != last {
		t.Fatalf("checkpoint moved back to %s", *cp.Cursor)
	}
	for _, c := range []struct{ path, body string }{
		{"/api/v1/checkpoints/Bad_Name", `{"cursor":"` + last + `"}`},
		{"/api/v1/checkpoints/" + strings.Repeat("a", 65), `{"cursor":"` + last + `"}`},
		{"/api/v1/checkpoints/replica", `{}`},
		{"/api/v1/checkpoints/replica", `{"cursor":"` + last + `","extra":1}`},
		{"/api/v1/checkpoints/replica", `{"cursor":"` + last + `"}{}`},
		{"/api/v1/checkpoints/replica", `not json`},
	} {
		status, body = f.send(t, "PUT", c.path, observer, c.body)
		wantV1Error(t, status, body, 400, query.CodeInvalidArgument)
	}
	status, body = f.send(t, "PUT", "/api/v1/checkpoints/replica", observer, `{"cursor":"c1_garbage"}`)
	wantV1Error(t, status, body, 400, query.CodeInvalidCursor)
	status, body = f.send(t, "GET", "/api/v1/checkpoints/Bad_Name", observer, "")
	wantV1Error(t, status, body, 400, query.CodeInvalidArgument)

	// Every commit attempt is audited as a mutation; reads that succeed are not.
	log, err := os.ReadFile(filepath.Join(f.dir, agentauth.AuditFileName))
	if err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		var e agentauth.AuditEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			t.Fatalf("audit line %q", line)
		}
		if e.Scope == agentauth.ScopeObserve && e.Outcome == agentauth.OutcomeAllowed {
			allowed++
		}
	}
	if allowed != 11 {
		t.Fatalf("audited %d allowed observe mutations:\n%s", allowed, log)
	}

	// Checkpoints are scoped to the current account.
	if _, err = f.store.UseAccount(context.Background(), "second@lid"); err != nil {
		t.Fatal(err)
	}
	if cp := read("replica"); cp.Cursor != nil {
		t.Fatalf("checkpoint leaked to another account %+v", cp)
	}
	status, body = f.send(t, "PUT", "/api/v1/checkpoints/replica", observer, `{"cursor":"`+last+`"}`)
	wantV1Error(t, status, body, 410, query.CodeCursorExpired)
	if got := f.changes(t, observer, ""); len(got.Data.Changes) != 0 {
		t.Fatalf("changes leaked to another account %+v", got.Data)
	}
	status, body = f.send(t, "GET", "/api/v1/changes?after="+last, observer, "")
	wantV1Error(t, status, body, 410, query.CodeCursorExpired)
}
