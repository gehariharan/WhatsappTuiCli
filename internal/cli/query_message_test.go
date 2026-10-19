package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"whatsappsync/internal/msgref"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

func TestQueryMessageByRef(t *testing.T) {
	a, out, writer, account, dir := queryFixture(t)
	ref := msgref.Encode("test@lid", "one@g.us", "1")
	data := queryRun(t, a, out, dir, "message", "--ref", ref, "--json")
	var got struct {
		AccountID int64           `json:"account_id"`
		Ref       string          `json:"ref"`
		Item      json.RawMessage `json:"item"`
		Extra     json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.AccountID != account || got.Ref != ref || got.Extra != nil {
		t.Fatalf("bad envelope %s", data)
	}
	// The item is byte-identical to the legacy shape used by query messages.
	page, err := writer.Messages(context.Background(), account, "one@g.us", storage.MessageQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var legacy []byte
	for _, m := range page.Items {
		if m.MessageID == "1" {
			legacy, _ = json.Marshal(query.FormatMessage(account, m))
			if query.RefFor("test@lid", m) != ref {
				t.Fatal("RefFor disagrees with Encode")
			}
		}
	}
	if string(got.Item) != string(legacy) {
		t.Fatalf("item %s differs from legacy %s", got.Item, legacy)
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(data, &keys)
	if len(keys) != 3 {
		t.Fatalf("unexpected top-level fields %s", data)
	}
}

func TestQueryMessageErrors(t *testing.T) {
	a, out, _, _, dir := queryFixture(t)
	for name, args := range map[string][]string{
		"missing ref":     {"message"},
		"invalid ref":     {"message", "--ref", "m1_not*base64"},
		"legacy row id":   {"message", "--ref", "1"},
		"other account":   {"message", "--ref", msgref.Encode("other@lid", "one@g.us", "1")},
		"unknown message": {"message", "--ref", msgref.Encode("test@lid", "one@g.us", "nope")},
		"positional":      {"message", "--ref", msgref.Encode("test@lid", "one@g.us", "1"), "extra"},
	} {
		out.Reset()
		err := a.Run(context.Background(), append([]string{"--data-dir", dir, "query"}, args...))
		if err == nil || out.Len() != 0 {
			t.Errorf("%s: err=%v stdout=%q", name, err, out.String())
		}
		if name == "invalid ref" && !errors.Is(err, msgref.ErrInvalid) {
			t.Errorf("invalid ref error is untyped: %v", err)
		}
		if name == "other account" && !strings.Contains(err.Error(), "not in the current account") {
			t.Errorf("cross-account error: %v", err)
		}
	}
	out.Reset()
	if err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "message", "--help"}); err != nil || !strings.Contains(out.String(), "--ref REF") {
		t.Fatalf("help: %v %q", err, out.String())
	}
}
