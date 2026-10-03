package vimtext

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

func TestRouting(t *testing.T) {
	keys := []tea.Key{
		{Code: 's', Mod: tea.ModCtrl}, {Code: tea.KeyEnter, Mod: tea.ModCtrl},
		{Code: 'p', Mod: tea.ModCtrl}, {Code: 'c', Mod: tea.ModCtrl},
		{Code: 'h', Mod: tea.ModCtrl}, {Code: 'j', Mod: tea.ModCtrl},
		{Code: 'k', Mod: tea.ModCtrl}, {Code: 'l', Mod: tea.ModCtrl},
		{Code: 'o', Mod: tea.ModCtrl}, {Code: 'i', Mod: tea.ModCtrl},
		{Code: tea.KeyTab}, {Code: tea.KeyTab, Mod: tea.ModShift},
		{Code: ' ', Text: " "}, {Code: ':', Text: ":"}, {Code: 'q', Text: "q"},
		{Code: 'z', Mod: tea.ModAlt},
	}
	for _, key := range keys {
		t.Run(key.String(), func(t *testing.T) {
			m := New(ui.NewTheme(true, true, false), Options{})
			m.SetValue("abc")
			_ = m.Focus()
			_, _ = m.Update(runeKey('2'))
			_, event := m.Update(tea.KeyPressMsg(key))
			if event.Kind != Unhandled || event.Key.String() != key.String() {
				t.Fatalf("event = %#v", event)
			}
			if m.Pending() != "" {
				t.Fatalf("pending state was not cleared: %q", m.Pending())
			}
		})
	}
	for _, suffix := range []string{"u", "b", "H"} {
		t.Run("g"+suffix, func(t *testing.T) {
			m := New(ui.NewTheme(true, true, false), Options{})
			m.SetValue("abc")
			_ = m.Focus()
			_, _ = m.Update(runeKey('g'))
			r, _ := []rune(suffix), suffix
			_, event := m.Update(tea.KeyPressMsg(tea.Key{Code: r[0], Text: suffix}))
			if event.Kind != Seq || event.Seq != "g"+suffix || m.Pending() != "" {
				t.Fatalf("event=%#v pending=%q", event, m.Pending())
			}
		})
	}
}

func TestInsertAndVisualRouting(t *testing.T) {
	for _, mode := range []Mode{Insert, Visual, VisualLine} {
		for _, key := range []byte{'c', 'h', 'j', 'k', 'l'} {
			m := New(ui.NewTheme(true, true, false), Options{})
			m.SetValue("abc")
			_ = m.Focus()
			m.mode = mode
			_, event := m.Update(tea.KeyPressMsg(tea.Key{Code: rune(key), Mod: tea.ModCtrl}))
			if key == 'c' {
				if event.Kind != None || m.Mode() != Normal {
					t.Fatalf("%s ctrl-c was not Escape", mode)
				}
			} else if mode == Insert {
				if event.Kind != Unhandled {
					t.Fatalf("insert ctrl-%c event = %v", key, event.Kind)
				}
			} else if event.Kind != Unhandled {
				t.Fatalf("%s ctrl-%c event = %v", mode, key, event.Kind)
			}
		}
	}
}

func TestClipboardIsRedactedForDeleteAndReadOnlyYank(t *testing.T) {
	const marker = "MASK_ME"
	var received []string
	clipboard := func(text string) tea.Cmd {
		received = append(received, text)
		return func() tea.Msg { return nil }
	}
	redact := func(text string) string { return strings.ReplaceAll(text, marker, "****") }
	for _, tc := range []struct {
		name     string
		readOnly bool
		keys     string
	}{
		{"delete", false, "dd"},
		{"readonly visual yank", true, "Vy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received = nil
			m := New(ui.NewTheme(true, true, false), Options{
				ReadOnly: tc.readOnly, Clipboard: clipboard, Redact: redact,
			})
			m.SetValue(marker)
			_ = m.Focus()
			for _, msg := range parseKeys(t, tc.keys) {
				_, _ = m.Update(msg)
			}
			if len(received) != 1 || strings.Contains(received[0], marker) {
				t.Fatalf("clipboard received unredacted text or wrong call count: %d", len(received))
			}
		})
	}
}

type fakeCompleter struct{}

func (fakeCompleter) Complete(before string) ([]Item, int) {
	if strings.HasSuffix(before, "pr") {
		return []Item{
			{Label: "print", Detail: "function", Insert: "print"},
			{Label: "printf", Detail: "function", Insert: "printf"},
		}, 2
	}
	return nil, 0
}

func TestCompletionNavigationAcceptAndEscapeLadder(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{StartInsert: true, Completer: fakeCompleter{}})
	_ = m.Focus()
	for _, msg := range parseKeys(t, "pr") {
		_, _ = m.Update(msg)
	}
	if len(m.completions) != 2 || m.completeSel != 0 {
		t.Fatal("completion popup did not open")
	}
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModCtrl}))
	if m.completeSel != 1 {
		t.Fatal("Ctrl-n did not navigate")
	}
	_, event := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if event.Kind != Changed || m.Value() != "printf" {
		t.Fatalf("accepted value length=%d event=%v", len(m.Value()), event.Kind)
	}

	m.SetValue("")
	m.mode = Insert
	for _, msg := range parseKeys(t, "pr") {
		_, _ = m.Update(msg)
	}
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.Mode() != Insert || len(m.completions) != 0 {
		t.Fatal("first Escape should close completion")
	}
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.Mode() != Normal {
		t.Fatal("second Escape should leave insert")
	}
}

func TestSingleLineSubmitAndRestrictions(t *testing.T) {
	for _, startInsert := range []bool{false, true} {
		m := New(ui.NewTheme(true, true, false), Options{SingleLine: true, StartInsert: startInsert})
		m.SetValue("a")
		_ = m.Focus()
		_, event := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		if event.Kind != Submit {
			t.Fatalf("startInsert=%v event=%v", startInsert, event.Kind)
		}
	}
	m := New(ui.NewTheme(true, true, false), Options{SingleLine: true, StartInsert: true})
	_ = m.Focus()
	_, event := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if event.Kind != Unhandled {
		t.Fatalf("single-line Tab event=%v", event.Kind)
	}
}

func TestReadOnlyNeverChanges(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{ReadOnly: true})
	m.SetValue("abc")
	_ = m.Focus()
	for _, msg := range parseKeys(t, "iAxDCSrxpP>><LT><LT>") {
		_, event := m.Update(msg)
		if event.Kind == Changed {
			t.Fatal("read-only editor emitted Changed")
		}
	}
	if m.Value() != "abc" {
		t.Fatal("read-only editor changed")
	}
}

func TestViewIsPureInEveryMode(t *testing.T) {
	for _, mode := range []Mode{Normal, Insert, Visual, VisualLine} {
		t.Run(mode.String(), func(t *testing.T) {
			m := New(ui.NewTheme(true, false, false), Options{LineNumbers: true})
			m.SetValue(strings.Repeat("long line\n", 12))
			m.SetSize(12, 4)
			m.SetCursor(8, 5)
			_ = m.Focus()
			m.mode = mode
			m.anchor = position{7, 1}
			m.opts.Clipboard, m.opts.Redact = nil, nil
			before := *m
			first, second := m.View(), m.View()
			if first != second || !reflect.DeepEqual(before, *m) {
				t.Fatal("View mutated the model")
			}
		})
	}
}
