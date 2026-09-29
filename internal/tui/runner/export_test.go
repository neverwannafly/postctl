package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/engine"
)

func TestExportUsesSharedJSONAndJUnitReporters(t *testing.T) {
	for _, tc := range []struct {
		ext, contains string
	}{
		{".json", `"type":"run-finished"`},
		{".xml", `<testsuites`},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			m := New(testEnv(80, 24), Deps{})
			sum := &engine.RunSummary{Iterations: 1}
			m.summary = sum
			m.runEvents = []engine.RunEvent{
				engine.RunStarted{CollectionName: "Demo", Iterations: 1},
				engine.IterationStarted{Iteration: 0, Iterations: 1},
				engine.RunFinished{Summary: sum},
			}
			path := filepath.Join(t.TempDir(), "run"+tc.ext)
			m.exportPath.Set(path)
			msg := m.confirmExport()()
			done, ok := msg.(exportDoneMsg)
			if !ok || done.err != nil {
				t.Fatalf("export = %#v", msg)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tc.contains) {
				t.Fatalf("export does not contain %q:\n%s", tc.contains, raw)
			}
		})
	}
}

func TestExportFormatToggleChangesExtension(t *testing.T) {
	m := New(testEnv(80, 24), Deps{})
	m.summary = &engine.RunSummary{}
	m.beginExport()
	if _, cmd := m.exportKey(tea.KeyPressMsg{Code: tea.KeyTab}); cmd != nil {
		t.Fatal("toggle returned a command")
	}
	if !m.exportXML || filepath.Ext(m.exportPath.Text()) != ".xml" {
		t.Fatalf("format xml=%v path=%q", m.exportXML, m.exportPath.Text())
	}
}

func TestFailuresOnlyViewDoesNotRewriteRows(t *testing.T) {
	m := New(testEnv(80, 24), Deps{})
	m.phase = phLive
	m.failuresOnly = true
	m.rows = []row{
		{id: "ok", name: "OK", state: stDone},
		{id: "bad", name: "Bad", state: stDone, errText: "failed"},
	}
	before := append([]row(nil), m.rows...)
	_ = m.View()
	if len(m.rows) != len(before) || m.rows[0].id != before[0].id || m.rows[1].id != before[1].id {
		t.Fatalf("View mutated rows: %#v", m.rows)
	}
}
