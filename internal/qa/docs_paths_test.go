package qa

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// backticked matches an inline code span on one line.
	backticked = regexp.MustCompile("`([^`\n]+)`")
	// mdLink matches a markdown link target.
	mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	// testName matches a Go test function name.
	testName = regexp.MustCompile(`^Test[A-Z][A-Za-z0-9_]*$`)
	// envName matches a postctl environment variable.
	envName = regexp.MustCompile(`^POSTCTL_[A-Z_]+$`)
	// taskRef matches `task <name>` inside a code span.
	taskRef = regexp.MustCompile(`^task ([a-z][a-z:-]*)`)
	// pathLike is a token that can only be a repository path.
	pathLike = regexp.MustCompile(`^[A-Za-z0-9_.@/-]+$`)
)

// notCommitted are paths agent docs may mention that never exist in a clean
// checkout: gitignored data and build output.
var notCommitted = []string{"testdata/private", ".postctl", "bin/", ".task"}

// externalPrefixes start Go import paths that are not files in this repository.
var externalPrefixes = []string{"github.com/", "encoding/", "net/", "os/", "runtime/", "io/"}

// agentDocs lists every agent-facing guide: AGENTS.md and CLAUDE.md at any
// depth, and the Cursor rules.
func agentDocs(t *testing.T, root string) []string {
	t.Helper()
	var docs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "private":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		base := filepath.Base(rel)
		if base == "AGENTS.md" || base == "CLAUDE.md" || strings.HasPrefix(rel, ".cursor/rules/") && strings.HasSuffix(rel, ".mdc") {
			docs = append(docs, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(docs)
	return docs
}

// TestAgentDocsReferencesExist keeps the agent guides honest: every repository
// path, Go symbol next to a path, test name, environment variable and task
// name they mention must exist, and every import and link must resolve.
func TestAgentDocsReferencesExist(t *testing.T) {
	root := repoRoot(t)
	indexBasenames(t, root)
	docs := agentDocs(t, root)
	for _, required := range []string{"AGENTS.md", "CLAUDE.md"} {
		if !contains(docs, required) {
			t.Fatalf("%s is missing", required)
		}
	}
	if len(docs) < 10 {
		t.Fatalf("expected the root guides, nested guides and Cursor rules; found %d files", len(docs))
	}
	tests := declaredTests(t, root)
	goSource := allGoSource(t, root)
	tasks := taskNames(t, root)

	for _, doc := range docs {
		t.Run(strings.ReplaceAll(doc, "/", "_"), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, doc))
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			dir := filepath.Dir(doc)

			for _, line := range strings.Split(text, "\n") {
				if strings.HasPrefix(line, "@") {
					if !pathExists(root, dir, strings.TrimPrefix(strings.TrimSpace(line), "@")) {
						t.Errorf("%s: import %q does not exist", doc, line)
					}
				}
			}
			for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
				target := strings.TrimPrefix(m[1], "mdc:")
				if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
					continue
				}
				target, _, _ = strings.Cut(target, "#")
				if target != "" && !pathExists(root, dir, target) {
					t.Errorf("%s: link target %q does not exist", doc, m[1])
				}
			}
			for _, m := range backticked.FindAllStringSubmatch(text, -1) {
				token := strings.TrimSpace(m[1])
				switch {
				case testName.MatchString(token):
					if !tests[token] {
						t.Errorf("%s: test %q does not exist", doc, token)
					}
				case envName.MatchString(token):
					if !strings.Contains(goSource, token) {
						t.Errorf("%s: environment variable %q is not used by any Go file", doc, token)
					}
				case taskRef.MatchString(token):
					if name := taskRef.FindStringSubmatch(token)[1]; !tasks[name] {
						t.Errorf("%s: task %q is not in Taskfile.yml", doc, name)
					}
				case looksLikePath(token):
					if !pathExists(root, dir, token) {
						t.Errorf("%s: path %q does not exist", doc, token)
					}
				}
			}
		})
	}
}

// looksLikePath reports whether a code span is a concrete repository path (no
// placeholders, globs, flags, URLs or absolute paths).
func looksLikePath(token string) bool {
	if token == "" || !pathLike.MatchString(token) || strings.HasPrefix(token, "-") ||
		strings.HasPrefix(token, "/") || strings.Contains(token, "..") {
		return false
	}
	for _, prefix := range externalPrefixes {
		if strings.HasPrefix(token, prefix) {
			return false // Go import paths, not repository files
		}
	}
	if strings.Contains(token, "/") {
		return true
	}
	switch filepath.Ext(token) {
	case ".go", ".md", ".yml", ".yaml", ".mdc", ".py":
		return true
	}
	return false
}

func pathExists(root, docDir, token string) bool {
	token = strings.TrimSuffix(filepath.Clean(token), "/")
	for _, prefix := range notCommitted {
		if token == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(token+"/", prefix) ||
			strings.HasPrefix(token, prefix) || strings.HasSuffix(token, "/"+strings.TrimSuffix(prefix, "/")) {
			return true
		}
	}
	for _, base := range []string{root, filepath.Join(root, docDir)} {
		if _, err := os.Stat(filepath.Join(base, token)); err == nil {
			return true
		}
	}
	// A bare file name is shorthand for "the file of that name in the package
	// the sentence is about"; accept it when some file in the repository has it.
	return !strings.Contains(token, "/") && basenames[token]
}

// basenames holds the base name of every file in the repository, filled by
// TestAgentDocsReferencesExist before any path is checked.
var basenames = map[string]bool{}

func indexBasenames(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "private":
				return filepath.SkipDir
			}
			return nil
		}
		basenames[d.Name()] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

var testFunc = regexp.MustCompile(`(?m)^func (Test[A-Z][A-Za-z0-9_]*)\(`)

func declaredTests(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	walkGo(t, root, func(path string, data []byte) {
		if !strings.HasSuffix(path, "_test.go") {
			return
		}
		for _, m := range testFunc.FindAllSubmatch(data, -1) {
			out[string(m[1])] = true
		}
	})
	return out
}

func allGoSource(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	walkGo(t, root, func(_ string, data []byte) { b.Write(data) })
	return b.String()
}

func walkGo(t *testing.T, root string, visit func(path string, data []byte)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "private":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			visit(path, data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var taskDecl = regexp.MustCompile(`(?m)^  ([a-z][a-z:-]*):\s*$`)

// taskNames returns the task names declared in Taskfile.yml.
func taskNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, m := range taskDecl.FindAllSubmatch(data, -1) {
		out[string(m[1])] = true
	}
	return out
}
