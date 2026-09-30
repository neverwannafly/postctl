package qa

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var goRef = regexp.MustCompile("`([^`[:space:]]+\\.go)`(?:[[:space:]]+`([A-Za-z_][A-Za-z0-9_]*(?:\\.[A-Za-z_][A-Za-z0-9_]*)?)`)?")

// TestArchitectureDocsReferencesExist keeps the onboarding path executable:
// every backticked Go path must exist and an adjacent symbol must be declared
// in that exact file.
func TestArchitectureDocsReferencesExist(t *testing.T) {
	root := repoRoot(t)
	docs := []string{
		"CONTRIBUTING.md",
		"docs/architecture.md",
		"docs/onboarding.md",
	}
	flows, err := filepath.Glob(filepath.Join(root, "docs", "flows", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range flows {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, rel)
	}

	for _, doc := range docs {
		doc := doc
		t.Run(strings.ReplaceAll(doc, "/", "_"), func(t *testing.T) {
			checkDocRefs(t, root, doc)
		})
	}
}

func checkDocRefs(t *testing.T, root, doc string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range goRef.FindAllStringSubmatch(string(data), -1) {
		rel, symbol := filepath.Clean(match[1]), match[2]
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			t.Errorf("%s: Go reference must be repository-relative: %q", doc, match[1])
			continue
		}
		path := filepath.Join(root, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s: referenced file %q: %v", doc, rel, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("%s: Go reference %q is a directory", doc, rel)
			continue
		}
		if symbol != "" && !declares(t, path, symbol) {
			t.Errorf("%s: %q does not declare %q", doc, rel, symbol)
		}
	}
}

func declares(t *testing.T, path, want string) bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Errorf("parse %s: %v", path, err)
		return false
	}
	declared := map[string]bool{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					declared[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, name := range s.Names {
						declared[name.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			declared[d.Name.Name] = true
			if recv := receiverName(d); recv != "" {
				declared[recv+"."+d.Name.Name] = true
			}
		}
	}
	return declared[want]
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if name, ok := expr.(*ast.Ident); ok {
		return name.Name
	}
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}
