package vimtext

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/ui"
)

type corpusCase struct {
	name       string
	opts       Options
	start      string
	row, col   int
	keys       string
	want       string
	wantRow    int
	wantCol    int
	wantMode   Mode
	wantEvents []EventKind
}

func TestKeystrokeCorpus(t *testing.T) {
	cases := []corpusCase{
		{name: "insert", start: "ac", keys: "lib<Esc>", want: "abc", wantCol: 1},
		{name: "append", start: "ac", keys: "a b<Esc>", want: "a bc", wantCol: 2},
		{name: "insert line start", start: "  x", col: 2, keys: "I!<Esc>", want: "  !x", wantCol: 2},
		{name: "append line end", start: "abc", keys: "A!<Esc>", want: "abc!", wantCol: 3},
		{name: "open below", start: "a", keys: "oB<Esc>", want: "a\nB", wantRow: 1},
		{name: "open above", start: "a", keys: "OA<Esc>", want: "A\na"},
		{name: "delete word", start: "one two", keys: "dw", want: "two"},
		{name: "change inner word", start: "one two", col: 5, keys: "ciwX<Esc>", want: "one X", wantCol: 4},
		{name: "delete line", start: "a\nb\nc", row: 1, keys: "dd", want: "a\nc", wantRow: 1},
		{name: "delete two lines", start: "a\nb\nc", keys: "2dd", want: "c"},
		{name: "change line", start: "abc", keys: "ccx<Esc>", want: "x"},
		{name: "delete tail", start: "abcd", col: 1, keys: "D", want: "a"},
		{name: "change tail", start: "abcd", col: 1, keys: "CX<Esc>", want: "aX", wantCol: 1},
		{name: "delete char", start: "abc", col: 1, keys: "x", want: "ac", wantCol: 1},
		{name: "delete before", start: "abc", col: 1, keys: "X", want: "bc"},
		{name: "substitute", start: "abc", keys: "sX<Esc>", want: "Xbc"},
		{name: "substitute line", start: "abc", keys: "SX<Esc>", want: "X"},
		{name: "replace", start: "abc", col: 1, keys: "rX", want: "aXc", wantCol: 1},
		{name: "join", start: "a\n  b", keys: "J", want: "a b", wantCol: 1},
		{name: "toggle case", start: "aBc", keys: "3~", want: "AbC", wantCol: 2},
		{name: "indent", start: "a", keys: ">>", want: "  a", wantCol: 2},
		{name: "outdent", start: "  a", keys: "<LT><LT>", want: "a"},
		{name: "undo insert session", start: "a", keys: "Axyz<Esc>u", want: "a"},
		{name: "redo insert session", start: "a", keys: "Axyz<Esc>u<C-r>", want: "axyz", wantCol: 3},
		{name: "dot append", start: "a\nb", keys: "A!<Esc>j.", want: "a!\nb!", wantRow: 1, wantCol: 1},
		{name: "dot delete line", start: "a\nb\nc", keys: "dd.", want: "c"},
		{name: "dot change inner word", start: "one two\nthree four", col: 1, keys: "ciwX<Esc>j.", want: "X two\nX four", wantRow: 1},
		{name: "word forward", start: "one two", keys: "w", want: "one two", wantCol: 4},
		{name: "WORD forward", start: "a.b c", keys: "W", want: "a.b c", wantCol: 4},
		{name: "word backward", start: "one two", col: 6, keys: "b", want: "one two", wantCol: 4},
		{name: "word end", start: "one two", keys: "e", want: "one two", wantCol: 2},
		{name: "line first nonblank", start: "  a", col: 2, keys: "^", want: "  a", wantCol: 2},
		{name: "line end", start: "abc", keys: "$", want: "abc", wantCol: 2},
		{name: "go bottom", start: "a\nb\nc", keys: "G", want: "a\nb\nc", wantRow: 2},
		{name: "go top", start: "a\nb", row: 1, keys: "gg", want: "a\nb"},
		{name: "go counted line", start: "a\nb\nc", keys: "2G", want: "a\nb\nc", wantRow: 1},
		{name: "find", start: "abca", keys: "fa", want: "abca", wantCol: 3},
		{name: "find backward", start: "abca", col: 3, keys: "Fb", want: "abca", wantCol: 1},
		{name: "till", start: "abca", keys: "ta", want: "abca", wantCol: 2},
		{name: "operator find", start: "abc def", keys: "dfc", want: " def"},
		{name: "operator till", start: "abc def", keys: "dtc", want: "c def"},
		{name: "repeat find", start: "ababa", keys: "fb;", want: "ababa", wantCol: 3},
		{name: "reverse find", start: "ababa", keys: "fb;,", want: "ababa", wantCol: 1},
		{name: "match bracket", start: "(a)", keys: "%", want: "(a)", wantCol: 2},
		{name: "unmatched bracket", start: "(a", keys: "%", want: "(a"},
		{name: "inner quotes", start: `"abc"`, col: 2, keys: `di"`, want: `""`, wantCol: 1},
		{name: "around quotes", start: `"abc" x`, col: 2, keys: `da"`, want: "x"},
		{name: "nested parens", start: "(a(b)c)", col: 3, keys: "di(", want: "(a()c)", wantCol: 3},
		{name: "inner braces", start: "{abc}", col: 2, keys: "ci{X<Esc>", want: "{X}", wantCol: 1},
		{name: "inner brackets", start: "[abc]", col: 2, keys: "di[", want: "[]", wantCol: 1},
		{name: "inner backticks", start: "`abc`", col: 2, keys: "di`", want: "``", wantCol: 1},
		{name: "around WORD", start: "a.b  c", col: 1, keys: "daW", want: "c"},
		{name: "visual delete", start: "abcd", keys: "vlld", want: "d"},
		{name: "visual swap anchor", start: "abcd", col: 2, keys: "vhol", want: "abcd", wantCol: 3, wantMode: Visual},
		{name: "visual line delete", start: "a\nb\nc", keys: "Vjd", want: "c"},
		{name: "visual indent", start: "a\nb", keys: "Vj>", want: "  a\n  b", wantRow: 1},
		{name: "insert backspace", start: "a", keys: "Axy<BS><Esc>", want: "ax", wantCol: 1},
		{name: "insert delete", start: "ab", keys: "i<Del><Esc>", want: "b"},
		{name: "insert ctrl word", start: "", keys: "ione two<C-w><Esc>", want: "one ", wantCol: 3},
		{name: "insert ctrl line", start: "a", keys: "A xyz<C-u><Esc>", want: ""},
		{name: "insert tab", start: "", keys: "i<Tab><Esc>", want: "  ", wantCol: 1},
		{name: "paste unicode", start: "", keys: "i<Paste:é界👩‍💻><Esc>", want: "é界👩‍💻", wantCol: 2},
		{name: "unicode motion", start: "é界👩‍💻x", keys: "lll", want: "é界👩‍💻x", wantCol: 3},
		{name: "single line paste", opts: Options{SingleLine: true}, start: "", keys: "i<Paste:a\nb><Esc>", want: "a b", wantCol: 2},
		{name: "single line line delete", opts: Options{SingleLine: true}, start: "abc", keys: "dd", want: ""},
		{name: "readonly edit no-op", opts: Options{ReadOnly: true}, start: "abc", keys: "ixdd", want: "abc"},
		{name: "counted word operator", start: "a b c d", keys: "d3w", want: "d"},
		{name: "WORD backward", start: "a.b c", col: 4, keys: "B", want: "a.b c"},
		{name: "WORD end", start: "a.b c", keys: "E", want: "a.b c", wantCol: 2},
		{name: "previous word end", start: "one two", col: 4, keys: "ge", want: "one two", wantCol: 2},
		{name: "previous WORD end", start: "a.b c", col: 4, keys: "gE", want: "a.b c", wantCol: 2},
		{name: "arrow motions", start: "abc\ndef", keys: "<Right><Down><Left><Up>", want: "abc\ndef"},
		{name: "insert arrows", start: "ab", keys: "i<Right>X<Esc>", want: "aXb", wantCol: 1},
		{name: "put after character", start: "abc", keys: "xp", want: "bac", wantCol: 1},
		{name: "put before character", start: "abc", keys: "xP", want: "abc"},
		{name: "put line after", start: "a\nb", keys: "yyp", want: "a\na\nb", wantRow: 1},
		{name: "put line before", start: "a\nb", row: 1, keys: "yyP", want: "a\nb\nb", wantRow: 1},
		{name: "Y yanks whole line", start: "a\nb", keys: "Yp", want: "a\na\nb", wantRow: 1},
		{name: "search forward", start: "one two", keys: "/two<CR>", want: "one two", wantCol: 4},
		{name: "search backward", start: "one two one", col: 11, keys: "?two<CR>", want: "one two one", wantCol: 4},
		{name: "next search", start: "one two one two", keys: "/two<CR>n", want: "one two one two", wantCol: 12},
		{name: "previous search", start: "one two one two", keys: "/two<CR>nN", want: "one two one two", wantCol: 4},
		{name: "star search", start: "one two one", keys: "*", want: "one two one", wantCol: 8},
		{name: "around word", start: "one  two", keys: "daw", want: "two"},
		{name: "inner single quote", start: "'one'", col: 2, keys: "di'", want: "''", wantCol: 1},
		{name: "around single quote", start: "'one' x", col: 2, keys: "da'", want: "x"},
		{name: "inner paren alias", start: "(one)", col: 2, keys: "dib", want: "()", wantCol: 1},
		{name: "around paren alias", start: "(one) x", col: 2, keys: "dab", want: " x"},
		{name: "inner brace alias", start: "{one}", col: 2, keys: "diB", want: "{}", wantCol: 1},
		{name: "around brace alias", start: "{one} x", col: 2, keys: "daB", want: " x"},
		{name: "around bracket", start: "[one] x", col: 2, keys: "da[", want: " x"},
		{name: "visual yank", start: "abc", keys: "vly", want: "abc"},
		{name: "visual x", start: "abc", keys: "vlx", want: "c"},
		{name: "visual change", start: "abc", keys: "vlcX<Esc>", want: "Xc"},
		{name: "visual outdent", start: "  a\n  b", keys: "Vj<LT>", want: "a\nb", wantRow: 1},
		{name: "visual toggle", start: "Ab", keys: "vl~", want: "aB", wantCol: 1},
		{name: "visual join", start: "a\nb", keys: "VjJ", want: "a b", wantCol: 1},
		{name: "visual find motion", start: "abcde", keys: "vfd", want: "abcde", wantCol: 3, wantMode: Visual},
		{name: "visual gg motion", start: "a\nb\nc", row: 2, keys: "vgg", want: "a\nb\nc", wantMode: Visual},
		{name: "single line vertical no-op", opts: Options{SingleLine: true}, start: "abc", keys: "jk", want: "abc"},
		{name: "readonly yank", opts: Options{ReadOnly: true}, start: "abc", keys: "yy", want: "abc"},
	}
	cases = append(cases, reviewerRegressionCases()...)
	for n := 1; n <= 50; n++ {
		cases = append(cases, corpusCase{
			name: "counted right " + fmt.Sprint(n), start: strings.Repeat("x", 51), keys: fmt.Sprintf("%dl", n),
			want: strings.Repeat("x", 51), wantCol: n,
		})
	}
	for n := 1; n <= 40; n++ {
		cases = append(cases, corpusCase{
			name: "counted left " + fmt.Sprint(n), start: strings.Repeat("x", 41), col: 40, keys: fmt.Sprintf("%dh", n),
			want: strings.Repeat("x", 41), wantCol: 40 - n,
		})
	}
	for n := 1; n <= 50; n++ {
		ch := string(rune('a' + n%26))
		cases = append(cases, corpusCase{
			name: "insert corpus " + fmt.Sprint(n), start: "z", keys: "i" + ch + "<Esc>",
			want: ch + "z",
		})
	}
	if len(cases) < 150 {
		t.Fatalf("corpus has %d cases, want at least 150", len(cases))
	}
	t.Logf("corpus cases: %d", len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(ui.NewTheme(true, true, false), tc.opts)
			m.SetValue(tc.start)
			m.SetCursor(tc.row, tc.col)
			_ = m.Focus()
			var events []EventKind
			for _, msg := range parseKeys(t, tc.keys) {
				_, event := m.Update(msg)
				if event.Kind != None {
					events = append(events, event.Kind)
				}
			}
			if got := m.Value(); got != tc.want {
				t.Fatalf("value mismatch (lengths got=%d want=%d)", len(got), len(tc.want))
			}
			row, col := m.Cursor()
			if row != tc.wantRow || col != tc.wantCol {
				t.Fatalf("cursor = (%d,%d), want (%d,%d)", row, col, tc.wantRow, tc.wantCol)
			}
			if m.Mode() != tc.wantMode {
				t.Fatalf("mode = %s, want %s", m.Mode(), tc.wantMode)
			}
			if tc.wantEvents != nil && fmt.Sprint(events) != fmt.Sprint(tc.wantEvents) {
				t.Fatalf("events = %v, want %v", events, tc.wantEvents)
			}
		})
	}
}

func parseKeys(t *testing.T, notation string) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	for notation != "" {
		if notation[0] != '<' {
			cluster, rest, _, _ := uniseg.FirstGraphemeClusterInString(notation, -1)
			r, _ := utf8.DecodeRuneInString(cluster)
			out = append(out, tea.KeyPressMsg(tea.Key{Code: r, Text: cluster}))
			notation = rest
			continue
		}
		end := strings.IndexByte(notation, '>')
		if end < 0 {
			t.Fatal("unterminated key notation")
		}
		token := notation[1:end]
		notation = notation[end+1:]
		if strings.HasPrefix(token, "Paste:") {
			out = append(out, tea.PasteMsg{Content: strings.TrimPrefix(token, "Paste:")})
			continue
		}
		keys := map[string]tea.Key{
			"Esc": {Code: tea.KeyEscape}, "CR": {Code: tea.KeyEnter},
			"Tab": {Code: tea.KeyTab}, "S-Tab": {Code: tea.KeyTab, Mod: tea.ModShift},
			"BS": {Code: tea.KeyBackspace}, "Del": {Code: tea.KeyDelete},
			"Up": {Code: tea.KeyUp}, "Down": {Code: tea.KeyDown},
			"Left": {Code: tea.KeyLeft}, "Right": {Code: tea.KeyRight},
			"C-r": {Code: 'r', Mod: tea.ModCtrl}, "C-w": {Code: 'w', Mod: tea.ModCtrl},
			"C-u": {Code: 'u', Mod: tea.ModCtrl}, "C-c": {Code: 'c', Mod: tea.ModCtrl},
			"LT": {Code: '<', Text: "<"},
		}
		key, ok := keys[token]
		if !ok {
			t.Fatalf("unknown key notation <%s>", token)
		}
		out = append(out, tea.KeyPressMsg(key))
	}
	return out
}
