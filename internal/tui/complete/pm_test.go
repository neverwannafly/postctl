package complete

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func pmLabels(t *testing.T, before string) (labels []string, replace int) {
	t.Helper()
	items, replace := PM().Complete(before)
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	return labels, replace
}

func contains(list []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, l := range list {
			if l == w {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestPMCompletesMembersAtEveryDepth(t *testing.T) {
	cases := []struct {
		before  string
		want    []string
		replace int
	}{
		{"pm.", []string{"environment", "response", "expect", "test"}, 0},
		{"  const x = pm.env", []string{"environment"}, 3},
		{"pm.environment.", []string{"get", "set", "unset", "toObject"}, 0},
		{"pm.collectionVariables.se", []string{"set"}, 2},
		{"pm.response.", []string{"code", "json", "to", "headers"}, 0},
		{"pm.response.to.have.", []string{"status", "header", "jsonBody"}, 0},
		{"pm.response.to.be.", []string{"ok", "success", "clientError"}, 0},
		{"pm.request.url.query.", []string{"add", "upsert", "remove"}, 0},
		{"pm.cookies.jar().", []string{"get", "getAll", "set"}, 0},
		{"pm.execution.", []string{"setNextRequest", "skipRequest"}, 0},
		{"if (pm.expect(pm.response.", []string{"code", "json"}, 0},
		{"pm.expect(a).to.equal(pm.variables.", []string{"get", "set"}, 0},
	}
	for _, c := range cases {
		got, replace := pmLabels(t, c.before)
		if !contains(got, c.want...) || replace != c.replace {
			t.Errorf("%q -> %v (replace %d), want %v (replace %d)", c.before, got, replace, c.want, c.replace)
		}
	}
	nones := []string{"", "pm", "foo.", "xpm.", "pm.nope.", "pm.response.json()."}
	for _, none := range nones {
		if got, _ := pmLabels(t, none); len(got) != 0 {
			t.Errorf("%q offered %v", none, got)
		}
	}
}

func TestPMInsertTextFollowsTheMemberKind(t *testing.T) {
	items, _ := PM().Complete("pm.")
	insert := map[string]string{}
	for _, it := range items {
		insert[it.Label] = it.Insert
	}
	for label, want := range map[string]string{"environment": "environment.", "test": "test(", "expect": "expect("} {
		if insert[label] != want {
			t.Errorf("%s inserts %q, want %q", label, insert[label], want)
		}
	}
	items, _ = PM().Complete("pm.response.")
	for _, it := range items {
		if it.Label == "code" && it.Insert != "code" {
			t.Errorf("a property inserts %q", it.Insert)
		}
	}
}

func TestPMExpectSubjectsAndChaiChain(t *testing.T) {
	got, replace := pmLabels(t, "pm.expect(")
	if !contains(got, "pm.response.code", "pm.response.json()") || replace != 0 {
		t.Fatalf("subjects = %v replace %d", got, replace)
	}
	got, replace = pmLabels(t, "pm.expect(pm.response.code).to.")
	if !contains(got, "be", "have", "equal") || replace != 0 {
		t.Fatalf("chain = %v", got)
	}
	got, replace = pmLabels(t, "pm.expect(a).to.have.prop")
	if !contains(got, "property") || replace != 4 {
		t.Fatalf("chain prefix = %v replace %d", got, replace)
	}
	if got, _ := pmLabels(t, "pm.expect(a, 'x'."); len(got) != 0 {
		t.Fatalf("inside the arguments offered %v", got)
	}
}

func TestPMTableIsConsistent(t *testing.T) {
	for _, path := range PMPaths() {
		if !strings.HasPrefix(path, "pm") {
			t.Errorf("path %q does not start with pm", path)
		}
		spec := pmTable[path]
		for _, m := range parseMembers(spec) {
			if m.object {
				if _, ok := pmTable[path+"."+m.name]; !ok {
					t.Errorf("%s.%s is marked an object but has no entry", path, m.name)
				}
			}
		}
		if path != "pm" {
			parent := path[:strings.LastIndex(path, ".")]
			if !contains(PMMembers(parent), path[strings.LastIndex(path, ".")+1:]) {
				t.Errorf("%s is not a member of %s", path, parent)
			}
		}
	}
}

// docNoise are backticked words in the scripting document that are types,
// states or Go names rather than pm members.
var docNoise = map[string]bool{
	"PropertyList": true, "VariableList": true, "JSONError": true, "Result": true, "Request": true,
	"yes": true, "no": true, "partial": true, "no-op": true, "Cannot": true,
	"chai": true, "chai-postman": true, "Buffer": true, "Promise": true, "null": true,
	"try": true, "catch": true, "await": true, "res": true,
}

// TestPMTableCoversScriptingDoc keeps the completion table honest: every pm.*
// path and every member word in the compatibility tables of docs/scripting.md
// must exist in the table.
func TestPMTableCoversScriptingDoc(t *testing.T) {
	data, err := os.ReadFile("../../../docs/scripting.md")
	if err != nil {
		t.Skipf("docs/scripting.md not readable: %v", err)
	}
	doc := string(data)
	start := strings.Index(doc, "## Compatibility table")
	end := strings.Index(doc, "### Libraries")
	if start < 0 || end < start {
		t.Fatal("the compatibility table moved; update this test")
	}
	section := doc[start:end]

	members := map[string]bool{}
	for _, path := range PMPaths() {
		for _, m := range PMMembers(path) {
			members[m] = true
		}
	}
	for _, w := range strings.Fields(chaiWords) {
		members[strings.TrimRight(w, ".(")] = true
	}

	pathRE := regexp.MustCompile(`pm(?:\.[A-Za-z_][A-Za-z0-9_]*)+`)
	wordRE := regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)(?:\\(\\))?`")
	checked := 0
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if cells := strings.Split(line, "|"); len(cells) > 2 && strings.TrimSpace(cells[2]) == "no" {
			continue // documented as unavailable, so completion rightly omits it
		}
		for _, path := range pathRE.FindAllString(line, -1) {
			checked++
			segs := strings.Split(path, ".")
			cur := "pm"
			for i, seg := range segs[1:] {
				if !contains(PMMembers(cur), seg) {
					t.Errorf("documented %s: %q is not a member of %s", path, seg, cur)
					break
				}
				if i == len(segs)-2 {
					break
				}
				cur += "." + seg
			}
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 || !strings.Contains(cells[1], "pm.") {
			continue
		}
		for _, cell := range cells[1:] {
			for _, m := range wordRE.FindAllStringSubmatch(cell, -1) {
				word := m[1]
				if docNoise[word] || word == "pm" {
					continue
				}
				checked++
				if !members[word] {
					t.Errorf("documented member %q is missing from the pm table", word)
				}
			}
		}
	}
	if checked < 60 {
		t.Fatalf("only %d documented names were checked; the document format changed", checked)
	}
}
