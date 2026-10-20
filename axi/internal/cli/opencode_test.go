package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gehariharan/wasync/axi/internal/agentsetup"
)

func TestSetupOpenCodeUsesNativeGlobalAndProjectPaths(t *testing.T) {
	for _, project := range []bool{false, true} {
		h := newHarness(t)
		root := agentHome(h)
		relative := filepath.Join(".config", "opencode")
		args := []string{"hooks", "--agent", "opencode"}
		if project {
			root = t.TempDir()
			relative = ".opencode"
			args = append(args, "--dir", root)
		}
		plugin := filepath.Join(root, relative, "plugins", "whatsapp-axi.js")
		r, code := setupJSONRun(t, h, append(args, "--dry-run")...)
		if code != ExitOK || r.Written != 0 || len(r.Changes) != 1 {
			t.Fatalf("dry run: %d %+v", code, r)
		}
		if _, err := os.Stat(plugin); !os.IsNotExist(err) {
			t.Fatal("dry run installed plugin", err)
		}
		r, code = setupJSONRun(t, h, args...)
		if code != ExitOK || r.Written != 1 {
			t.Fatalf("install: %d %+v", code, r)
		}
		installed, err := os.ReadFile(plugin)
		if err != nil || string(installed) != string(agentsetup.OpenCodePlugin(harnessBin)) {
			t.Fatalf("plugin contract: %v", err)
		}
		r, code = setupJSONRun(t, h, args...)
		if code != ExitOK || r.Written != 0 {
			t.Fatalf("repeat: %d %+v", code, r)
		}
		r, code = setupJSONRun(t, h, append(args, "--remove")...)
		if code != ExitOK || r.Written != 1 {
			t.Fatalf("remove: %d %+v", code, r)
		}
		if _, err := os.Stat(plugin); !os.IsNotExist(err) {
			t.Fatal("remove left plugin", err)
		}
	}
}

func TestSetupPlansOpenCodeBeforeWritingAnyAgent(t *testing.T) {
	h := newHarness(t)
	root := agentHome(h)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), "not json")
	out, code := h.run(t, "setup", "hooks", "--agent", "opencode,claude")
	if code != ExitError {
		t.Fatalf("%d %s", code, out)
	}
	plugin := filepath.Join(root, ".config", "opencode", "plugins", "whatsapp-axi.js")
	if _, err := os.Stat(plugin); !os.IsNotExist(err) {
		t.Fatal("partial plugin install", err)
	}
	out, code = h.run(t, "setup", "hooks", "--agent", "all,absent")
	if code != ExitUsage {
		t.Fatalf("ignored invalid agent: %d %s", code, out)
	}
}
