package transport

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gehariharan/wasync/axi/internal/config"
	"github.com/gehariharan/wasync/axi/internal/contract"
	"github.com/gehariharan/wasync/axi/internal/testutil"
)

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *contract.Error, got %T %v", err, err)
	}
	return ce.Code
}

func TestValidateAddr(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:8787", "[::1]:8787", "127.0.0.2:1"} {
		if err := ValidateAddr(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "localhost:8787", "0.0.0.0:8787", "192.168.1.2:8787", "example.com:80",
		"127.0.0.1", "127.0.0.1:0", "127.0.0.1:99999", "127.0.0.1:http", "[::]:8787", "8.8.8.8:53"} {
		if err := ValidateAddr(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := NewAPI(APIConfig{Addr: "10.0.0.1:8787"}); err == nil {
		t.Fatal("NewAPI accepted a non-loopback address")
	}
}

func newAPI(t *testing.T, srv *testutil.API, cred *config.Credential) *API {
	t.Helper()
	a, err := NewAPI(APIConfig{Addr: srv.Addr(), Credential: cred, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAPIVersionSendsNoCredential(t *testing.T) {
	srv := testutil.NewAPI(t)
	a := newAPI(t, srv, &config.Credential{Secret: "wsa_secret"})
	v, err := a.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.APIVersion != 1 || v.WasyncVersion != "0.2.0-test" || !v.HasFeature(contract.FeatureChanges) || v.ArchiveSchemaVersion == nil {
		t.Fatalf("unexpected version %+v", v)
	}
	if srv.Auth[0] != "" || srv.Requests[0] != "/api/v1/version" {
		t.Fatalf("version request = %q auth %q", srv.Requests[0], srv.Auth[0])
	}
}

func TestAPIGetSendsBearerAndDecodesPage(t *testing.T) {
	srv := testutil.NewAPI(t)
	srv.Routes["/chats"] = map[string]any{"request_id": "r1", "data": []map[string]string{{"jid": "1@g.us"}}, "page": map[string]any{"next_cursor": "c2", "has_more": true}}
	a := newAPI(t, srv, &config.Credential{Secret: "wsa_secret"})
	var resp contract.Response
	if err := a.Get(context.Background(), "/chats", url.Values{"match": {"fam ily"}}, &resp); err != nil {
		t.Fatal(err)
	}
	if srv.Auth[0] != "Bearer wsa_secret" {
		t.Fatalf("auth header %q", srv.Auth[0])
	}
	if srv.Requests[0] != "/api/v1/chats?match=fam+ily" {
		t.Fatalf("request %q", srv.Requests[0])
	}
	if resp.RequestID != "r1" || resp.Page == nil || !resp.Page.HasMore || resp.Page.NextCursor != "c2" {
		t.Fatalf("response %+v", resp)
	}
	var rows []map[string]string
	if err := a.Get(context.Background(), "/chats", nil, &rows); err != nil || rows[0]["jid"] != "1@g.us" {
		t.Fatalf("rows %v err %v", rows, err)
	}
}

func TestAPIErrorEnvelopes(t *testing.T) {
	srv := testutil.NewAPI(t)
	for _, code := range contract.WasyncCodes {
		srv.Routes["/e/"+code] = map[string]any{"request_id": "r-" + code, "error_code": code, "error": "boom " + code, "help": "fix " + code, "state": map[string]any{"owner": "running"}}
		srv.Codes["/e/"+code] = 400
	}
	a := newAPI(t, srv, &config.Credential{Secret: "wsa_x"})
	for _, code := range contract.WasyncCodes {
		err := a.Get(context.Background(), "/e/"+code, nil, &struct{}{})
		var ce *contract.Error
		if !errors.As(err, &ce) {
			t.Fatalf("%s: %v", code, err)
		}
		if ce.Code != code || ce.Message != "boom "+code || ce.Help != "fix "+code || ce.RequestID != "r-"+code || ce.Transport != NameAPI || ce.StateString() != `{"owner":"running"}` {
			t.Fatalf("%s: decoded %+v", code, ce)
		}
	}
}

func TestAPIGetWithoutCredential(t *testing.T) {
	srv := testutil.NewAPI(t)
	a := newAPI(t, srv, nil)
	if got := codeOf(t, a.Get(context.Background(), "/chats", nil, &struct{}{})); got != contract.CodeCredentialAbsent {
		t.Fatalf("code %s", got)
	}
	if len(srv.Requests) != 0 {
		t.Fatal("request sent without a credential")
	}
}

func TestAPINeverFollowsRedirects(t *testing.T) {
	srv := testutil.NewAPI(t)
	a := newAPI(t, srv, &config.Credential{Secret: "wsa_x"})
	err := a.Get(context.Background(), "/redirect", nil, &struct{}{})
	if codeOf(t, err) != contract.CodeTransport || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err %v", err)
	}
	if len(srv.Requests) != 1 {
		t.Fatalf("requests %v", srv.Requests)
	}
}

func TestAPIBadResponses(t *testing.T) {
	srv := testutil.NewAPI(t)
	srv.RawVer = "<html>not wasync</html>"
	a := newAPI(t, srv, nil)
	if _, err := a.Version(context.Background()); codeOf(t, err) != contract.CodeInvalidResponse {
		t.Fatalf("err %v", err)
	}
	srv.VerCode = 404
	srv.RawVer = "404 page not found"
	if _, err := a.Version(context.Background()); codeOf(t, err) != contract.CodeWasyncTooOld {
		t.Fatalf("err %v", err)
	}
}

func TestAPIRejectsBadPaths(t *testing.T) {
	srv := testutil.NewAPI(t)
	a := newAPI(t, srv, &config.Credential{Secret: "wsa_x"})
	for _, p := range []string{"chats", "/../api.token", "/chats?x=1", "//evil", `/a\b`} {
		if err := a.Get(context.Background(), p, nil, nil); codeOf(t, err) != contract.CodeInvalidArgument {
			t.Fatalf("%q: %v", p, err)
		}
	}
	if len(srv.Requests) != 0 {
		t.Fatal("bad path reached the server")
	}
}

func TestAPIUnreachable(t *testing.T) {
	a, err := NewAPI(APIConfig{Addr: testutil.ClosedAddr(t), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Version(context.Background()); codeOf(t, err) != contract.CodeOwnerOffline {
		t.Fatalf("err %v", err)
	}
}
