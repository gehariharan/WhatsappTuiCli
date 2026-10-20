package query

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gehariharan/wasync/internal/agentauth"
)

func TestHealthStateNeverConnectedFromStaleKeepalive(t *testing.T) {
	for _, tc := range []struct {
		connection string
		failing    bool
		state      string
		connected  bool
	}{
		{"online", false, StateOnline, true},
		{"online", true, StateDegraded, false},
		{"connecting", false, StateConnecting, false},
		{"reconnecting", false, StateReconnecting, false},
		{"pairing", false, StatePairing, false},
		{"offline", false, StateOffline, false},
		{"attention required", true, StateAttentionRequired, false},
		{"capture stopped", false, StateCaptureStopped, false},
		{"unlinked", false, StateUnlinked, false},
		{"offline browsing", false, StateOffline, false},
		{"", false, StateOffline, false},
	} {
		if state, connected := HealthState(tc.connection, tc.failing); state != tc.state || connected != tc.connected {
			t.Fatalf("%q failing=%t: %s %t", tc.connection, tc.failing, state, connected)
		}
	}
}

func TestDiagnoseEmptyDataDirWithoutCreating(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	d := Diagnose(context.Background(), DiagnosticsInput{Transport: "query", DataDir: dir, Health: StateOffline})
	if d.DataDirPrivate || d.Archive.Exists || d.Archive.Readable || d.Archive.SchemaVersion != nil || !d.AgentsRegistryPrivate || d.Agent != nil || d.Paired != nil {
		t.Fatalf("empty diagnostics %+v", d)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("diagnose created the data directory")
	}
	want := []string{"version", "data_dir", "archive", "pairing", "owner", "health", "agents_registry", "agent", "journal"}
	if len(d.Checks) != len(want) {
		t.Fatalf("checks %+v", d.Checks)
	}
	for i, c := range d.Checks {
		if c.Name != want[i] || (c.Status != "pass" && c.Fix == "") {
			t.Fatalf("check %d %+v", i, c)
		}
	}
}

func TestDiagnoseScopeRepairPreservesRestrictions(t *testing.T) {
	for _, scopes := range [][]string{
		{agentauth.ScopeReadChats, agentauth.ScopeReadMessages},
		{agentauth.ScopeObserve},
		{agentauth.ScopeReadMessages},
		{},
	} {
		agent := &agentauth.Client{Name: "reader", Scopes: scopes, Chats: []string{"family@g.us", "team@g.us"}, RatePerMinute: 5}
		d := Diagnose(context.Background(), DiagnosticsInput{Transport: "api", DataDir: t.TempDir(), Agent: agent})
		found := false
		for _, check := range d.Checks {
			if check.Name == "agent" {
				found = true
				want := "wasync agents revoke reader && wasync agents grant reader --scope read.chats,read.messages,observe --chat family@g.us --chat team@g.us --rate 5"
				if check.Status != "warn" || check.Fix != want {
					t.Fatalf("scopes %v: %+v", scopes, check)
				}
			}
		}
		if !found {
			t.Fatal("agent diagnostic missing")
		}
	}
}
