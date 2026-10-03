package vimtext

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

	m.SetValue("")
	m.mode = Insert
	for _, msg := range parseKeys(t, "pr") {
		_, _ = m.Update(msg)
	}
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if m.Mode() != Insert || len(m.completions) != 0 {
		t.Fatal("Ctrl-C should close completion before leaving insert")
	}
}

func TestCompletionPopupMovesAboveAndAlignsToCursor(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{StartInsert: true, Completer: fakeCompleter{}})
	m.SetValue("a\nb\nc\npr")
	m.SetSize(20, 4)
	m.SetCursor(3, 2)
	_ = m.Focus()
	m.refreshCompletion()
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[1], "print") || strings.Contains(lines[3], "print") {
		t.Fatal("bottom-row completion was not placed above the cursor")
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Fatal("completion popup was not aligned to the cursor column")
	}
}

func TestPendingRoutingAndShortViewportStability(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue("a\nb\nc\nd")
	_ = m.Focus()
	_, _ = m.Update(runeKey('g'))
	_, event := m.Update(runeKey(' '))
	if event.Kind != Seq || event.Seq != "g " || m.Pending() != "" {
		t.Fatalf("g+Space event=%#v pending=%q", event, m.Pending())
	}
	m.mode = Visual
	_, _ = m.Update(runeKey('2'))
	_, event = m.Update(runeKey('q'))
	if event.Kind != Unhandled || m.Pending() != "" {
		t.Fatalf("visual unhandled event=%#v pending=%q", event, m.Pending())
	}
	for height := 1; height <= 4; height++ {
		m.SetSize(8, height)
		m.SetCursor(3, 0)
		top := m.top
		for range 5 {
			m.ensureVisible()
			if m.top != top {
				t.Fatalf("height %d scroll offset oscillated", height)
			}
		}
	}
}

func TestEnsureVisibleIncludesCursorCell(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue("界a")
	m.SetSize(2, 1)
	m.SetCursor(0, 1)
	if m.left != 1 {
		t.Fatalf("wide cursor cell clipped: left=%d", m.left)
	}
	m.mode = Insert
	m.SetCursor(0, 2)
	if m.left == 0 {
		t.Fatal("end-of-line cursor cell clipped")
	}
}

func TestEnsureVisiblePastEOLUsesCellWidths(t *testing.T) {
	th := ui.NewTheme(true, true, false)
	m := New(th, Options{SingleLine: true, StartInsert: true})
	m.SetValue("界a")
	m.SetSize(3, 1)
	m.SetCursor(0, 2)
	if m.left == 0 {
		t.Fatal("EOL scroll used grapheme count; cursor hidden at exact cell fit")
	}
	if !strings.Contains(m.View(), " ") {
		t.Fatalf("expected visible insert cursor at EOL, got:\n%s", m.View())
	}
	m.mode = Normal
	m.SetCursor(0, 2)
	m.ensureVisible()
	if m.left == 0 {
		t.Fatal("normal mode EOL scroll should use cell widths")
	}
}

func TestUndoRedoHistoryIsBounded(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{})
	for i := range historyLimit + 25 {
		m.pushUndo(snapshot{lines: []string{fmt.Sprint(i)}})
		m.pushRedo(snapshot{lines: []string{fmt.Sprint(i)}})
	}
	if len(m.undo) != historyLimit || len(m.redo) != historyLimit {
		t.Fatalf("history sizes undo=%d redo=%d", len(m.undo), len(m.redo))
	}
	if m.undo[0].lines[0] != "25" || m.redo[0].lines[0] != "25" {
		t.Fatal("history did not drop the oldest snapshots")
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

func TestEnsureVisibleLineFitsWithCursorOnCharacter(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{SingleLine: true})
	m.SetValue("1234567890")
	m.SetSize(10, 1)
	_ = m.Focus()
	m.mode = Normal
	m.col = 9
	m.ensureVisible()
	if m.left != 0 {
		t.Fatalf("left=%d want 0 when line fits with cursor on last char", m.left)
	}
}

func TestEnsureVisiblePastEndNeedsRoomForCursor(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{SingleLine: true})
	m.SetValue("1234567890")
	m.SetSize(10, 1)
	_ = m.Focus()
	m.mode = Insert
	m.col = 10
	m.ensureVisible()
	if m.left == 0 {
		t.Fatal("insert at EOL on full-width line should scroll")
	}
}

func TestMaskKeepsCursorColumnsAligned(t *testing.T) {
	th := ui.NewTheme(true, true, false)
	m := New(th, Options{SingleLine: true, Mask: th.Glyphs.Mask})
	const secret = "abcdefghijklmnopqrstuvwxyz012345"
	m.SetValue(secret)
	m.SetSize(10, 1)
	_ = m.Focus()
	m.SetCursor(0, 15)
	view := m.View()
	if strings.Contains(view, secret) {
		t.Fatal("masked view shows secret")
	}
	if !strings.Contains(view, th.Glyphs.Mask) {
		t.Fatal("masked view shows no mask glyph")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	_, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	vis := m.View()
	if strings.Contains(vis, secret) {
		t.Fatal("visual selection leaked secret")
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
			before := deepCopyModel(m)
			first, second := m.View(), m.View()
			if first != second || !reflect.DeepEqual(before, *m) {
				t.Fatal("View mutated the model")
			}
		})
	}
}

func deepCopyModel(m *Model) Model {
	out := *m
	out.lines = append([]string(nil), m.lines...)
	out.completions = append([]Item(nil), m.completions...)
	out.changeKeys = append([]tea.Msg(nil), m.changeKeys...)
	out.lastChange = append([]tea.Msg(nil), m.lastChange...)
	if m.highlights != nil {
		out.highlights = make([][]Span, len(m.highlights))
		for i := range m.highlights {
			out.highlights[i] = append([]Span(nil), m.highlights[i]...)
		}
	}
	copySnapshots := func(in []snapshot) []snapshot {
		if in == nil {
			return nil
		}
		result := make([]snapshot, len(in))
		for i := range in {
			result[i] = in[i]
			result[i].lines = append([]string(nil), in[i].lines...)
		}
		return result
	}
	out.undo = copySnapshots(m.undo)
	out.redo = copySnapshots(m.redo)
	if m.changeStart != nil {
		s := *m.changeStart
		s.lines = append([]string(nil), m.changeStart.lines...)
		out.changeStart = &s
	}
	return out
}

func TestMaskHidesTheBufferWithoutChangingIt(t *testing.T) {
	m := New(ui.NewTheme(true, true, false), Options{SingleLine: true})
	m.SetValue("sentinel-text")
	m.SetSize(20, 1)
	_ = m.Focus()
	m.SetMask("•")
	view := ansi.Strip(m.View())
	if strings.Contains(view, "sentinel") || !strings.Contains(view, "••••") {
		t.Fatalf("masked view leaked or lost its glyphs: %q", view)
	}
	if m.Value() != "sentinel-text" {
		t.Fatal("masking changed the buffer")
	}
	m.SetMask("")
	if !strings.Contains(ansi.Strip(m.View()), "sentinel") {
		t.Fatal("unmasked view lost the text")
	}
}
