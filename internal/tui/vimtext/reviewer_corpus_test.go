package vimtext

import (
	"fmt"
	"strings"
)

// reviewerRegressionCases pins every behavior reported by the two UX-V1
// reviewers. The generated count matrices intentionally exercise distinct
// counts as independent permanent corpus entries.
func reviewerRegressionCases() []corpusCase {
	cases := []corpusCase{
		// A1: indentation operators never delete their motion/object range.
		{name: "review A1 indent word", start: "foo bar", keys: ">w", want: "  foo bar"},
		{name: "review A1 indent down", start: "a\nb\nc", keys: ">j", want: "  a\n  b\nc"},
		{name: "review A1 outdent down", start: "  a\n  b\nc", keys: "<LT>j", want: "a\nb\nc"},
		{name: "review A1 indent inner word", start: "foo bar", col: 1, keys: ">iw", want: "  foo bar", wantCol: 1},

		// A2: empty inner objects are valid empty ranges.
		{name: "review A2 delete empty parens", start: "f()", col: 1, keys: "di(", want: "f()", wantCol: 2},
		{name: "review A2 change empty parens", start: "()", keys: "ci(X<Esc>", want: "(X)", wantCol: 1},
		{name: "review A2 change empty quotes", start: `a "" b`, col: 3, keys: `ci"X<Esc>`, want: `a "X" b`, wantCol: 3},
		{name: "review A2 delete empty brackets", start: "x[]", col: 1, keys: "di[", want: "x[]", wantCol: 2},
		{name: "review A2 change empty braces", start: "{}", keys: "ci}X<Esc>", want: "{X}", wantCol: 1},

		// A3/B1/B2/B3/B4: Vim word-motion boundaries.
		{name: "review A3 w crosses line to start", start: "foo\nbar baz", keys: "w", want: "foo\nbar baz", wantRow: 1},
		{name: "review A3 dw before newline", start: "a \nb", col: 1, keys: "dw", want: "a\nb"},
		{name: "review A3 empty line stops w", start: "a\n\nb", keys: "w", want: "a\n\nb", wantRow: 1},
		{name: "review A3 empty line stops b", start: "a\n\nb", row: 2, keys: "b", want: "a\n\nb", wantRow: 1},
		{name: "review A3 w last word clamps", start: "foo bar", col: 4, keys: "w", want: "foo bar", wantCol: 6},
		{name: "review B1 e advances from short word end", start: "a b c", keys: "e", want: "a b c", wantCol: 2},
		{name: "review B1 e advances from word end", start: "foo bar", col: 2, keys: "e", want: "foo bar", wantCol: 6},
		{name: "review B1 counted e advances", start: "a b c d", keys: "2e", want: "a b c d", wantCol: 4},
		{name: "review B2 ge previous end", start: "foo bar", col: 5, keys: "ge", want: "foo bar", wantCol: 2},
		{name: "review B2 counted ge", start: "one two three", col: 10, keys: "2ge", want: "one two three", wantCol: 2},
		{name: "review B3 dw last word line", start: "foo bar\nbaz", col: 4, keys: "dw", want: "foo \nbaz", wantCol: 3},
		{name: "review B3 dw first line", start: "foo\nbar", keys: "dw", want: "\nbar"},
		{name: "review B3 cw nonblank", start: "foo bar", keys: "cwX<Esc>", want: "X bar"},
		{name: "review B4 cw inside word", start: "foo bar", col: 2, keys: "cwX<Esc>", want: "foX bar", wantCol: 2},

		// A4/B5/B6: linewise edits and cursor clamping.
		{name: "review A4 visual line delete undo", start: "a\nb\nc", keys: "Vjdu", want: "a\nb\nc"},
		{name: "review A4 visual line x undo", start: "a\nb\nc", keys: "Vjxu", want: "a\nb\nc"},
		{name: "review A4 visual line change undo", start: "a\nb\nc", keys: "VjcX<Esc>u", want: "a\nb\nc", wantRow: 1},
		{name: "review B5 cc preserves line", start: "a\nb\nc", row: 1, keys: "ccX<Esc>", want: "a\nX\nc", wantRow: 1},
		{name: "review B5 S preserves line", start: "a\nb\nc", row: 1, keys: "SX<Esc>", want: "a\nX\nc", wantRow: 1},
		{name: "review B5 visual line c preserves line", start: "a\nb\nc", row: 1, keys: "VcX<Esc>", want: "a\nX\nc", wantRow: 1},
		{name: "review B6 d dollar clamp", start: "abc", col: 1, keys: "d$", want: "a"},
		{name: "review B6 daw clamp", start: "x foo", col: 2, keys: "daw", want: "x"},
		{name: "review B6 visual delete clamp", start: "abc\ndef", keys: "vjd", want: "ef"},

		// B7: curswant and display columns.
		{name: "review B7 dollar curswant", start: "abcdef\nab\nabcdefgh", keys: "$jj", want: "abcdef\nab\nabcdefgh", wantRow: 2, wantCol: 7},
		{name: "review B7 zero resets curswant", start: "abcdef\nab\nabcdefgh", keys: "$j0j", want: "abcdef\nab\nabcdefgh", wantRow: 2},
		{name: "review B7 caret resets curswant", start: "abcdef\n  a\n    z", keys: "$j^j", want: "abcdef\n  a\n    z", wantRow: 2, wantCol: 2},
		{name: "review B7 display column wide", start: "世界abc\nabcdef", col: 2, keys: "j", want: "世界abc\nabcdef", wantRow: 1, wantCol: 4},

		// B8/B9: counts and dot-repeat retain the original change.
		{name: "review B8 counted X", start: "abcdef", col: 5, keys: "3X", want: "abf", wantCol: 2},
		{name: "review B8 counted substitute", start: "abcdef", keys: "3sX<Esc>", want: "Xdef"},
		{name: "review B8 counted replace", start: "abcdef", keys: "3rX", want: "XXXdef", wantCol: 2},
		{name: "review B8 counted put", start: "abcd", keys: "x3p", want: "baaacd", wantCol: 3},
		{name: "review B8 counted join", start: "a\nb\nc\nd", keys: "3J", want: "a b c\nd", wantCol: 3},
		{name: "review B8 counted insert", start: "a", keys: "3iX<Esc>", want: "XXXa", wantCol: 2},
		{name: "review B8 counted append", start: "a", keys: "3AX<Esc>", want: "aXXX", wantCol: 3},
		{name: "review B8 counted open", start: "a", keys: "3oX<Esc>", want: "a\nX\nX\nX", wantRow: 3},
		{name: "review B8 dot counted x", start: "abcdefgh", keys: "3x.", want: "gh"},
		{name: "review B8 dot counted dd", start: "a\nb\nc\nd\ne", keys: "2dd.", want: "e"},
		{name: "review B8 dot operator count", start: "a b c d e", keys: "d2w.", want: "e"},
		{name: "review B8 counted around word", start: "one two three", keys: "d2aw", want: "three"},
		{name: "review B8 counted inner parens", start: "((x))", col: 2, keys: "d2i(", want: "()", wantCol: 1},
		{name: "review B9 dot replace", start: "abcd", keys: "rzl.", want: "zzcd", wantCol: 1},
		{name: "review B9 dot visual delete", start: "abcdef", keys: "vldl.", want: "cf", wantCol: 1},
		{name: "review B9 dot visual change", start: "abcdef", keys: "vlcX<Esc>l.", want: "XXef", wantCol: 1},

		// B10: same-line search wrap.
		{name: "review B10 forward own line wrap", start: "foo bar foo", col: 8, keys: "/foo<CR>", want: "foo bar foo"},
		{name: "review B10 backward own line wrap", start: "foo bar foo", keys: "?foo<CR>", want: "foo bar foo", wantCol: 8},
		{name: "review B10 star own line wrap", start: "foo bar foo", col: 8, keys: "*", want: "foo bar foo"},
		{name: "review B10 n own line wrap", start: "foo bar foo", keys: "/foo<CR>n", want: "foo bar foo"},
		{name: "review B10 N own line wrap", start: "foo bar foo", keys: "/foo<CR>N", want: "foo bar foo"},
		{name: "review B10 single line search wrap", opts: Options{SingleLine: true}, start: "foo bar foo", col: 8, keys: "/foo<CR>", want: "foo bar foo"},

		// B11/B12: text objects and matching pairs.
		{name: "review B11 ciw whitespace", start: "a b", col: 1, keys: "ciwX<Esc>", want: "aXb", wantCol: 1},
		{name: "review B11 quote containing cursor", start: `"a" "b"`, col: 2, keys: `ci"X<Esc>`, want: `"X" "b"`, wantCol: 1},
		{name: "review B11 quote next pair", start: `say "hi" ok`, keys: `ci"X<Esc>`, want: `say "X" ok`, wantCol: 5},
		{name: "review B11 around quote trailing space", start: `x "a" y`, col: 3, keys: `da"`, want: "x y", wantCol: 2},
		{name: "review B11 around quote leading space", start: `x "a"`, col: 3, keys: `da"`, want: "x"},
		{name: "review B11 around word leading fallback", start: "x foo.bar", col: 2, keys: "daw", want: "x.bar", wantCol: 1},
		{name: "review B11 i close paren alias", start: "(abc)", col: 4, keys: "di)", want: "()", wantCol: 1},
		{name: "review B11 a close paren alias", start: "(abc) x", col: 4, keys: "da)", want: " x"},
		{name: "review B11 i close brace alias", start: "{abc}", col: 4, keys: "di}", want: "{}", wantCol: 1},
		{name: "review B11 a close brace alias", start: "{abc} x", col: 4, keys: "da}", want: " x"},
		{name: "review B11 i close bracket alias", start: "[abc]", col: 4, keys: "di]", want: "[]", wantCol: 1},
		{name: "review B11 a close bracket alias", start: "[abc] x", col: 4, keys: "da]", want: " x"},
		{name: "review B11 multiline braces", start: "{\n  a\n}", row: 1, col: 1, keys: "ci{X<Esc>", want: "{\n  X\n}", wantRow: 1, wantCol: 2},
		{name: "review B11 multiline parens", start: "f(\n a\n)", row: 1, col: 1, keys: "di(", want: "f(\n)", wantRow: 1},
		{name: "review B12 percent multiline", start: "{\n a\n}", keys: "%", want: "{\n a\n}", wantRow: 2},
		{name: "review B12 percent searches line", start: "x (a)", keys: "%", want: "x (a)", wantCol: 4},

		// B13/B14: operator motions and single-line behavior.
		{name: "review B13 dj linewise", start: "a\nb\nc", keys: "dj", want: "c"},
		{name: "review B13 dk linewise", start: "a\nb\nc", row: 1, keys: "dk", want: "c"},
		{name: "review B13 dG linewise", start: "a\nb\nc", row: 1, keys: "dG", want: "a"},
		{name: "review B13 dgg linewise", start: "a\nb\nc", row: 2, keys: "dgg", want: ""},
		{name: "review B13 backward find inclusive target", start: "abcd", col: 3, keys: "dFb", want: "ad", wantCol: 1},
		{name: "review B13 backward till exclusive cursor", start: "abcd", col: 3, keys: "dTb", want: "abd", wantCol: 2},
		{name: "review B13 percent inclusive", start: "(abc)x", keys: "d%", want: "x"},
		{name: "review B13 repeat find operator", start: "a,b,c", keys: "f,d;", want: "ac", wantCol: 1},
		{name: "review B13 reverse find operator", start: "a,b,c", keys: "2f,d,", want: "a,c", wantCol: 1},
		{name: "review B13 search operator", start: "one two three", keys: "d/two<CR>", want: "two three"},
		{name: "review B14 single line open below no-op", opts: Options{SingleLine: true}, start: "abc", keys: "oX<Esc>", want: "abc"},
		{name: "review B14 single line open above no-op", opts: Options{SingleLine: true}, start: "abc", keys: "OX<Esc>", want: "abc"},
		{name: "review B14 single line line paste", opts: Options{SingleLine: true}, start: "abc", keys: "ddp", want: "abc", wantCol: 2},
		{name: "review B14 single line change line", opts: Options{SingleLine: true}, start: "abc", keys: "ccX<Esc>", want: "X"},
		{name: "review B14 single line yank put", opts: Options{SingleLine: true}, start: "abc", keys: "yyp", want: "aabcbc", wantCol: 3},

		// D1-D8: smaller Vim semantics.
		{name: "review D1 repeat till forward", start: "a,b,c,d", keys: "t,;", want: "a,b,c,d", wantCol: 2},
		{name: "review D1 repeat till backward", start: "a,b,c,d", col: 6, keys: "T,;", want: "a,b,c,d", wantCol: 4},
		{name: "review D2 join close paren", start: "call(\n)", keys: "J", want: "call()", wantCol: 5},
		{name: "review D2 join empty next", start: "a\n\nb", keys: "J", want: "a\nb"},
		{name: "review D2 join keeps first trailing", start: "a  \nb", keys: "J", want: "a  b", wantCol: 3},
		{name: "review D2 indent skips empty", start: "a\n\nb", keys: "3>>", want: "  a\n\n  b"},
		{name: "review D3 yiw cursor start", start: "abc", col: 2, keys: "yiw", want: "abc"},
		{name: "review D3 yi paren cursor start", start: "(abc)", col: 3, keys: "yi(", want: "(abc)", wantCol: 1},
		{name: "review D3 visual line yank cursor start", start: "a\nb\nc", keys: "Vjy", want: "a\nb\nc"},
		{name: "review D3 visual char yank cursor start", start: "abc", keys: "vey", want: "abc"},
		{name: "review D3 yb cursor start", start: "one two", col: 6, keys: "yb", want: "one two", wantCol: 4},
		{name: "review D4 visual zero motion", start: "abc", col: 2, keys: "v0", want: "abc", wantMode: Visual},
		{name: "review D4 vv exits", start: "abc", keys: "vv", want: "abc"},
		{name: "review D4 v switches visual line", start: "abc", keys: "Vv", want: "abc", wantMode: Visual},
		{name: "review D4 V switches visual char", start: "abc", keys: "vV", want: "abc", wantMode: VisualLine},
		{name: "review D5 visual change one undo", start: "abcd", keys: "vlcX<Esc>u", want: "abcd"},
		{name: "review D6 ctrl-w joins newline only", start: "one\ntwo", row: 1, keys: "i<C-w><Esc>", want: "onetwo", wantCol: 2},
		{name: "review D7 dd first nonblank", start: "a\n  b", keys: "dd", want: "  b"},
		{name: "review D7 G first nonblank", start: "a\n  b", keys: "G", want: "a\n  b", wantRow: 1},
		{name: "review D8 around quotes expectation", start: `"abc" x`, col: 2, keys: `da"`, want: "x"},

		// Differential-review blockers and nvim-default semantics.
		{name: "oracle dw only word", start: "foo", keys: "dw", want: ""},
		{name: "oracle dw final word", start: "foo bar", col: 4, keys: "dw", want: "foo ", wantCol: 3},
		{name: "oracle dW final WORD", start: "ab cd", col: 3, keys: "dW", want: "ab ", wantCol: 2},
		{name: "oracle d2w final words", start: "a bb cc", col: 2, keys: "d2w", want: "a ", wantCol: 1},
		{name: "oracle ywP final word", start: "foo bar", col: 4, keys: "ywP", want: "foo barbar", wantCol: 6},
		{name: "oracle cw final word", start: "foo bar", col: 4, keys: "cwX<Esc>", want: "foo X", wantCol: 4},
		{name: "oracle row one diw", start: "x\nfoo bar", row: 1, keys: "diw", want: "x\n bar", wantRow: 1},
		{name: "oracle row one diW", start: "x\nfoo bar", row: 1, keys: "diW", want: "x\n bar", wantRow: 1},
		{name: "oracle row one di paren", start: "x\n(a)", row: 1, col: 1, keys: "di(", want: "x\n()", wantRow: 1, wantCol: 1},
		{name: "oracle row one di bracket", start: "x\n[a]", row: 1, col: 1, keys: "di[", want: "x\n[]", wantRow: 1, wantCol: 1},
		{name: "oracle row one di quote", start: "x\n\"a\"", row: 1, col: 1, keys: "di\"", want: "x\n\"\"", wantRow: 1, wantCol: 1},
		{name: "oracle failed df", start: "abc", keys: "dfz", want: "abc"},
		{name: "oracle failed dt", start: "abc", keys: "dtz", want: "abc"},
		{name: "oracle failed dF", start: "abc", col: 2, keys: "dFz", want: "abc", wantCol: 2},
		{name: "oracle failed dT", start: "abc", col: 2, keys: "dTz", want: "abc", wantCol: 2},
		{name: "oracle closed nested bracket", start: "[[a],b]", col: 5, keys: "ci[Z<Esc>", want: "[Z]", wantCol: 1},
		{name: "oracle closed nested paren", start: "(a (b) c)", col: 7, keys: "ci(X<Esc>", want: "(X)", wantCol: 1},
		{name: "oracle dl last char", start: "abc", col: 2, keys: "dl", want: "ab", wantCol: 1},
		{name: "oracle yl last char", start: "abc", col: 2, keys: "yl", want: "abc", wantCol: 2},
		{name: "oracle d2l last char", start: "abc", col: 2, keys: "d2l", want: "ab", wantCol: 1},
		{name: "oracle blank inner block", start: "{\n\n}", row: 1, keys: "di{", want: "{\n}", wantRow: 1},
		{name: "oracle whitespace line skipped by w", start: "a\n   \nb", keys: "w", want: "a\n   \nb", wantRow: 2},
		{name: "oracle insert ctrl u stops at start", start: "ab", col: 1, keys: "iXY<C-u><Esc>", want: "ab"},
		{name: "oracle insert ctrl w stops at start", start: "one two", col: 4, keys: "iXY<C-w><Esc>", want: "one two", wantCol: 3},
		{name: "oracle replace enter splits", start: "abc", col: 1, keys: "r<CR>", want: "a\nc", wantRow: 1},
		{name: "oracle two linewise put", start: "a\nb", keys: "yy2p", want: "a\na\na\nb", wantRow: 1},
		{name: "oracle visual inner word", start: "one two", col: 5, keys: "viwd", want: "one ", wantCol: 3},
		{name: "oracle visual around word", start: "one two", col: 5, keys: "vawd", want: "one", wantCol: 2},
		{name: "oracle visual dollar delete joins", start: "abc\ndef", col: 1, keys: "v$d", want: "adef", wantCol: 1},
		{name: "oracle two D", start: "abc\ndef\nghi", col: 1, keys: "2D", want: "a\nghi"},
		{name: "oracle dot visual indent", start: "a\nb\nc", keys: "Vj>j.", want: "  a\n    b\n  c", wantRow: 1},
		{name: "oracle apostrophe quote pairing", start: "it's 'x'", col: 6, keys: "ci'Z<Esc>", want: "it's 'Z'", wantCol: 6},
		{name: "oracle star whole word", start: "foo foobar foo", keys: "*", want: "foo foobar foo", wantCol: 11},
		{name: "oracle star whitespace next word", start: "one two", col: 3, keys: "*", want: "one two", wantCol: 4},
		{name: "oracle empty search reuses pattern", start: "one two one", keys: "/two<CR>0/<CR>", want: "one two one", wantCol: 4},
		{name: "oracle insert arrow splits undo", start: "ab", keys: "iX<Right>Y<Esc>u", want: "Xab", wantCol: 2},
		{name: "oracle inner block yank linewise", start: "{\n  a\n}", row: 1, col: 1, keys: "yi{p", want: "{\n  a\n  a\n}", wantRow: 2, wantCol: 2},
		{name: "oracle counted next search", start: "a a a a", keys: "/a<CR>2n", want: "a a a a", wantCol: 6},
		{name: "oracle delete next search", start: "one two three two", keys: "/two<CR>0dn", want: "two three two"},
		{name: "oracle delete searched pattern", start: "one two three", keys: "d/two<CR>", want: "two three"},
		{name: "oracle open autoindent", start: "  a", keys: "oX<Esc>", want: "  a\n  X", wantRow: 1, wantCol: 2},
		{name: "oracle open above autoindent", start: "  a", keys: "OX<Esc>", want: "  X\n  a", wantCol: 2},
		{name: "oracle enter autoindent", start: "  ab", col: 2, keys: "i<CR>X<Esc>", want: "  \n  Xab", wantRow: 1, wantCol: 2},
		{name: "oracle change line autoindent", start: "  abc", col: 2, keys: "ccX<Esc>", want: "  X", wantCol: 2},
		{name: "oracle G keeps column", start: "abc\n  def", col: 1, keys: "G", want: "abc\n  def", wantRow: 1, wantCol: 1},
		{name: "oracle dd keeps column", start: "abc\n  def", col: 1, keys: "dd", want: "  def", wantCol: 1},
		{name: "oracle shift keeps column", start: "abc", col: 1, keys: ">>", want: "  abc", wantCol: 1},

		// Independent oracle-audit regressions.
		{name: "oracle dd last line two", start: "a\nb", row: 1, keys: "dd", want: "a"},
		{name: "oracle dd last line three", start: "a\nb\nc", row: 2, keys: "dd", want: "a\nb", wantRow: 1},
		{name: "oracle counted dd last line two", start: "a\nb\nc", row: 2, keys: "2dd", want: "a\nb\nc", wantRow: 2},
		{name: "oracle counted dd last line three", start: "a\nb\nc", row: 2, keys: "3dd", want: "a\nb\nc", wantRow: 2},
		{name: "oracle outer paren after closed inner", start: "f(a,\n (b),\n c)", row: 2, col: 2, keys: "ci(X<Esc>", want: "f(X)", wantCol: 2},
		{name: "oracle quote between pairs", start: `"a" "b"`, col: 3, keys: `ci"X<Esc>`, want: `"a"X"b"`, wantCol: 3},
		{name: "oracle brackets ignore quoted close", start: `x = ["a]", 1]`, col: 6, keys: "ci[Q<Esc>", want: "x = [Q]", wantCol: 5},
		{name: "oracle dot after insert arrow", start: "ab\ncd", keys: "ix<Down>y<Esc>j.", want: "xab\ncyyd", wantRow: 1, wantCol: 1},
		{name: "oracle undo insert cursor", start: "abc", keys: "A12<Esc>u", want: "abc", wantCol: 2},
		{name: "oracle undo redo delete cursor", start: "abc def", keys: "xwxuu<C-r>", want: "bc def"},
		{name: "oracle undo redo join cursor", start: "a\nb", keys: "Ju<C-r>", want: "a b"},
		{name: "oracle three put before cursor", start: "abcd", col: 1, keys: "x3P", want: "abbbcd", wantCol: 3},
		{name: "oracle multiline inner brace cursor", start: "{\n  a\n}", row: 1, col: 1, keys: "di{", want: "{\n}", wantRow: 1},
		{name: "oracle multiline inner bracket cursor", start: "[\n  a\n]", row: 1, col: 1, keys: "di[", want: "[\n]", wantRow: 1},
		{name: "oracle replace newline empty line", keys: "r<CR>", want: ""},
		{name: "oracle insert arrow at eol keeps repeat", start: "👩‍💻 dev\né界", row: 1, keys: "Ax<Right>y<Esc>.", want: "👩‍💻 dev\né界xyxy", wantRow: 1, wantCol: 5},
	}

	for n := 1; n <= 20; n++ {
		start := strings.Repeat("x", 25)
		cases = append(cases, corpusCase{
			name:  "review count x matrix " + fmt.Sprint(n),
			start: start, keys: fmt.Sprintf("%dx", n),
			want: start[n:],
		})
	}
	for n := 1; n <= 15; n++ {
		start := strings.Repeat("a", 20)
		cases = append(cases, corpusCase{
			name:  "review count replace matrix " + fmt.Sprint(n),
			start: start, keys: fmt.Sprintf("%drZ", n),
			want: strings.Repeat("Z", n) + start[n:], wantCol: n - 1,
		})
	}
	for n := 1; n <= 15; n++ {
		cases = append(cases, corpusCase{
			name:  "review count insert matrix " + fmt.Sprint(n),
			start: "z", keys: fmt.Sprintf("%diX<Esc>", n),
			want: strings.Repeat("X", n) + "z", wantCol: n - 1,
		})
	}
	for n := 1; n <= 15; n++ {
		cases = append(cases, corpusCase{
			name:  "review count append matrix " + fmt.Sprint(n),
			start: "z", keys: fmt.Sprintf("%daX<Esc>", n),
			want: "z" + strings.Repeat("X", n), wantCol: n,
		})
	}
	for n := 1; n <= 10; n++ {
		start := strings.Repeat("a", 15)
		cases = append(cases, corpusCase{
			name:  "review count substitute matrix " + fmt.Sprint(n),
			start: start, keys: fmt.Sprintf("%dsX<Esc>", n),
			want: "X" + start[n:],
		})
	}
	return cases
}
