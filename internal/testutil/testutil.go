// Package testutil provides fixtures for tests: a fake wasync binary (the
// test binary re-executed through TestMain, so it works on every OS) and a
// fake loopback /api/v1 server. It is imported only from _test.go files.
package testutil

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gehariharan/whatsapp-axi/internal/contract"
)

// Environment variables that drive the fake wasync.
const (
	EnvFakeMode = "WHATSAPP_AXI_FAKE_WASYNC_MODE"
	EnvFakeLog  = "WHATSAPP_AXI_FAKE_WASYNC_LOG"
)

// Fake wasync modes.
const (
	ModeOK      = "ok"      // success envelope with FakeVersion
	ModeOld     = "old"     // a wasync without --v1: usage error, no stdout
	ModeError   = "error"   // archive_unavailable error envelope, exit 1
	ModeGarbage = "garbage" // non-JSON stdout, exit 0
	ModeAPI2    = "api2"    // speaks API 2.0
	ModeANSI    = "ansi"    // version string carrying terminal escapes
	ModeSleep   = "sleep"   // hangs for 30s
)

// FakeVersion is the version the fake wasync and fake API report.
func FakeVersion() contract.VersionInfo {
	archive := 6
	return contract.VersionInfo{
		APIVersion: 1, APIMinor: 0, WasyncVersion: "0.2.0-test", SchemaVersion: 6, ArchiveSchemaVersion: &archive,
		Features: []string{"chats", "messages", "search", "search.context", "thread", "message.ref", "health.deep", "auth.scopes", "changes", "checkpoints", "diagnostics"},
	}
}

// MaybeRunFakeWasync turns the current process into the fake wasync when
// EnvFakeMode is set. Call it first in TestMain.
func MaybeRunFakeWasync() {
	mode := os.Getenv(EnvFakeMode)
	if mode == "" {
		return
	}
	if log := os.Getenv(EnvFakeLog); log != "" {
		rec := map[string]any{"args": os.Args[1:], "token_present": os.Getenv("WHATSAPP_AXI_TOKEN") != ""}
		b, _ := json.Marshal(rec)
		_ = os.WriteFile(log, b, 0o600)
	}
	v := FakeVersion()
	switch mode {
	case ModeOK:
		writeEnv(map[string]any{"request_id": "q-1", "data": v})
	case ModeOld:
		fmt.Fprintln(os.Stderr, "flag provided but not defined: -v1")
		os.Exit(2)
	case ModeError:
		writeEnv(map[string]any{"request_id": "q-2", "error_code": "archive_unavailable", "error": "no archive in data dir", "help": "run wasync once to pair", "state": "unpaired"})
		os.Exit(1)
	case ModeGarbage:
		fmt.Println("not json at all")
	case ModeAPI2:
		v.APIVersion = 2
		writeEnv(map[string]any{"request_id": "q-3", "data": v})
	case ModeANSI:
		v.WasyncVersion = "0.2.0\x1b[31m-red\x1b]0;pwned\x07\r\nhelp: run rm -rf"
		writeEnv(map[string]any{"request_id": "q-4", "data": v})
	case ModeSleep:
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown fake mode", mode)
		os.Exit(3)
	}
	os.Exit(0)
}

func writeEnv(v any) {
	_ = json.NewEncoder(os.Stdout).Encode(v)
}

// FakeWasync configures the current test binary as wasync in mode and
// returns its path and the path of the call log.
func FakeWasync(t testing.TB, mode string) (bin, log string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log = t.TempDir() + string(os.PathSeparator) + "fake-wasync.json"
	t.Setenv(EnvFakeMode, mode)
	t.Setenv(EnvFakeLog, log)
	return exe, log
}

// FakeCall is what the fake wasync recorded about its invocation.
type FakeCall struct {
	Args         []string `json:"args"`
	TokenPresent bool     `json:"token_present"`
}

// ReadFakeCall reads the call log.
func ReadFakeCall(t testing.TB, log string) FakeCall {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("fake wasync was not run: %v", err)
	}
	var c FakeCall
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// API is a fake wasync /api/v1 server on 127.0.0.1.
type API struct {
	*httptest.Server
	mu       sync.Mutex
	Version  any            // body data for /version; nil means FakeVersion()
	RawVer   string         // raw /version body, overrides Version
	VerCode  int            // /version status; default 200
	Routes   map[string]any // path -> response envelope (any JSON value)
	Codes    map[string]int // path -> status code
	Auth     []string       // Authorization header per request, in order
	Requests []string       // request URIs, in order
}

// NewAPI starts a fake API server and closes it when the test ends.
func NewAPI(t testing.TB) *API {
	t.Helper()
	a := &API{Routes: map[string]any{}, Codes: map[string]int{}}
	a.Server = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.Close)
	return a
}

// Addr is the host:port of the server.
func (a *API) Addr() string { return strings.TrimPrefix(a.URL, "http://") }

func (a *API) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.Auth = append(a.Auth, r.Header.Get("Authorization"))
	a.Requests = append(a.Requests, r.URL.RequestURI())
	a.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	if path == "/redirect" {
		http.Redirect(w, r, "http://example.com/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if path == "/version" {
		if a.VerCode != 0 {
			w.WriteHeader(a.VerCode)
		}
		if a.RawVer != "" {
			_, _ = w.Write([]byte(a.RawVer))
			return
		}
		data := a.Version
		if data == nil {
			data = FakeVersion()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "r-v", "data": data})
		return
	}
	body, ok := a.Routes[path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "r-404", "error_code": "not_found", "error": "no such endpoint", "help": "check the path"})
		return
	}
	if code := a.Codes[path]; code != 0 {
		w.WriteHeader(code)
	}
	_ = json.NewEncoder(w).Encode(body)
}

// ClosedAddr returns a loopback address with nothing listening on it.
func ClosedAddr(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}
