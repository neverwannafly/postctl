# UX-V1 — `vimtext`: the modal vim text editor widget

Owner model: GPT-5.6 Sol (medium) · Branch: `ws/ux-v1-vimtext` · Worktree: `~/work/postctl-wt/ux-v1-vimtext`

## Goal

One reusable Bubble Tea v2 component, `internal/tui/vimtext`, that every text field in postctl will use: URL bar, key-value cells, auth and settings forms, body, scripts, WebSocket composer, env editor, prompts and filters, and (read-only) the response body. This workstream builds and tests the widget **only**; adopting it in panes is phase 2 (V2a, V2b, V3). Correctness matters more than breadth: every supported key must behave like Vim.

## Read first

`AGENTS.md`, `internal/tui/AGENTS.md`, `docs/briefs/UX0-overview.md`, `docs/plan.md` §0 U1 and U6, the **Key map** section of `docs/design-language.md` (the "Text fields" part is your spec), `docs/tui-stack.md`. Current widgets for reference: `internal/tui/textin/textin.go` (single-line, custom) and `internal/tui/request/texted.go` (wraps Bubbles `textarea`; note how line numbers, resize and the purity test `TestTextareaViewIsPure` work). Theme tokens: `internal/ui`.

## Scope (files you own)

`internal/tui/vimtext/` (new package) and one section in `docs/tui-stack.md`. Touch nothing else. No new module dependency unless it is pure Go and you justify it in `docs/tui-stack.md`; chroma v2, lipgloss v2, `github.com/charmbracelet/x/ansi` and `github.com/rivo/uniseg` are already available. The package must not import `internal/tui` (root), `internal/engine` or any pane; it may import `internal/ui`.

## API (implement exactly; additive extras are fine)

```go
package vimtext

type Mode int // Normal, Insert, Visual, VisualLine; String() gives "NORMAL", "INSERT", "VISUAL", "VISUAL LINE"

type Options struct {
    SingleLine  bool   // URL, cells, prompts: no newlines; Enter emits Submit; j/k no-ops; dd/cc/yy act on the whole value
    ReadOnly    bool   // response body: motions, search, visual, yank; no edits, no insert mode
    StartInsert bool   // overlay inputs open in insert
    LineNumbers bool
    TabText     string // inserted by Tab in insert (multi-line); default two spaces
    Placeholder string
    Highlighter Highlighter              // nil = plain
    Completer   Completer                // nil = no completion
    Clipboard   func(text string) tea.Cmd // default tea.SetClipboard (OSC 52)
    Redact      func(text string) string  // applied to text before it reaches Clipboard; nil = identity
}

type EventKind int // None, Changed, Submit, Leave, Unhandled, Seq

type Event struct {
    Kind EventKind
    Key  tea.KeyPressMsg // for Unhandled: the key the caller should route upward
    Seq  string          // for Seq: a two-key sequence the widget does not own, e.g. "gu"
}

func New(th ui.Theme, opts Options) *Model
func (m *Model) Update(msg tea.Msg) (tea.Cmd, Event)
func (m *Model) View() string // pure: no mutation of any kind
func (m *Model) SetSize(w, h int)
func (m *Model) SetValue(s string) // resets undo history and cursor to start
func (m *Model) Value() string
func (m *Model) Focus() tea.Cmd
func (m *Model) Blur()
func (m *Model) Focused() bool
func (m *Model) Mode() Mode
func (m *Model) Pending() string // pending count/operator/register for the footer, e.g. "2d"; "" when idle
func (m *Model) Cursor() (row, col int)
func (m *Model) SetCursor(row, col int) // clamps; used by mouse in phase 3
func (m *Model) Select(r0, c0, r1, c1 int) // enter Visual with that range; used by mouse drag in phase 3

type Span struct{ Start, End int; Style lipgloss.Style } // byte offsets in one line
type Highlighter interface{ Highlight(lines []string) [][]Span }
func ChromaHighlighter(th ui.Theme, lexer string) Highlighter // "javascript", "json"; maps chroma token types to ui semantic tokens; NO_COLOR returns no spans

type Item struct{ Label, Detail, Insert string }
type Completer interface {
    // Complete is called in insert mode after each edit. It returns the items to
    // show for the text before the cursor on the current line, and how many bytes
    // before the cursor the accepted Insert replaces. No items hides the popup.
    Complete(lineBeforeCursor string) (items []Item, replace int)
}
```

## Behavior

**Normal:** `h j k l` and arrows, `w b e W B E ge`, `0 ^ $`, `gg G` (with count: go to line), `f t F T ; ,`, `%`, counts on motions and operators (`3w`, `2dd`, `d3w`); operators `d c y` with any motion and text objects `iw aw iW aW i" a" i' a' i` + "`" + ` i( a( ib i{ a{ iB i[ a[`; `dd cc yy`, `D C Y`, `x X`, `s S`, `r<char>`, `p P` (charwise and linewise), `J`, `~`, `>> <<`; `u` undo, `Ctrl-r` redo; `.` repeats the last change including its insert text; `/ ? n N *` search (search prompt is drawn inside the widget on its last line; `Esc` cancels, `Enter` jumps; wrap-around); `i a I A o O` enter insert; `v V` visual.

**Insert:** text and paste (`tea.PasteMsg`, multi-line paste collapses newlines to spaces in SingleLine), `Backspace`, `Delete`, arrows, `Ctrl-w`, `Ctrl-u`, `Tab` (TabText; in SingleLine: Unhandled), `Enter` (newline; SingleLine: Submit). The insert session is one undo step. With a Completer: popup under the cursor line, `Ctrl-n`/`Ctrl-p` or `Down`/`Up` move, `Enter`/`Tab` accept, `Esc` closes the popup first (a second `Esc` leaves insert).

**Visual / Visual line:** motions extend, `o` swaps anchor, `d x c y > < ~ J` act, `Esc` back to normal.

**Registers:** one unnamed register; every yank and delete also calls `Options.Clipboard(Redact(text))`. No named registers or macros.

**Events and routing (critical; the shell relies on it):**
- `Changed` after any edit. `Submit` on Enter in SingleLine (insert and normal). `Leave` on `Esc` in normal mode with nothing pending.
- `Unhandled` (the caller routes `Event.Key` upward) for: `Ctrl-S`, `Ctrl-Enter`, `Ctrl-P`, `Ctrl-C` in normal mode, any `Alt-` key, `Ctrl-h/j/k/l` in normal and visual mode, `Ctrl-o`, `Ctrl-i`/`Tab` in normal mode, `Space` and `:` in normal mode, `q` in normal mode, `Tab`/`Shift-Tab` in normal mode, and any key the widget does not support. Count/operator state is cleared when a key is reported Unhandled.
- `Ctrl-C` in insert or visual acts as `Esc` (handled).
- `Seq` for a `g` followed by a key the widget does not own (it owns `gg`, `ge`, `gE`): report `Seq: "g"+key`, e.g. `gu`, `gb`, `gH`.
- ReadOnly: insert-entering keys and edit operators are no-ops (not Unhandled); `y` and visual yank work; `Changed` is never emitted.

**Rendering:** soft-wrap off; horizontal scroll keeps the cursor visible; vertical scroll keeps `scrolloff` 2; offsets are adjusted in `Update`/`SetSize`, never in `View`. Cursor: block in normal/visual, bar in insert (use the reverse-video cell as the cursor so it works with NO_COLOR and ASCII). Selection rendered with a `ui` selection token plus reverse video. Line numbers right-aligned in a muted token. Wide characters and grapheme clusters move as one cell (uniseg); tabs render as spaces. Highlight spans are recomputed in `Update` after a change and cached (not in `View`). An empty unfocused field shows the placeholder in a muted token.

## Tests (table-driven, synthetic text only)

- A keystroke corpus: `{name, opts, start text, start cursor, keys "dwi foo<Esc>", want text, want cursor, want mode, want events}` with a tiny key-notation parser (`<Esc>`, `<C-r>`, `<CR>`, `<Tab>`, `<BS>`, `<C-w>`). At least 150 cases covering every key listed above, counts, `.` repeat after `ciw`/`dd`/`A`, undo/redo across insert sessions, text objects at line edges and nested pairs, `%` on unmatched brackets, search wrap, SingleLine and ReadOnly restrictions, multi-byte text (`é`, CJK, emoji with ZWJ).
- Routing: every key listed under Unhandled and Seq produces exactly that event and leaves state clean.
- Clipboard: yank and delete call `Clipboard` with `Redact` applied (use a fake redactor that replaces a sentinel; assert the sentinel never reaches the fake clipboard in ReadOnly mode).
- Completion: fake completer; popup navigation, accept replaces `replace` bytes, Esc ladder.
- `TestViewIsPure` (View twice, deep-compare the model) in each mode; golden renders at 40x5 and 80x12 for normal, insert, visual, line numbers, highlight, placeholder, completion popup, in dark, `NO_COLOR` and ASCII looks (follow the golden helper style of `internal/tui/request`; add an `-update` flag in this package).
- A benchmark: 5,000-line buffer, `G`, `gg`, typing 100 characters, highlight recompute; Update must stay under 2 ms per key on this machine.

## Acceptance

`task agents:check` green; goldens reviewed; `docs/tui-stack.md` section "vimtext" (what it is, why custom instead of `textarea`, supported keys pointer to the design-language map). Ledger STARTED and DONE entries plus a status row. Commit on your branch, do not push. Final message: API deviations (should be none), supported-key list, test counts, benchmark numbers, commit hash.
