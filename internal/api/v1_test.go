package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"whatsappsync/internal/golden"
	"whatsappsync/internal/query"
	"whatsappsync/internal/storage"
)

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (f fixture) v1(t *testing.T, method, path, token string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

func TestV1VersionNeedsNoAuthAndLeaksNoContent(t *testing.T) {
	f := setup(t)
	for _, token := range []string{"", "wrong", f.token} {
		res, body := f.v1(t, "GET", "/api/v1/version", token)
		if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("version %d %v: %s", res.StatusCode, res.Header, body)
		}
		if strings.Contains(string(body), "hello archive") || strings.Contains(string(body), "@g.us") || strings.Contains(string(body), "test@lid") {
			t.Fatalf("version leaked archive content: %s", body)
		}
		var got struct {
			RequestID string            `json:"request_id"`
			Data      query.VersionInfo `json:"data"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if !requestIDPattern.MatchString(got.RequestID) || res.Header.Get("X-Request-Id") != got.RequestID {
			t.Fatalf("request id %q header %q", got.RequestID, res.Header.Get("X-Request-Id"))
		}
		if got.Data.APIVersion != 1 || got.Data.APIMinor != 0 || got.Data.SchemaVersion != storage.SchemaVersion || got.Data.ArchiveSchemaVersion != nil || !slices.IsSorted(got.Data.Features) || !slices.Contains(got.Data.Features, "envelope.v1") || !slices.Contains(got.Data.Features, "query.v1") || !slices.Contains(got.Data.Features, "version") {
			t.Fatalf("version data %+v", got.Data)
		}
	}
	first, _ := f.v1(t, "GET", "/api/v1/version", "")
	second, _ := f.v1(t, "GET", "/api/v1/version", "")
	if first.Header.Get("X-Request-Id") == second.Header.Get("X-Request-Id") {
		t.Fatal("request id reused")
	}
}

func TestV1HostAndOriginChecksStillApply(t *testing.T) {
	f := setup(t)
	h := (&handler{store: f.store, token: f.token, authority: "127.0.0.1:8787"}).routes()
	for _, tc := range []struct{ host, origin string }{{"attacker.invalid:8787", ""}, {"127.0.0.1:8787", "https://attacker.invalid"}, {"127.0.0.1:8787", "null"}} {
		for _, path := range []string{"/api/v1/version", "/api/v1/chats"} {
			req := httptest.NewRequest("GET", "http://127.0.0.1:8787"+path, nil)
			req.Host = tc.host
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Authorization", "Bearer "+f.token)
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			var e query.V1Error
			if res.Code != 403 || json.Unmarshal(res.Body.Bytes(), &e) != nil || e.ErrorCode != query.CodeForbidden || !requestIDPattern.MatchString(e.RequestID) {
				t.Fatalf("accepted host/origin %+v %s: %d %s", tc, path, res.Code, res.Body)
			}
		}
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:8787/api/v1/version", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("valid host rejected: %d", res.Code)
	}
}

func TestV1UnknownPathsRequireAuthAndUseEnvelope(t *testing.T) {
	f := setup(t)
	for _, path := range []string{"/api/v1", "/api/v1/", "/api/v1/chats", "/api/v1/messages/search?q=hello"} {
		for _, token := range []string{"", "wrong"} {
			res, body := f.v1(t, "GET", path, token)
			var e query.V1Error
			if res.StatusCode != 401 || res.Header.Get("WWW-Authenticate") != "Bearer" || json.Unmarshal(body, &e) != nil || e.ErrorCode != query.CodeUnauthorized || e.RequestID != res.Header.Get("X-Request-Id") {
				t.Fatalf("%s %q: %d %s", path, token, res.StatusCode, body)
			}
		}
		res, body := f.v1(t, "GET", path, f.token)
		var e query.V1Error
		if res.StatusCode != 401 || json.Unmarshal(body, &e) != nil || e.ErrorCode != query.CodeUnauthorized || strings.Contains(string(body), "hello archive") {
			t.Fatalf("legacy api.token accepted on %s: %d %s", path, res.StatusCode, body)
		}
		res, body = f.v1(t, "GET", path, f.agent)
		if res.StatusCode != 404 || json.Unmarshal(body, &e) != nil || e.ErrorCode != query.CodeNotFound || e.Help != "see GET /api/v1/version for supported features" || strings.Contains(string(body), "hello archive") {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
	}
	// Legacy errors keep their shape beside v1.
	res, body := f.v1(t, "GET", "/api/status", "")
	if res.StatusCode != 401 || strings.TrimSpace(string(body)) != `{"error":"bearer token required"}` || res.Header.Get("X-Request-Id") != "" {
		t.Fatalf("legacy changed: %d %s", res.StatusCode, body)
	}
}

func TestV1Snapshots(t *testing.T) {
	f := setup(t)
	type call struct{ method, path, token string }
	read, none := f.token, ""
	calls := []call{
		{"GET", "/api/v1/version", none},
		{"GET", "/api/v1/version", read},
		{"POST", "/api/v1/version", none},
		{"GET", "/api/v1/chats", none},
		{"GET", "/api/v1/chats", "wrong"},
		{"GET", "/api/v1/chats", read},
		{"GET", "/api/v1/unknown", f.agent},
		{"GET", "/api/v1/changes", none},
		{"GET", "/api/v1/changes", f.agent},
		{"PUT", "/api/v1/checkpoints/replica", f.agent},
	}
	snapshots := make([]any, 0, len(calls))
	for _, c := range calls {
		res, body := f.v1(t, c.method, c.path, c.token)
		auth := "none"
		if c.token == read {
			auth = "api.token"
		} else if c.token == f.agent {
			auth = "agent"
		} else if c.token != "" {
			auth = "invalid"
		}
		snapshots = append(snapshots, map[string]any{
			"request":          c.method + " " + c.path,
			"auth":             auth,
			"status":           res.StatusCode,
			"content_type":     res.Header.Get("Content-Type"),
			"www_authenticate": res.Header.Get("WWW-Authenticate"),
			"request_id":       requestIDPattern.MatchString(res.Header.Get("X-Request-Id")),
			"body":             golden.Redact(golden.Decode(t, body), "request_id", "wasync_version", "features"),
		})
	}
	golden.Compare(t, "v1/api.json", snapshots)
}

func TestAgentCredentialRejectedOnLegacyRoutes(t *testing.T) {
	f := setup(t)
	for _, path := range []string{"/api/status", "/api/chats", "/api/messages/search?q=hello", "/api/messages/1"} {
		if body := f.request(t, "GET", path, f.agent, 401); strings.Contains(string(body), "hello archive") {
			t.Fatalf("agent credential read legacy %s", path)
		}
	}
	for _, path := range []string{"/api/chats/one@g.us/disable", "/api/chats/enable-all"} {
		f.request(t, "POST", path, f.agent, 401)
	}
}
