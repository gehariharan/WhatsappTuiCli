package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gehariharan/wasync/axi/internal/contract"
)

func env(m map[string]string) Getenv { return func(k string) string { return m[k] } }

func code(err error) string {
	var ce *contract.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func TestLoadDefaults(t *testing.T) {
	s := Load(env(map[string]string{EnvConfigDir: "/cfg"}))
	if s.APIAddr != DefaultAPIAddr || s.ConfigDir != "/cfg" || s.Bin != "" || s.DataDir != "" {
		t.Fatalf("%+v", s)
	}
	s = Load(env(map[string]string{EnvAPIAddr: " 127.0.0.1:9999 ", EnvBin: "/x/wasync", EnvDataDir: "/d"}))
	if s.APIAddr != "127.0.0.1:9999" || s.Bin != "/x/wasync" || s.DataDir != "/d" {
		t.Fatalf("%+v", s)
	}
}

func TestCredentialFromEnv(t *testing.T) {
	c, err := LoadCredential(env(map[string]string{EnvToken: "wsa_abc"}), "")
	if err != nil || c.Secret != "wsa_abc" || c.Source != "env" {
		t.Fatalf("%+v %v", c, err)
	}
	// wasync's own hex api.token / send.token are never accepted.
	for _, bad := range []string{"0123456789abcdef0123456789abcdef", "wsa_", "wsa_a b"} {
		if _, err := LoadCredential(env(map[string]string{EnvToken: bad}), ""); code(err) != contract.CodeCredentialBad {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestCredentialFile(t *testing.T) {
	dir := t.TempDir()
	if c, err := LoadCredential(env(nil), dir); c != nil || err != nil {
		t.Fatalf("missing file: %v %v", c, err)
	}
	path := filepath.Join(dir, CredentialFile)
	if err := os.WriteFile(path, []byte("wsa_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCredential(env(nil), dir)
	if err != nil || c.Secret != "wsa_file" || c.Source != path {
		t.Fatalf("%+v %v", c, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCredential(env(nil), dir); code(err) != contract.CodeCredentialBad {
			t.Fatalf("world-readable file accepted: %v", err)
		}
	}
	// The environment wins over the file.
	c, err = LoadCredential(env(map[string]string{EnvToken: "wsa_env"}), dir)
	if err != nil || c.Secret != "wsa_env" {
		t.Fatalf("%+v %v", c, err)
	}
}
