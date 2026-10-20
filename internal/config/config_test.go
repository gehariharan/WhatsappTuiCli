package config

import (
	"github.com/gehariharan/wasync/internal/buildinfo"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDistributionDefaultAndExplicitDataDirectory(t *testing.T) {
	original := buildinfo.Distribution
	defer func() { buildinfo.Distribution = original }()
	t.Setenv("WASYNC_DATA_DIR", "")
	for _, distribution := range []string{"standalone", "msix"} {
		buildinfo.Distribution = distribution
		cfg, err := Default()
		if err != nil {
			t.Fatal(err)
		}
		base, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		expected := filepath.Join(base, "wasync")
		if runtime.GOOS == "windows" && distribution == "msix" {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			expected = filepath.Join(home, ".wasync")
		}
		if cfg.DataDir != expected {
			t.Fatalf("%s default %q, want %q", distribution, cfg.DataDir, expected)
		}
	}
	explicit := filepath.Join(t.TempDir(), "explicit")
	t.Setenv("WASYNC_DATA_DIR", explicit)
	cfg, err := Default()
	if err != nil || cfg.DataDir != explicit {
		t.Fatalf("explicit override lost: %+v %v", cfg, err)
	}
}
