// Package golden compares JSON contract snapshots in tests. Snapshots freeze
// existing agent-facing output so later API work cannot change it silently.
package golden

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Update rewrites snapshots instead of comparing: WASYNC_UPDATE_GOLDEN=1 go test ./...
func Update() bool { return os.Getenv("WASYNC_UPDATE_GOLDEN") == "1" }

// Redact replaces the values of volatile keys at any depth, keeping their
// presence and JSON type visible so a removed or retyped field still fails.
func Redact(value any, keys ...string) any {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			redacted := false
			for _, key := range keys {
				if k == key && child != nil && child != "" {
					v[k] = "<" + key + ">"
					redacted = true
				}
			}
			if !redacted {
				v[k] = Redact(child, keys...)
			}
		}
	case []any:
		for i := range v {
			v[i] = Redact(v[i], keys...)
		}
	}
	return value
}

// Decode parses raw JSON, failing the test on invalid output.
func Decode(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("invalid JSON %q: %v", raw, err)
	}
	return value
}

// Compare checks value against testdata/name, or writes it when updating.
func Compare(t *testing.T, name string, value any) {
	t.Helper()
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	path := filepath.Join("testdata", name)
	if Update() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing snapshot %s; run with WASYNC_UPDATE_GOLDEN=1: %v", path, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("%s changed; existing output must stay compatible.\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}
