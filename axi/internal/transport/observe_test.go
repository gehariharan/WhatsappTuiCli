package transport

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gehariharan/wasync/axi/internal/config"
	"github.com/gehariharan/wasync/axi/internal/contract"
	"github.com/gehariharan/wasync/axi/internal/testutil"
)

func TestQueryArgsJournalRoutes(t *testing.T) {
	args, err := QueryArgs("", "/changes", url.Values{"after": {"c1_x"}, "limit": {"5"}})
	if want := []string{"query", "changes", "--after=c1_x", "--limit=5", "--v1", "--json"}; err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("changes args %v err %v", args, err)
	}
	args, err = QueryArgs("/d", "/checkpoints/replica", nil)
	if want := []string{"--data-dir", "/d", "query", "checkpoint", "--consumer=replica", "--v1", "--json"}; err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("checkpoint args %v err %v", args, err)
	}
	for _, p := range []string{"/checkpoints/", "/checkpoints/a/b", "/checkpoints"} {
		if _, err := QueryArgs("", p, nil); codeOf(t, err) != contract.CodeOwnerOffline {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := QueryArgs("", "/changes", url.Values{"since": {"2026-10-01"}}); codeOf(t, err) != contract.CodeInvalidArgument {
		t.Fatalf("timestamp parameter accepted offline: %v", err)
	}
}

func TestAPIPutSendsJSONWithBearer(t *testing.T) {
	j := testutil.NewJournal(3)
	srv := testutil.NewJournalAPI(t, j)
	a, err := NewAPI(APIConfig{Addr: srv.Addr(), Credential: &config.Credential{Secret: "wsa_x"}})
	if err != nil {
		t.Fatal(err)
	}
	var cp map[string]any
	if err := a.Put(context.Background(), "/checkpoints/replica", map[string]string{"cursor": j.Cursor(2)}, &cp); err != nil {
		t.Fatal(err)
	}
	if cp["cursor"] != j.Cursor(2) || srv.Requests()[0] != "PUT /api/v1/checkpoints/replica" {
		t.Fatalf("cp %v requests %v", cp, srv.Requests())
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(srv.Bodies()[0]), &body); err != nil || body["cursor"] != j.Cursor(2) || len(body) != 1 {
		t.Fatalf("body %q", srv.Bodies()[0])
	}
	err = a.Put(context.Background(), "/checkpoints/replica", map[string]string{"cursor": j.Cursor(1)}, &cp)
	if codeOf(t, err) != contract.CodeInvalidArgument || !strings.Contains(err.Error(), "backwards") {
		t.Fatalf("backwards commit: %v", err)
	}
	if err := a.Put(context.Background(), "/../x", nil, nil); codeOf(t, err) != contract.CodeInvalidArgument {
		t.Fatalf("bad path: %v", err)
	}
	noCred, _ := NewAPI(APIConfig{Addr: srv.Addr()})
	if err := noCred.Put(context.Background(), "/checkpoints/replica", nil, nil); codeOf(t, err) != contract.CodeCredentialAbsent {
		t.Fatalf("no credential: %v", err)
	}
	if len(srv.Requests()) != 2 {
		t.Fatalf("requests %v", srv.Requests())
	}
}

func TestQueryJournalMatchesAPI(t *testing.T) {
	j := testutil.NewJournal(4)
	j.Checkpoints["replica"] = testutil.JournalCheckpoint{Epoch: j.Epoch, Sequence: 1}
	srv := testutil.NewJournalAPI(t, j)
	api, _ := NewAPI(APIConfig{Addr: srv.Addr(), Credential: &config.Credential{Secret: "wsa_x"}})
	bin, log := testutil.FakeJournalWasync(t, j)
	q := NewQuery(QueryConfig{Bin: bin})
	for _, c := range []struct {
		path  string
		query url.Values
	}{
		{"/changes", url.Values{"limit": {"2"}}},
		{"/changes", url.Values{"after": {j.Cursor(2)}, "limit": {"5"}}},
		{"/checkpoints/replica", nil},
		{"/checkpoints/other", nil},
	} {
		var fromAPI, fromQuery contract.Response
		if err := api.Get(context.Background(), c.path, c.query, &fromAPI); err != nil {
			t.Fatal(err)
		}
		if err := q.Get(context.Background(), c.path, c.query, &fromQuery); err != nil {
			t.Fatal(err)
		}
		if string(fromAPI.Data) != string(fromQuery.Data) || !reflect.DeepEqual(fromAPI.Page, fromQuery.Page) {
			t.Fatalf("%s %v: api %s %+v, query %s %+v", c.path, c.query, fromAPI.Data, fromAPI.Page, fromQuery.Data, fromQuery.Page)
		}
	}
	err := q.Get(context.Background(), "/changes", url.Values{"after": {"c1_garbage"}}, &contract.Response{})
	if codeOf(t, err) != contract.CodeInvalidCursor {
		t.Fatalf("offline invalid cursor: %v", err)
	}
	if calls := testutil.ReadJournalCalls(t, log); len(calls) != 5 {
		t.Fatalf("calls %v", calls)
	}
}
