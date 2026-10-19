package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"whatsappsync/internal/agentauth"
	"whatsappsync/internal/api"
	"whatsappsync/internal/golden"
	"whatsappsync/internal/msgref"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

// seedV1 adds chats for matching and a second enabled chat with a revoked
// and an expired message beside the fixture's one@g.us messages 0, 1, 2.
func seedV1(t *testing.T, writer *storage.Store, account int64) {
	t.Helper()
	ctx := context.Background()
	if err := writer.SaveGroups(ctx, account, []storage.Group{{ID: "team-a@g.us", Name: "Team A"}, {ID: "team-b@g.us", Name: "Team B"}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.SetEnabled(ctx, account, []string{"two@g.us"}, true); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	expires := time.Now().Add(200 * time.Millisecond)
	for i, m := range []storage.Message{
		{MessageID: "t0", Text: "hello two"}, {MessageID: "t1", Text: "revoked"}, {MessageID: "t2", Text: "expiring hello", ExpiresAt: &expires}, {MessageID: "t3", Text: "after"},
	} {
		m.GroupID, m.SenderID, m.Type, m.Source, m.Timestamp = "two@g.us", "other@lid", "text", "live", stamp.Add(time.Duration(i)*time.Second)
		if _, err := writer.Apply(ctx, account, []storage.Message{m}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writer.Apply(ctx, account, []storage.Message{{GroupID: "two@g.us", MessageID: "t1", Kind: "delete", MutationAt: stamp.Add(time.Hour), Source: "live"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Until(expires) + 50*time.Millisecond)
}

func queryV1Fail(t *testing.T, a *App, out *bytes.Buffer, dir string, args ...string) query.V1Error {
	t.Helper()
	out.Reset()
	err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
	var e query.V1Error
	if !errors.As(err, new(*ReportedError)) || json.Unmarshal(out.Bytes(), &e) != nil || e.Error != err.Error() || !cliRequestID.MatchString(e.RequestID) {
		t.Fatalf("%v: %v %s", args, err, out.Bytes())
	}
	return e
}

func messageIDs(ms []query.V1Message) string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.MessageID)
	}
	return strings.Join(out, ",")
}

func TestQueryV1ChatsMatch(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	seedV1(t, writer, account)
	var got struct {
		Data query.V1ChatList `json:"data"`
	}
	for match, want := range map[string]string{"two@g.us": "two@g.us", "FIRST": "one@g.us", "team a": "team-a@g.us", "sec": "two@g.us", "irs": "one@g.us", "zzz": ""} {
		if err := json.Unmarshal(queryRun(t, a, out, dir, "chats", "--v1", "--match", match), &got); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, g := range got.Data.Items {
			ids = append(ids, g.ID)
		}
		if strings.Join(ids, ",") != want || got.Data.AccountID != account {
			t.Errorf("%q: %v want %q", match, ids, want)
		}
	}
	e := queryV1Fail(t, a, out, dir, "chats", "--v1", "--match", "Team")
	raw, _ := json.Marshal(e.Details)
	var amb query.ChatAmbiguity
	if e.ErrorCode != query.CodeChatAmbiguous || e.Help != query.ChatAmbiguousHelp || json.Unmarshal(raw, &amb) != nil || len(amb.Candidates) != 2 || amb.Candidates[0].ID != "team-a@g.us" || amb.Candidates[1].Kind != "group" {
		t.Fatalf("ambiguity %+v", e)
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "chats", "--v1", "--enabled", "--match", "team"), &got); err != nil || len(got.Data.Items) != 0 {
		t.Fatalf("enabled match %v %+v", err, got)
	}
	for _, args := range [][]string{{"chats", "--v1", "--match", " "}, {"chats", "--v1", "--match", strings.Repeat("x", 257)}, {"groups", "--v1", "--match", "x"}} {
		if e := queryV1Fail(t, a, out, dir, args...); e.ErrorCode != query.CodeInvalidArgument {
			t.Fatalf("%v: %+v", args, e)
		}
	}
	// Legacy query chats is unchanged and refuses the v1-only flag.
	out.Reset()
	err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "chats", "--match", "First"})
	if err == nil || out.Len() != 0 || !strings.Contains(err.Error(), "--match requires --v1") {
		t.Fatalf("legacy --match: %v %q", err, out.String())
	}
	if data := queryRun(t, a, out, dir, "chats"); bytes.Contains(data, []byte("request_id")) || !bytes.Contains(data, []byte(`"team-b@g.us"`)) {
		t.Fatalf("legacy chats %s", data)
	}
}

func TestQueryV1MessagesSearchMessageThread(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	seedV1(t, writer, account)
	var list struct {
		Data struct {
			Items []query.V1Message `json:"items"`
		} `json:"data"`
		Page query.V1Page `json:"page"`
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "messages", "--chat", "two@g.us", "--v1"), &list); err != nil || messageIDs(list.Data.Items) != "t3,t1,t0" || list.Data.Items[1].DeletedAt == nil || list.Data.Items[1].Text != "" || list.Data.Items[0].Ref != msgref.Encode("test@lid", "two@g.us", "t3") || list.Data.Items[0].ChatKind != "group" {
		t.Fatalf("messages %v %+v", err, list)
	}
	type hit struct {
		query.V1Message
		Before []query.V1Message `json:"before"`
		After  []query.V1Message `json:"after"`
	}
	var hits struct {
		Data struct {
			Items []hit `json:"items"`
		} `json:"data"`
	}
	// Equal timestamps in one@g.us are ordered by row id; context stays in the hit's chat.
	if err := json.Unmarshal(queryRun(t, a, out, dir, "search", "hello", "--v1", "--context", "1"), &hits); err != nil || len(hits.Data.Items) != 4 {
		t.Fatalf("search %v %+v", err, hits)
	}
	for _, h := range hits.Data.Items {
		want := map[string][2]string{"2": {"1", ""}, "1": {"0", "2"}, "0": {"", "1"}, "t0": {"", "t1"}}[h.MessageID]
		if messageIDs(h.Before) != want[0] || messageIDs(h.After) != want[1] {
			t.Errorf("hit %s: before %s after %s", h.MessageID, messageIDs(h.Before), messageIDs(h.After))
		}
	}
	data := queryRun(t, a, out, dir, "search", "hello", "--v1")
	if bytes.Contains(data, []byte(`"before"`)) || !bytes.Contains(data, []byte(`"ref":"m1_`)) || bytes.Contains(data, []byte("expiring")) {
		t.Fatalf("search without context %s", data)
	}
	ref := hits.Data.Items[0].Ref
	var one struct {
		Data query.V1Message `json:"data"`
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "message", "--ref", ref, "--v1"), &one); err != nil || one.Data.Ref != ref || one.Data.MessageID != hits.Data.Items[0].MessageID {
		t.Fatalf("message %v %+v", err, one)
	}
	if data := queryRun(t, a, out, dir, "message", "--ref", ref); bytes.Contains(data, []byte("request_id")) || !bytes.Contains(data, []byte(`"item":`)) {
		t.Fatalf("legacy message changed %s", data)
	}
	var thread struct {
		Data query.V1Thread `json:"data"`
	}
	t1 := msgref.Encode("test@lid", "two@g.us", "t1")
	if err := json.Unmarshal(queryRun(t, a, out, dir, "thread", "--ref", t1), &thread); err != nil || thread.Data.Anchor.DeletedAt == nil || messageIDs(thread.Data.Before) != "t0" || messageIDs(thread.Data.After) != "t3" {
		t.Fatalf("thread %v %+v", err, thread)
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "thread", "--ref", msgref.Encode("test@lid", "one@g.us", "1"), "--before", "0", "--after", "20", "--v1"), &thread); err != nil || len(thread.Data.Before) != 0 || thread.Data.Before == nil || messageIDs(thread.Data.After) != "2" {
		t.Fatalf("bounded thread %v %+v", err, thread)
	}
	for _, tc := range []struct {
		code string
		args []string
	}{
		{query.CodeInvalidArgument, []string{"search", "hello", "--v1", "--context", "6"}},
		{query.CodeInvalidArgument, []string{"search", "hello", "--v1", "--context", "-1"}},
		{query.CodeInvalidArgument, []string{"messages", "--chat", "one@g.us", "--v1", "--context", "1"}},
		{query.CodeInvalidArgument, []string{"thread"}},
		{query.CodeInvalidArgument, []string{"thread", "--ref", "m1_bad*"}},
		{query.CodeInvalidArgument, []string{"thread", "--ref", t1, "--before", "21"}},
		{query.CodeInvalidArgument, []string{"thread", "--ref", t1, "extra"}},
		{query.CodeNotFound, []string{"thread", "--ref", msgref.Encode("test@lid", "two@g.us", "t2")}},
		{query.CodeNotFound, []string{"thread", "--ref", msgref.Encode("other@lid", "two@g.us", "t0")}},
		{query.CodeNotFound, []string{"message", "--ref", msgref.Encode("test@lid", "two@g.us", "t2"), "--v1"}},
		{query.CodeInvalidArgument, []string{"message", "--ref", "1", "--v1"}},
	} {
		if e := queryV1Fail(t, a, out, dir, tc.args...); e.ErrorCode != tc.code {
			t.Fatalf("%v: %+v", tc.args, e)
		}
	}
	out.Reset()
	if err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "search", "hello", "--context", "1"}); err == nil || out.Len() != 0 {
		t.Fatalf("legacy --context: %v %q", err, out.String())
	}
	out.Reset()
	if err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "thread", "--help"}); err != nil || !strings.Contains(out.String(), "query thread --ref REF") {
		t.Fatalf("thread help: %v %q", err, out.String())
	}
	// v1 and legacy cursors are not interchangeable.
	var legacy struct {
		Next string `json:"next_cursor"`
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "messages", "--chat", "one@g.us", "--limit", "1"), &legacy); err != nil || legacy.Next == "" {
		t.Fatal(err)
	}
	if e := queryV1Fail(t, a, out, dir, "messages", "--chat", "one@g.us", "--limit", "1", "--v1", "--cursor", legacy.Next); e.ErrorCode != query.CodeInvalidCursor {
		t.Fatalf("%+v", e)
	}
	if err := json.Unmarshal(queryRun(t, a, out, dir, "messages", "--chat", "one@g.us", "--limit", "1", "--v1"), &list); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "messages", "--chat", "one@g.us", "--limit", "1", "--cursor", list.Page.NextCursor}); err == nil {
		t.Fatal("legacy accepted a v1 cursor")
	}
}

// TestV1TransportsAgree checks that /api/v1 and wasync query --v1 produce the
// same data for the same archive, so AXI can switch transports transparently.
func TestV1TransportsAgree(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	seedV1(t, writer, account)
	server, err := api.Start("127.0.0.1:0", dir, writer, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	credential, _, err := agentauth.Grant(dir, agentauth.GrantRequest{Name: "parity", RatePerMinute: agentauth.MaxRatePerMinute}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ref := msgref.Encode("test@lid", "two@g.us", "t1")
	normalize := func(raw []byte) string {
		t.Helper()
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		delete(v, "request_id")
		if page, ok := v["page"].(map[string]any); ok {
			delete(page, "next_cursor") // signed differently per transport
		}
		if v["error_code"] != nil {
			delete(v, "error") // human text may differ; code, help and details may not
			delete(v, "help")
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	for _, c := range []struct {
		path string
		args []string
	}{
		{"/api/v1/chats", []string{"chats", "--v1"}},
		{"/api/v1/chats?enabled=true", []string{"chats", "--v1", "--enabled"}},
		{"/api/v1/chats?match=first", []string{"chats", "--v1", "--match", "first"}},
		{"/api/v1/chats?match=team", []string{"chats", "--v1", "--match", "team"}},
		{"/api/v1/chats/one@g.us/messages?limit=2", []string{"messages", "--chat", "one@g.us", "--limit", "2", "--v1"}},
		{"/api/v1/chats/two@g.us/messages?sender_id=other@lid&from=2026-10-01T00:00:01Z", []string{"messages", "--chat", "two@g.us", "--sender", "other@lid", "--since", "2026-10-01T00:00:01Z", "--v1"}},
		{"/api/v1/messages/search?q=hello&context=2", []string{"search", "hello", "--context", "2", "--v1"}},
		{"/api/v1/messages/search?q=hello&chat=one@g.us&limit=1", []string{"search", "hello", "--chat", "one@g.us", "--limit", "1", "--v1"}},
		{"/api/v1/messages/" + ref, []string{"message", "--ref", ref, "--v1"}},
		{"/api/v1/messages/" + ref + "/thread", []string{"thread", "--ref", ref}},
		{"/api/v1/messages/" + ref + "/thread?before=0&after=1", []string{"thread", "--ref", ref, "--before", "0", "--after", "1"}},
		{"/api/v1/messages/" + msgref.Encode("test@lid", "two@g.us", "t2"), []string{"message", "--ref", msgref.Encode("test@lid", "two@g.us", "t2"), "--v1"}},
	} {
		req, _ := http.NewRequest("GET", server.URL+c.path, nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		out.Reset()
		_ = a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, c.args...))
		if got, want := normalize(out.Bytes()), normalize(body); got != want {
			t.Errorf("%s vs %v:\napi %s\ncli %s", c.path, c.args, want, got)
		}
	}
}

// TestQueryV1MessageSnapshots freezes the offline chats/messages/search/thread envelopes.
func TestQueryV1MessageSnapshots(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	seedV1(t, writer, account)
	ref := msgref.Encode("test@lid", "two@g.us", "t1")
	snapshots := []any{}
	for _, args := range [][]string{
		{"chats", "--v1", "--match", "first"},
		{"chats", "--v1", "--match", "team"},
		{"messages", "--chat", "two@g.us", "--v1"},
		{"search", "hello", "--chat", "one@g.us", "--context", "1", "--limit", "1", "--v1"},
		{"message", "--ref", ref, "--v1"},
		{"thread", "--ref", ref, "--before", "1", "--after", "1"},
		{"thread", "--ref", msgref.Encode("test@lid", "two@g.us", "t2")},
		{"thread", "--ref", "m1_bad"},
	} {
		out.Reset()
		err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
		snapshots = append(snapshots, map[string]any{"args": args, "exit_error": err != nil, "stdout": golden.Redact(golden.Decode(t, out.Bytes()), "request_id", "next_cursor", "received_at")})
	}
	golden.Compare(t, "v1/messages.json", snapshots)
}
