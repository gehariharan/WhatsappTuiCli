package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gehariharan/wasync/internal/agentauth"
	"github.com/gehariharan/wasync/internal/golden"
	"github.com/gehariharan/wasync/internal/localfs"
	"github.com/gehariharan/wasync/internal/processlock"
	"github.com/gehariharan/wasync/internal/query"
)

func doctorRun(t *testing.T, a *App, out *bytes.Buffer, dir string, args ...string) query.Diagnostics {
	t.Helper()
	data := queryRun(t, a, out, dir, append([]string{"doctor"}, args...)...)
	var env struct {
		RequestID string            `json:"request_id"`
		Data      query.Diagnostics `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil || !cliRequestID.MatchString(env.RequestID) {
		t.Fatalf("doctor output %v: %s", err, data)
	}
	return env.Data
}

func check(d query.Diagnostics, name string) query.DiagnosticCheck {
	for _, c := range d.Checks {
		if c.Name == name {
			return c
		}
	}
	return query.DiagnosticCheck{}
}

func TestQueryDoctorWithoutArchiveCreatesNothing(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "absent")
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}, OpenClient: func(context.Context, string, *slog.Logger) (Client, error) {
		t.Fatal("doctor opened WhatsApp")
		return nil, nil
	}}
	for _, args := range [][]string{nil, {"--json"}, {"--v1"}} {
		d := doctorRun(t, a, &out, dir, args...)
		if d.Transport != "query" || d.Owner.Running || d.Owner.APIVersion != nil || d.Health != "offline" || d.Archive.Exists || d.DataDirPrivate || d.Paired == nil || *d.Paired || d.Agent != nil {
			t.Fatalf("absent data dir: %+v", d)
		}
		if c := check(d, "archive"); c.Status != "fail" || c.Fix != "wasync login" {
			t.Fatalf("archive check %+v", c)
		}
	}
	if entries := dirEntries(t, parent); len(entries) != 0 {
		t.Fatalf("doctor created %v", entries)
	}
	out.Reset()
	if err := a.Run(context.Background(), []string{"--data-dir", dir, "query", "doctor", "--help"}); err != nil || !bytes.Contains(out.Bytes(), []byte("Usage: wasync [--data-dir PATH] query doctor")) {
		t.Fatalf("doctor help: %v %s", err, out.Bytes())
	}
}

func TestQueryDoctorDetectsOwnerByLockWithoutHoldingIt(t *testing.T) {
	a, out, _, _, dir := queryFixture(t) // holds device.lock like a running owner
	credential, _, err := agentauth.Grant(dir, agentauth.GrantRequest{Name: "doctor-agent"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before := dirEntries(t, dir)
	d := doctorRun(t, a, out, dir)
	if !d.Owner.Running || d.Owner.APIVersion != nil || d.Health != "unknown" || !d.DataDirPrivate || !d.AgentsRegistryPrivate {
		t.Fatalf("owner running: %+v", d)
	}
	if !d.Archive.Exists || !d.Archive.Readable || d.Archive.SchemaVersion == nil || *d.Archive.SchemaVersion != d.Archive.SupportedSchemaVersion {
		t.Fatalf("archive: %+v", d.Archive)
	}
	if c := check(d, "agent"); c.Status != "pass" {
		t.Fatalf("agent check %+v", c)
	}
	if c := check(d, "pairing"); c.Status != "fail" || c.Fix != "wasync login" {
		t.Fatalf("pairing check %+v", c)
	}
	if after := dirEntries(t, dir); !slices.Equal(before, after) {
		t.Fatalf("doctor changed files: %v -> %v", before, after)
	}
	raw := out.String()
	_, secret, _ := agentauth.ParseCredential(credential)
	for _, forbidden := range []string{credential, secret, "secret_sha256", "hello archive", dir} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("doctor leaked %q: %s", forbidden, raw)
		}
	}
}

func TestQueryDoctorOwnerStopped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	var buf bytes.Buffer
	a, out := &App{Out: &buf, Err: &bytes.Buffer{}}, &buf
	// Create it as wasync does: chmod alone does not make a Windows ACL private.
	if err := localfs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	lock, err := processlock.Acquire(filepath.Join(dir, "device.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if d := doctorRun(t, a, out, dir); !d.Owner.Running || d.Health != "unknown" {
		t.Fatalf("held lock not detected: %+v", d)
	}
	lock.Close()
	// A lock file that exists but is not held: owner not running.
	before := dirEntries(t, dir)
	d := doctorRun(t, a, out, dir)
	if d.Owner.Running || d.Health != "offline" || !d.DataDirPrivate || d.Archive.Exists {
		t.Fatalf("stopped owner: %+v", d)
	}
	if c := check(d, "owner"); c.Status != "warn" || c.Fix != "wasync serve" {
		t.Fatalf("owner check %+v", c)
	}
	if c := check(d, "agent"); c.Status != "warn" || c.Fix != "wasync agents grant NAME --scope read.chats,read.messages" {
		t.Fatalf("agent check %+v", c)
	}
	if after := dirEntries(t, dir); !slices.Equal(before, after) {
		t.Fatalf("doctor changed files: %v -> %v", before, after)
	}
	// The probe never keeps the lock, so an owner can start right after.
	next, err := processlock.Acquire(filepath.Join(dir, "device.lock"))
	if err != nil {
		t.Fatalf("doctor kept device.lock: %v", err)
	}
	next.Close()
	if runtime.GOOS == "windows" {
		return // chmod cannot widen a Windows ACL, so the public case is Unix-only here.
	}
	if err = os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	d = doctorRun(t, a, out, dir)
	if c := check(d, "data_dir"); d.DataDirPrivate || c.Status != "fail" || c.Fix == "" {
		t.Fatalf("public data dir: %+v", c)
	}
}

func TestQueryDoctorRejectsArguments(t *testing.T) {
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	err := a.Run(context.Background(), []string{"--data-dir", t.TempDir(), "query", "doctor", "extra"})
	var e query.V1Error
	if err == nil || json.Unmarshal(out.Bytes(), &e) != nil || e.ErrorCode != query.CodeInvalidArgument {
		t.Fatalf("bad args: %v %s", err, out.Bytes())
	}
}

func TestQueryDoctorSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var out bytes.Buffer
	a := &App{Out: &out, Err: &bytes.Buffer{}}
	data := queryRun(t, a, &out, dir, "doctor")
	v := golden.Redact(golden.Decode(t, data), "request_id", "wasync_version", "os", "arch", "journal_available", "supported_schema_version")
	for _, c := range v.(map[string]any)["data"].(map[string]any)["checks"].([]any) {
		check := c.(map[string]any)
		switch check["name"] {
		case "version":
			check["detail"] = "<detail>"
		case "journal":
			check["status"], check["detail"], check["fix"] = "<status>", "<detail>", "<fix>"
		}
	}
	golden.Compare(t, "v1/doctor.json", v)
}
