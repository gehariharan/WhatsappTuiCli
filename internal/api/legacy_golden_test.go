package api

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"whatsappsync/internal/golden"
	"whatsappsync/internal/storage"
)

// TestLegacyAPISnapshots freezes every existing /api/* and /health response
// shape. New API versions must be added beside these routes, never by changing them.
func TestLegacyAPISnapshots(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	stamp := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := f.store.SaveDiscoveredChat(ctx, f.account, storage.Group{ID: "15550001111@s.whatsapp.net", Name: "Direct"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetEnabled(ctx, f.account, []string{"15550001111@s.whatsapp.net"}, true); err != nil {
		t.Fatal(err)
	}
	for _, m := range []storage.Message{
		{GroupID: "one@g.us", MessageID: "reply", SenderID: "alice@lid", SenderAlt: "15550002222@s.whatsapp.net", Timestamp: stamp.Add(time.Minute), Type: "image", Text: "caption", ReplyTo: "a", Media: `{"mime":"image/jpeg","size":42}`, Source: "live"},
		{GroupID: "one@g.us", MessageID: "edited", SenderID: "alice@lid", Timestamp: stamp.Add(2 * time.Minute), Type: "text", Text: "before edit", Source: "live"},
		{GroupID: "one@g.us", MessageID: "edited", Kind: "edit", MutationAt: stamp.Add(3 * time.Minute), Type: "text", Text: "after edit", Source: "live"},
		{GroupID: "one@g.us", MessageID: "revoked", SenderID: "bob@lid", Timestamp: stamp.Add(4 * time.Minute), Type: "text", Text: "gone", Source: "history"},
		{GroupID: "one@g.us", MessageID: "revoked", Kind: "delete", MutationAt: stamp.Add(5 * time.Minute), Source: "live"},
		{GroupID: "15550001111@s.whatsapp.net", MessageID: "dm", SenderID: "15550001111@s.whatsapp.net", Timestamp: stamp.Add(6 * time.Minute), Type: "text", Text: "direct hello", FromMe: true, Source: "live"},
	} {
		if _, err := f.store.Apply(ctx, f.account, []storage.Message{m}); err != nil {
			t.Fatal(err)
		}
	}
	type call struct{ method, path, token string }
	read, none := f.token, ""
	calls := []call{
		{"GET", "/health", none},
		{"GET", "/api/status", read},
		{"GET", "/api/groups", read},
		{"GET", "/api/chats", read},
		{"GET", "/api/groups/one@g.us", read},
		{"GET", "/api/chats/15550001111@s.whatsapp.net", read},
		{"GET", "/api/groups/15550001111@s.whatsapp.net", read},
		{"GET", "/api/chats/one@g.us/messages", read},
		{"GET", "/api/groups/one@g.us/messages?limit=1", read},
		{"GET", "/api/chats/one@g.us/messages?sender_id=alice@lid&from=2026-10-01T12:00:00Z&to=2026-10-01T12:03:00Z", read},
		{"GET", "/api/messages/search?q=hello", read},
		{"GET", "/api/messages/search?q=archive&group_id=two@g.us", read},
		{"GET", "/api/messages/1", read},
		{"GET", "/api/status", none},
		{"GET", "/api/status", "wrong"},
		{"GET", "/api/messages/abc", read},
		{"GET", "/api/messages/999", read},
		{"GET", "/api/chats/missing@g.us", read},
		{"GET", "/api/messages/search", read},
		{"GET", "/api/chats/one@g.us/messages?limit=500", read},
		{"GET", "/api/chats/one@g.us/messages?from=yesterday", read},
		{"GET", "/api/chats/one@g.us/messages?cursor=garbage", read},
		{"POST", "/api/send", read},
		{"POST", "/api/sync", read},
		{"POST", "/api/media/policy", read},
		{"POST", "/api/actions", read},
		{"POST", "/api/actions/00000000000000000000000000000000/approve", read},
		{"POST", "/api/chats/two@g.us/disable", read},
		{"POST", "/api/groups/two@g.us/enable", read},
		{"POST", "/api/chats/one@g.us/history", read},
		{"POST", "/api/chats/disable-all", read},
		{"POST", "/api/groups/enable-all", read},
	}
	snapshots := make([]any, 0, len(calls))
	for _, c := range calls {
		req, err := http.NewRequest(c.method, f.server.URL+c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		auth := "none"
		if c.token == read {
			auth = "api.token"
		} else if c.token != "" {
			auth = "invalid"
		}
		snapshots = append(snapshots, map[string]any{
			"request":          c.method + " " + c.path,
			"auth":             auth,
			"status":           res.StatusCode,
			"content_type":     res.Header.Get("Content-Type"),
			"www_authenticate": res.Header.Get("WWW-Authenticate"),
			"body":             golden.Redact(golden.Decode(t, body), "received_at", "next_cursor"),
		})
	}
	golden.Compare(t, "legacy_api.json", snapshots)
}
