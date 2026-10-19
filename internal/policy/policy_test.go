package policy

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const root = "../.."

func TestModuleFilesHaveNoForbiddenDependencies(t *testing.T) {
	for _, name := range []string{"go.mod", "go.sum"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) && name == "go.sum" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(b))
		for _, bad := range ForbiddenModules {
			if strings.Contains(lower, bad) {
				t.Errorf("%s mentions %q: whatsapp-axi must not depend on WhatsApp or SQLite code (ADR-001)", name, bad)
			}
		}
	}
}

func TestNoSourceImportsForbiddenPackages(t *testing.T) {
	fset := token.NewFileSet()
	seen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		seen++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, bad := range ForbiddenModules {
				if strings.Contains(strings.ToLower(p), bad) {
					t.Errorf("%s imports %s (ADR-001 forbids %q)", path, p, bad)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 10 {
		t.Fatalf("scanned only %d Go files; is the repository root right?", seen)
	}
}
