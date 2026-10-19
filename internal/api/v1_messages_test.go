package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"whatsappsync/internal/agentauth"
	"whatsappsync/internal/golden"
	"whatsappsync/internal/msgref"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

type v1Body struct {
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data"`
	Page      *query.V1Page   `json:"page"`
	ErrorCode string          `json:"error_code"`
	Error     string          `json:"error"`
	Help      string          `json:"help"`
	Details   json.RawMessage `json:"details"`
}

func (f fixture) get(t *testing.T, path, token string, status int) v1Body {
	t.Helper()
	res, raw := f.v1(t, "GET", path, token)
	var body v1Body
	if err := json.Unmarshal(raw, &body); err != nil || res.StatusCode != status || body.RequestID != res.Header.Get("X-Request-Id") {
		t.Fatalf("GET %s: %d (want %d) %v %s", path, res.StatusCode, status, err, raw)
	}
	return body
}

func (f fixture) grant(t *testing.T, name string, scopes []string, chats ...string) string {
	t.Helper()
	credential, _, err := agentauth.Grant(f.dir, agentauth.GrantRequest{Name: name, Scopes: scopes, Chats: chats, RatePerMinute: agentauth.MaxRatePerMinute}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

func decodeInto[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return v
}

type v1List[T any] struct {
	AccountID int64 `json:"account_id"`
	Items     []T   `json:"items"`
}

var contextBase = time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

// seedContext adds ctx@g.us (m1..m8, several sharing one timestamp, m6
// revoked, m7 expired) and other@g.us (o1, o2 at the hit's timestamp).
func seedContext(t *testing.T, store *storage.Store, account int64) {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveGroups(ctx, account, []storage.Group{{ID: "ctx@g.us", Name: "Context"}, {ID: "other@g.us", Name: "Other"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEnabled(ctx, account, []string{"ctx@g.us", "other@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(300 * time.Millisecond)
	for _, m := range []struct {
		chat, id, text string
		offset         int
		expires        *time.Time
	}{
		{"ctx@g.us", "m1", "first", 0, nil}, {"ctx@g.us", "m2", "second", 1, nil}, {"ctx@g.us", "m3", "third", 1, nil},
		{"ctx@g.us", "m4", "the needle", 1, nil}, {"ctx@g.us", "m5", "fifth", 1, nil}, {"ctx@g.us", "m6", "sixth", 2, nil},
		{"ctx@g.us", "m7", "expiring needle", 3, &expires}, {"ctx@g.us", "m8", "eighth", 4, nil},
		{"other@g.us", "o1", "other needle", 1, nil}, {"other@g.us", "o2", "other noise", 1, nil},
	} {
		if _, err := store.Apply(ctx, account, []storage.Message{{GroupID: m.chat, MessageID: m.id, SenderID: "carol@lid", Timestamp: contextBase.Add(time.Duration(m.offset) * time.Minute), Type: "text", Text: m.text, Source: "live", ExpiresAt: m.expires}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(ctx, account, []storage.Message{{GroupID: "ctx@g.us", MessageID: "m6", Kind: "delete", MutationAt: contextBase.Add(time.Hour), Source: "live"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(expires) + 50*time.Millisecond)
}

func ids(ms []query.V1Message) string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.MessageID)
	}
	return strings.Join(out, ",")
}

func TestV1RoutesRequireAgentCredential(t *testing.T) {
	f := setup(t)
	ref := msgref.Encode("test@lid", "one@g.us", "a")
	for _, path := range []string{"/api/v1/chats", "/api/v1/chats/one@g.us", "/api/v1/chats/one@g.us/messages", "/api/v1/messages/search?q=hello", "/api/v1/messages/" + ref, "/api/v1/messages/" + ref + "/thread"} {
		for _, token := range []string{"", "wrong", f.token} {
			if body := f.get(t, path, token, 401); body.ErrorCode != query.CodeUnauthorized || strings.Contains(string(body.Data), "hello archive") {
				t.Fatalf("%s %q: %+v", path, token, body)
			}
		}
		res, raw := f.v1(t, "POST", path, f.agent)
		if res.StatusCode != 405 || res.Header.Get("Allow") != "GET, HEAD" || strings.Contains(string(raw), "hello archive") {
			t.Fatalf("POST %s: %d %s", path, res.StatusCode, raw)
		}
	}
}

func TestV1ChatsListMatchAndDetail(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.store.SaveGroups(ctx, f.account, []storage.Group{{ID: "team-a@g.us", Name: "Team Alpha"}, {ID: "team-b@g.us", Name: "team beta"}, {ID: "dteam@g.us", Name: "Dream Team"}, {ID: "49123@s.whatsapp.net", Name: "Bob"}}); err != nil {
		t.Fatal(err)
	}
	names := func(path string) string {
		t.Helper()
		list := decodeInto[v1List[query.Group]](t, f.get(t, path, f.agent, 200).Data)
		if list.AccountID != f.account {
			t.Fatalf("%s account %d", path, list.AccountID)
		}
		out := []string{}
		for _, g := range list.Items {
			out = append(out, g.ID)
		}
		return strings.Join(out, ",")
	}
	for path, want := range map[string]string{
		"/api/v1/chats":                                          "one@g.us,two@g.us,49123@s.whatsapp.net,dteam@g.us,team-a@g.us,team-b@g.us",
		"/api/v1/chats?enabled=true":                             "one@g.us,two@g.us",
		"/api/v1/chats?match=two@g.us":                           "two@g.us",    // tier 1: exact JID
		"/api/v1/chats?match=" + url.QueryEscape(" TEAM ALPHA "): "team-a@g.us", // tier 2: name, case-insensitive
		"/api/v1/chats?match=dream":                              "dteam@g.us",  // tier 3: prefix
		"/api/v1/chats?match=ECON":                               "two@g.us",    // tier 4: substring
		"/api/v1/chats?match=bob":                                "49123@s.whatsapp.net",
		"/api/v1/chats?match=nothing":                            "",
		"/api/v1/chats?match=team&enabled=true":                  "",
		"/api/v1/chats?match=first&enabled=true":                 "one@g.us",
	} {
		if got := names(path); got != want {
			t.Errorf("%s: %q want %q", path, got, want)
		}
	}
	// "team" is a prefix of two names, so the substring match "Dream Team" is not considered.
	body := f.get(t, "/api/v1/chats?match=team", f.agent, 409)
	amb := decodeInto[query.ChatAmbiguity](t, body.Details)
	if body.ErrorCode != query.CodeChatAmbiguous || body.Help != query.ChatAmbiguousHelp || amb.Match != "team" || len(amb.Candidates) != 2 || amb.Candidates[0] != (query.ChatCandidate{ID: "team-a@g.us", Name: "Team Alpha", Kind: "group"}) || amb.Candidates[1].ID != "team-b@g.us" {
		t.Fatalf("ambiguity %+v %s", body, body.Details)
	}
	for _, path := range []string{"/api/v1/chats?enabled=maybe", "/api/v1/chats?match=", "/api/v1/chats?match=%20", "/api/v1/chats?match=" + strings.Repeat("x", 257)} {
		if f.get(t, path, f.agent, 400).ErrorCode != query.CodeInvalidArgument {
			t.Fatal(path)
		}
	}
	g := decodeInto[query.Group](t, f.get(t, "/api/v1/chats/one@g.us", f.agent, 200).Data)
	if g.ID != "one@g.us" || g.Name != "First" || g.Kind != "group" || g.MessageCount != 2 {
		t.Fatalf("detail %+v", g)
	}
	if body := f.get(t, "/api/v1/chats/missing@g.us", f.agent, 404); body.ErrorCode != query.CodeNotFound {
		t.Fatalf("%+v", body)
	}
}

func TestV1ChatMessagesPagesAndFilters(t *testing.T) {
	f := setup(t)
	body := f.get(t, "/api/v1/chats/one@g.us/messages?limit=1", f.agent, 200)
	list := decodeInto[v1List[query.V1Message]](t, body.Data)
	if len(list.Items) != 1 || body.Page == nil || !body.Page.HasMore || body.Page.NextCursor == "" || list.Items[0].MessageID != "b" || list.Items[0].ChatKind != "group" || list.Items[0].Ref != msgref.Encode("test@lid", "one@g.us", "b") || list.Items[0].Text != "hello archive b" || list.Items[0].AccountID != f.account {
		t.Fatalf("first page %s %+v", body.Data, body.Page)
	}
	next := body.Page.NextCursor
	body = f.get(t, "/api/v1/chats/one@g.us/messages?limit=1&cursor="+url.QueryEscape(next), f.agent, 200)
	if list = decodeInto[v1List[query.V1Message]](t, body.Data); ids(list.Items) != "a" || body.Page.HasMore || body.Page.NextCursor != "" {
		t.Fatalf("second page %s %+v", body.Data, body.Page)
	}
	if list = decodeInto[v1List[query.V1Message]](t, f.get(t, "/api/v1/chats/one@g.us/messages?sender_id=bob@lid", f.agent, 200).Data); ids(list.Items) != "a" {
		t.Fatalf("sender filter %+v", list)
	}
	if list = decodeInto[v1List[query.V1Message]](t, f.get(t, "/api/v1/chats/one@g.us/messages?to=2026-10-01T12:00:00Z", f.agent, 200).Data); len(list.Items) != 0 {
		t.Fatalf("to filter %+v", list)
	}
	// Legacy and v1 cursors are not interchangeable, and filters are bound.
	var legacy struct {
		Next string `json:"next_cursor"`
	}
	if err := json.Unmarshal(f.request(t, "GET", "/api/chats/one@g.us/messages?limit=1", f.token, 200), &legacy); err != nil || legacy.Next == "" {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/chats/one@g.us/messages?limit=1&cursor=" + url.QueryEscape(legacy.Next),
		"/api/v1/chats/one@g.us/messages?limit=1&sender_id=bob@lid&cursor=" + url.QueryEscape(next),
		"/api/v1/messages/search?q=hello&chat=one@g.us&cursor=" + url.QueryEscape(next),
		"/api/v1/chats/one@g.us/messages?cursor=garbage",
	} {
		if body := f.get(t, path, f.agent, 400); body.ErrorCode != query.CodeInvalidCursor {
			t.Fatalf("%s: %+v", path, body)
		}
	}
	f.request(t, "GET", "/api/chats/one@g.us/messages?cursor="+url.QueryEscape(next), f.token, 400)
	for _, path := range []string{"limit=0", "limit=201", "limit=x", "from=yesterday", "from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z"} {
		if body := f.get(t, "/api/v1/chats/one@g.us/messages?"+path, f.agent, 400); body.ErrorCode != query.CodeInvalidArgument {
			t.Fatalf("%s: %+v", path, body)
		}
	}
	if body := f.get(t, "/api/v1/chats/missing@g.us/messages", f.agent, 404); body.ErrorCode != query.CodeNotFound {
		t.Fatalf("%+v", body)
	}
}

func TestV1ScopesAndAllowlist(t *testing.T) {
	f := setup(t)
	ref := msgref.Encode("test@lid", "two@g.us", "c")
	chatsOnly := f.grant(t, "chats-only", []string{agentauth.ScopeReadChats})
	for _, path := range []string{"/api/v1/chats/one@g.us/messages", "/api/v1/messages/search?q=hello", "/api/v1/messages/" + ref, "/api/v1/messages/" + ref + "/thread"} {
		body := f.get(t, path, chatsOnly, 403)
		if body.ErrorCode != query.CodeForbidden || !strings.Contains(body.Help, "read.messages") || !strings.Contains(body.Help, "wasync agents grant chats-only") || strings.Contains(string(body.Data), "hello") {
			t.Fatalf("%s: %+v", path, body)
		}
	}
	f.get(t, "/api/v1/chats", chatsOnly, 200)
	messagesOnly := f.grant(t, "messages-only", []string{agentauth.ScopeReadMessages})
	if body := f.get(t, "/api/v1/chats", messagesOnly, 403); !strings.Contains(body.Help, "read.chats") {
		t.Fatalf("%+v", body)
	}

	scoped := f.grant(t, "scoped", nil, "one@g.us")
	for _, path := range []string{"/api/v1/chats/two@g.us", "/api/v1/chats/two@g.us/messages", "/api/v1/messages/search?q=hello&chat=two@g.us", "/api/v1/messages/" + ref, "/api/v1/messages/" + ref + "/thread"} {
		body := f.get(t, path, scoped, 403)
		if body.ErrorCode != query.CodeForbidden || !strings.Contains(body.Help, "--chat two@g.us") || strings.Contains(string(body.Data), "hello archive c") {
			t.Fatalf("%s: %+v", path, body)
		}
	}
	chats := decodeInto[v1List[query.Group]](t, f.get(t, "/api/v1/chats", scoped, 200).Data)
	if len(chats.Items) != 1 || chats.Items[0].ID != "one@g.us" {
		t.Fatalf("list not filtered %+v", chats)
	}
	// Matching runs on visible chats only: "s" would pick Second by prefix.
	if chats = decodeInto[v1List[query.Group]](t, f.get(t, "/api/v1/chats?match=s", f.agent, 200).Data); chats.Items[0].ID != "two@g.us" {
		t.Fatalf("unscoped match %+v", chats)
	}
	if chats = decodeInto[v1List[query.Group]](t, f.get(t, "/api/v1/chats?match=s", scoped, 200).Data); len(chats.Items) != 1 || chats.Items[0].ID != "one@g.us" {
		t.Fatalf("scoped match %+v", chats)
	}
	if chats = decodeInto[v1List[query.Group]](t, f.get(t, "/api/v1/chats?match=two@g.us", scoped, 200).Data); len(chats.Items) != 0 {
		t.Fatalf("hidden chat matched %+v", chats)
	}
	body := f.get(t, "/api/v1/messages/search?q=hello", scoped, 200)
	if hits := decodeInto[v1List[query.V1Message]](t, body.Data); ids(hits.Items) != "b,a" || strings.Contains(string(body.Data), "two@g.us") {
		t.Fatalf("search not filtered %s", body.Data)
	}
	if hits := decodeInto[v1List[query.V1Message]](t, f.get(t, "/api/v1/messages/search?q=hello", f.agent, 200).Data); ids(hits.Items) != "c,b,a" {
		t.Fatalf("unscoped search %+v", hits)
	}
	f.get(t, "/api/v1/messages/"+msgref.Encode("test@lid", "one@g.us", "a")+"/thread", scoped, 200)
}

func TestV1SearchContextRefAndThread(t *testing.T) {
	f := setup(t)
	seedContext(t, f.store, f.account)
	type hit struct {
		query.V1Message
		Before []query.V1Message `json:"before"`
		After  []query.V1Message `json:"after"`
	}
	search := func(params string) []hit {
		t.Helper()
		return decodeInto[v1List[hit]](t, f.get(t, "/api/v1/messages/search?"+params, f.agent, 200).Data).Items
	}
	// Equal timestamps are ordered by row id; m6 is a tombstone; m7 expired.
	hits := search("q=needle&chat=ctx@g.us&context=2")
	if len(hits) != 1 || hits[0].MessageID != "m4" || ids(hits[0].Before) != "m2,m3" || ids(hits[0].After) != "m5,m6" {
		t.Fatalf("context 2 %+v", hits)
	}
	if m6 := hits[0].After[1]; m6.Text != "" || m6.DeletedAt == nil || m6.Ref != msgref.Encode("test@lid", "ctx@g.us", "m6") {
		t.Fatalf("tombstone %+v", m6)
	}
	hits = search("q=needle&context=5")
	if len(hits) != 2 || hits[0].MessageID != "o1" || ids(hits[0].Before) != "" || ids(hits[0].After) != "o2" || hits[1].MessageID != "m4" || ids(hits[1].Before) != "m1,m2,m3" || ids(hits[1].After) != "m5,m6,m8" {
		t.Fatalf("context 5 %+v", hits)
	}
	res, raw := f.v1(t, "GET", "/api/v1/messages/search?q=needle&chat=ctx@g.us", f.agent)
	if res.StatusCode != 200 || strings.Contains(string(raw), `"before"`) || !strings.Contains(string(raw), `"ref":"m1_`) {
		t.Fatalf("no context %s", raw)
	}
	if hits = search("q=expiring"); len(hits) != 0 {
		t.Fatalf("expired content returned %+v", hits)
	}
	for _, path := range []string{"q=", "q=needle&context=6", "q=needle&context=-1", "q=" + strings.Repeat("x", 1025)} {
		if body := f.get(t, "/api/v1/messages/search?"+path, f.agent, 400); body.ErrorCode != query.CodeInvalidArgument {
			t.Fatalf("%s %+v", path, body)
		}
	}
	if body := f.get(t, "/api/v1/messages/search?q=needle&chat=missing@g.us", f.agent, 404); body.ErrorCode != query.CodeNotFound {
		t.Fatalf("%+v", body)
	}

	// Round trip: search hit ref -> message -> thread anchor.
	anchor := search("q=needle&chat=ctx@g.us")[0].V1Message
	msg := decodeInto[query.V1Message](t, f.get(t, "/api/v1/messages/"+anchor.Ref, f.agent, 200).Data)
	want, _ := json.Marshal(anchor)
	if got, _ := json.Marshal(msg); string(got) != string(want) {
		t.Fatalf("message %s != hit %s", got, want)
	}
	thread := decodeInto[query.V1Thread](t, f.get(t, "/api/v1/messages/"+anchor.Ref+"/thread", f.agent, 200).Data)
	if got, _ := json.Marshal(thread.Anchor); string(got) != string(want) || ids(thread.Before) != "m1,m2,m3" || ids(thread.After) != "m5,m6,m8" {
		t.Fatalf("thread %+v", thread)
	}
	if thread = decodeInto[query.V1Thread](t, f.get(t, "/api/v1/messages/"+anchor.Ref+"/thread?before=1&after=0", f.agent, 200).Data); ids(thread.Before) != "m3" || thread.After == nil || len(thread.After) != 0 {
		t.Fatalf("bounded thread %+v", thread)
	}
	m6 := msgref.Encode("test@lid", "ctx@g.us", "m6")
	if thread = decodeInto[query.V1Thread](t, f.get(t, "/api/v1/messages/"+m6+"/thread?before=20&after=20", f.agent, 200).Data); thread.Anchor.DeletedAt == nil || ids(thread.Before) != "m1,m2,m3,m4,m5" || ids(thread.After) != "m8" {
		t.Fatalf("tombstone thread %+v", thread)
	}
	for _, p := range []string{"before=21", "after=-1", "before=x"} {
		if body := f.get(t, "/api/v1/messages/"+anchor.Ref+"/thread?"+p, f.agent, 400); body.ErrorCode != query.CodeInvalidArgument {
			t.Fatalf("%s %+v", p, body)
		}
	}
	for ref, status := range map[string]int{
		msgref.Encode("test@lid", "ctx@g.us", "m7"):    404, // expired
		msgref.Encode("other@lid", "ctx@g.us", "m4"):   404, // other account
		msgref.Encode("test@lid", "ctx@g.us", "nope"):  404,
		msgref.Encode("test@lid", "missing@g.us", "x"): 404,
		"m1_not*base64": 400, "42": 400, anchor.Ref + "=": 400,
	} {
		for _, suffix := range []string{"", "/thread"} {
			body := f.get(t, "/api/v1/messages/"+url.PathEscape(ref)+suffix, f.agent, status)
			if code := map[int]string{404: query.CodeNotFound, 400: query.CodeInvalidArgument}[status]; body.ErrorCode != code || strings.Contains(string(body.Data), "needle") {
				t.Fatalf("%s%s: %+v", ref, suffix, body)
			}
		}
	}
}

// TestV1MessageSnapshots freezes the chats, messages, search, message and thread envelopes.
func TestV1MessageSnapshots(t *testing.T) {
	f := setup(t)
	seedContext(t, f.store, f.account)
	if err := f.store.SaveGroups(context.Background(), f.account, []storage.Group{{ID: "team-a@g.us", Name: "Team A"}, {ID: "team-b@g.us", Name: "Team B"}}); err != nil {
		t.Fatal(err)
	}
	ref := msgref.Encode("test@lid", "ctx@g.us", "m4")
	scoped := f.grant(t, "scoped", nil, "one@g.us")
	chatsOnly := f.grant(t, "chats-only", []string{agentauth.ScopeReadChats})
	snapshots := []any{}
	for _, c := range []struct{ path, token, auth string }{
		{"/api/v1/chats?enabled=true", f.agent, "agent"},
		{"/api/v1/chats?match=context", f.agent, "agent"},
		{"/api/v1/chats?match=team", f.agent, "agent"},
		{"/api/v1/chats/ctx@g.us", f.agent, "agent"},
		{"/api/v1/chats/ctx@g.us/messages?limit=2", f.agent, "agent"},
		{"/api/v1/messages/search?q=needle&context=1", f.agent, "agent"},
		{"/api/v1/messages/search?q=hello", scoped, "agent --chat one@g.us"},
		{"/api/v1/messages/" + ref, f.agent, "agent"},
		{"/api/v1/messages/" + ref + "/thread?before=2&after=2", f.agent, "agent"},
		{"/api/v1/messages/m1_bad", f.agent, "agent"},
		{"/api/v1/messages/" + msgref.Encode("test@lid", "ctx@g.us", "m7"), f.agent, "agent"},
		{"/api/v1/chats/two@g.us/messages", scoped, "agent --chat one@g.us"},
		{"/api/v1/messages/" + ref, chatsOnly, "agent --scope read.chats"},
	} {
		res, body := f.v1(t, "GET", c.path, c.token)
		snapshots = append(snapshots, map[string]any{"request": "GET " + c.path, "auth": c.auth, "status": res.StatusCode, "body": golden.Redact(golden.Decode(t, body), "request_id", "next_cursor", "received_at")})
	}
	golden.Compare(t, "v1/messages.json", snapshots)
}
