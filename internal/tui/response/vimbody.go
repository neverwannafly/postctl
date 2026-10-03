package response

import (
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// The Body tab shows text bodies in a read-only vim buffer (vimtext with line
// numbers and the Chroma highlighter): motions, / n N, v and V, y and yy. The
// buffer holds what the server sent, as the Body tab always has (INT4); what
// leaves it, through a yank, is redacted first unless the pane is revealed.
// Editing keys do nothing. Keys the buffer does not own go to the shell with
// tui.ForwardKeyMsg and tui.ForwardSeqMsg. Bodies of 5 MiB or more, binary
// bodies and the jq view keep the virtual viewer.

// vimInlineMax is the largest body built on the Update goroutine. Bigger
// bodies (the highlighter takes about half a second per MiB) are built in a
// command, and the virtual viewer shows the body until the editor is ready.
const vimInlineMax = 64 << 10

// largeBodyNote is the footer note when the virtual viewer shows a text body.
const largeBodyNote = "Large body: vim keys limited"

// vimKey identifies what a built buffer shows; a different key means the
// buffer is stale and gets rebuilt.
type vimKey struct {
	st    *storedResult
	mode  bodyMode
	path  string
	theme int
}

// span is a byte range of the buffer text that holds secret material.
type span struct{ start, end int }

// bodyVim is the read-only buffer for one response body.
type bodyVim struct {
	key   vimKey
	f     *vimfield.Field
	doc   string // buffer text, as the editor holds it
	ct    string // content type whose redaction rules apply
	rd    *history.Redactor
	spans []span // credential values and known secrets in doc
	// wholeOnly is set when a secret is only found across lines. Any yank is
	// then fully masked, since it cannot be told which part holds it.
	wholeOnly bool
	w, h      int
}

// vimBuiltMsg delivers a buffer built off the Update goroutine.
type vimBuiltMsg struct {
	gen int
	key vimKey
	v   *bodyVim
}

// vimEligible reports whether the response in st can use the vim buffer: a
// text body that is not a hex dump and is below the large-body threshold.
func (p *Pane) vimEligible(st *storedResult) bool {
	if st == nil || st.result.Response == nil || st.result.Skipped {
		return false
	}
	body := st.result.Response.Body
	if len(body) == 0 || len(body) >= largeBodyThreshold || p.bodyMode == modeHex {
		return false
	}
	return DetectBodyKind(history.ContentType(st.result.Response.Headers), body) != BodyBinary
}

// vimOnScreen reports whether the Body tab shows the response body itself
// (not a send in progress, the diff, or a jq result).
func (p *Pane) vimOnScreen() bool {
	return p.tab == tabBody && !p.diff.open && !p.jqBarVisible() && p.ref.Request != nil &&
		p.sendingID != p.ref.Request.ID
}

func (p *Pane) wantVimKey() (vimKey, bool) {
	st := p.current()
	if !p.vimEligible(st) {
		return vimKey{}, false
	}
	return vimKey{st: st, mode: p.bodyMode, path: p.pathFilter, theme: p.themeGen}, true
}

// readyVim returns the built buffer when it matches what the pane wants.
func (p *Pane) readyVim() *bodyVim {
	if p.vim == nil {
		return nil
	}
	if want, ok := p.wantVimKey(); !ok || want != p.vim.key {
		return nil
	}
	return p.vim
}

// vimLive reports whether the Body tab is a live vim buffer: it takes the
// keys when the pane has the keyboard.
func (p *Pane) vimLive() bool { return p.vimOnScreen() && p.readyVim() != nil }

// largeBody reports whether the Body tab shows a text body in the virtual
// viewer because it is too large for the editor.
func (p *Pane) largeBody() bool {
	st := p.current()
	if st == nil || st.result.Response == nil || p.tab != tabBody || !p.vimOnScreen() {
		return false
	}
	body := st.result.Response.Body
	return len(body) >= largeBodyThreshold && p.bodyMode != modeHex &&
		DetectBodyKind(history.ContentType(st.result.Response.Headers), body) != BodyBinary
}

// ensureVim builds or drops the buffer so it matches the current response, and
// returns the command that builds a large one. Call it after every state
// change, from Update (never from View).
func (p *Pane) ensureVim() tea.Cmd { return p.syncVim(true) }

// ensureVimInline is ensureVim for callers that cannot run a command: it builds
// only a small buffer.
func (p *Pane) ensureVimInline() { _ = p.syncVim(false) }

func (p *Pane) syncVim(async bool) tea.Cmd {
	want, ok := p.wantVimKey()
	if !ok {
		p.vim, p.vimWant = nil, vimKey{}
		return nil
	}
	if p.vim != nil && p.vim.key == want {
		p.fitVim()
		return nil
	}
	if p.vimWant == want || p.tab != tabBody {
		return nil
	}
	if len(want.st.result.Response.Body) <= vimInlineMax {
		p.vimWant = vimKey{}
		p.setVim(p.buildVim(p.th, want))
		return nil
	}
	if !async {
		return nil
	}
	p.vimGen++
	p.vimWant = want
	gen, th := p.vimGen, p.th
	return func() tea.Msg { return vimBuiltMsg{gen: gen, key: want, v: p.buildVim(th, want)} }
}

func (p *Pane) onVimBuilt(msg vimBuiltMsg) {
	if msg.gen != p.vimGen || msg.key != p.vimWant {
		return
	}
	p.vimWant = vimKey{}
	if want, ok := p.wantVimKey(); ok && want == msg.key {
		p.setVim(msg.v)
	}
}

func (p *Pane) setVim(v *bodyVim) {
	p.vim = v
	p.syncVimFocus()
	p.fitVim()
}

// syncVimFocus gives the buffer the keyboard exactly while the pane has focus
// and the Body tab is the buffer; otherwise it blurs, which also drops a
// pending command or search.
func (p *Pane) syncVimFocus() {
	v := p.readyVim()
	switch {
	case v == nil:
		if p.vim != nil {
			p.vim.f.Blur()
		}
	case p.focused && p.vimOnScreen():
		if !v.f.Focused() {
			v.f.Focus()
		}
	case v.f.Focused():
		v.f.Blur()
	}
}

// fitVim sizes the buffer to the rows the Body tab has for it.
func (p *Pane) fitVim() {
	v := p.vim
	st := p.current()
	if v == nil || st == nil {
		return
	}
	w, h := max(10, p.w), p.vimRows(st.result)
	if w == v.w && h == v.h {
		return
	}
	v.w, v.h = w, h
	v.f.Fit(w, h)
}

// vimRows is the number of rows left for the buffer under the warnings and the
// truncation note.
func (p *Pane) vimRows(r tui.SendResult) int {
	h := p.contentHeight() - len(r.Warnings)
	if r.Response != nil && r.Response.BodyTruncated {
		h--
	}
	return max(1, h)
}

// buildVim builds the buffer for key. It reads only the stored result, the
// settings in key and th, so it can run in a command.
func (p *Pane) buildVim(th ui.Theme, key vimKey) *bodyVim {
	r := key.st.result
	body := r.Response.Body
	ct := history.ContentType(r.Response.Headers)
	kind := DetectBodyKind(ct, body)
	text := string(body)
	if key.mode == modePretty && kind == BodyJSON {
		if doc, err := buildJSONDoc(body, key.path); err == nil {
			text = strings.Join(doc.lines, "\n")
		}
	}
	if kind == BodyJSON {
		ct = "application/json" // the redaction rules for JSON apply to JSON-looking bodies
	}
	v := &bodyVim{key: key, doc: strings.ReplaceAll(text, "\r\n", "\n"), ct: ct, rd: resultRedactor(r), w: -1, h: -1}
	v.spans, v.wholeOnly = secretSpans(v.rd, ct, v.doc)

	opts := vimtext.Options{ReadOnly: true, LineNumbers: true, Redact: func(s string) string { return p.redactYank(v, s) }}
	if lang := highlightLang(kind); lang != "" {
		opts.Highlighter = vimtext.ChromaHighlighter(th, lang)
	}
	write := p.deps.Clipboard
	if write == nil {
		write = tea.SetClipboard // OSC 52, as vimtext does by default
	}
	opts.Clipboard = func(text string) tea.Cmd {
		p.yanked = true
		return write(text)
	}
	v.f = vimfield.New(th, opts)
	v.f.Set(v.doc)
	return v
}

// secretSpans finds the byte ranges of text that redaction changes, line by
// line. A range runs from the first changed byte of a line to the last, so it
// may cover a little more than the secret, never less. wholeOnly reports a
// secret that only shows when lines are redacted together.
func secretSpans(rd *history.Redactor, ct, text string) (spans []span, wholeOnly bool) {
	lines := strings.Split(text, "\n")
	masked := make([]string, len(lines))
	off := 0
	for i, line := range lines {
		masked[i] = rd.Body(ct, line)
		if masked[i] != line {
			pre := commonPrefix(line, masked[i])
			suf := commonSuffix(line[pre:], masked[i][pre:])
			spans = append(spans, span{start: off + pre, end: off + len(line) - suf})
		}
		off += len(line) + 1
	}
	return spans, rd.Body(ct, text) != strings.Join(masked, "\n")
}

// commonPrefix is the length of the common prefix of a and b, on a rune
// boundary of a.
func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	for n > 0 && n < len(a) && !utf8.RuneStart(a[n]) {
		n--
	}
	return n
}

// commonSuffix is the length of the common suffix of a and b, on a rune
// boundary of a.
func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	for n > 0 && n < len(a) && !utf8.RuneStart(a[len(a)-n]) {
		n--
	}
	return n
}

// redactYank is the vim buffer's Redact hook: it masks what a yank copies
// unless the pane is revealed. It finds every place the yanked text occurs in
// the buffer and masks whatever overlaps a secret range there, so a character
// selection inside a credential value is masked too. Text that cannot be found
// in the buffer is masked as a whole when the buffer holds any secret.
func (p *Pane) redactYank(v *bodyVim, frag string) string {
	p.lastYankMasked = false
	if p.revealSecrets || frag == "" {
		return frag
	}
	out := v.rd.Body(v.ct, frag)
	switch {
	case v.wholeOnly:
		p.lastYankMasked = true
		return history.Mask
	case len(v.spans) > 0:
		out = maskOccurrences(v, frag, out)
	}
	p.lastYankMasked = out != frag
	return out
}

// maskOccurrences replaces the parts of frag that overlap secret ranges at any
// of its occurrences in the buffer.
func maskOccurrences(v *bodyVim, frag, redacted string) string {
	probe := frag
	hit := make([]bool, len(frag))
	found := false
	for pass := 0; pass < 2 && !found; pass++ {
		if pass == 1 {
			probe = strings.TrimSuffix(frag, "\n") // the last line has no newline in the buffer
			if probe == frag || probe == "" {
				break
			}
		}
		for from := 0; from <= len(v.doc)-len(probe); {
			i := strings.Index(v.doc[from:], probe)
			if i < 0 {
				break
			}
			found = true
			at := from + i
			j := sort.Search(len(v.spans), func(k int) bool { return v.spans[k].end > at })
			for ; j < len(v.spans) && v.spans[j].start < at+len(probe); j++ {
				for b := max(v.spans[j].start, at); b < min(v.spans[j].end, at+len(probe)); b++ {
					hit[b-at] = true
				}
			}
			from = at + 1
		}
	}
	if !found {
		return history.Mask // the text is not in the buffer: assume the worst
	}
	var b strings.Builder
	for i := 0; i < len(frag); {
		if !hit[i] {
			j := i
			for j < len(frag) && !hit[j] {
				j++
			}
			b.WriteString(frag[i:j])
			i = j
			continue
		}
		for i < len(frag) && hit[i] {
			i++
		}
		b.WriteString(history.Mask)
	}
	// Known secrets that a character range cut in half are caught by the
	// redactor's own pass on what is left.
	return v.rd.Body(v.ct, b.String())
}

// vimKeyPress handles a key while the Body tab is a live vim buffer.
func (p *Pane) vimKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	v := p.vim
	f := v.f
	idle := f.Mode() == vimtext.Normal && f.Pending() == ""
	if idle {
		switch msg.String() {
		case "r":
			p.togglePrettyRaw()
			return p.ensureVim()
		case "u":
			p.revealSecrets = true
			return toast(ui.ToastInfo, "Copies are unmasked until you leave this tab")
		case ".":
			if p.bodyKind == BodyJSON && p.bodyMode == modePretty {
				p.beginPathFilter()
				return nil
			}
			return nil
		}
	}
	p.yanked = false
	res := f.Update(msg)
	cmds := []tea.Cmd{res.Cmd}
	if p.yanked {
		p.yanked = false
		label := "Copied"
		if p.lastYankMasked {
			label = "Copied, secrets masked (u unmasks)"
		}
		cmds = append(cmds, toast(ui.ToastSuccess, label))
	}
	switch res.Event.Kind {
	case vimtext.Leave:
		cmds = append(cmds, func() tea.Msg { return tui.FocusPaneMsg{Slot: tui.SlotRequest} })
	case vimtext.Unhandled:
		if f.Mode() == vimtext.Normal {
			key := res.Event.Key
			cmds = append(cmds, func() tea.Msg { return tui.ForwardKeyMsg{Key: key} })
		}
	case vimtext.Seq:
		if f.Mode() == vimtext.Normal {
			seq := res.Event.Seq
			cmds = append(cmds, func() tea.Msg { return tui.ForwardSeqMsg{Seq: seq} })
		}
	}
	return tea.Batch(cmds...)
}

// vimHints are the footer hints while the Body tab is a live vim buffer.
func (p *Pane) vimHints() []ui.Hint {
	f := p.vim.f
	switch {
	case strings.HasPrefix(f.Pending(), "/") || strings.HasPrefix(f.Pending(), "?"):
		return []ui.Hint{{Key: "Enter", Label: "Search", Priority: 0}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	case f.Mode() != vimtext.Normal:
		return []ui.Hint{{Key: "y", Label: "Copy selection", Priority: 0}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	}
	h := []ui.Hint{
		{Key: "/", Label: "Search", Priority: 2},
		{Key: "v", Label: "Select", Priority: 3},
		{Key: "y", Label: "Copy", Priority: 1},
		{Key: "[ ]", Label: "Tab", Priority: 4},
		{Key: "r", Label: "Raw/pretty", Priority: 5},
		{Key: "Esc", Label: "Request", Priority: 6},
	}
	if p.revealSecrets {
		h = append(h, ui.Hint{Key: "Copies unmasked", Priority: 0})
	} else {
		h = append(h, ui.Hint{Key: "u", Label: "Unmask copies", Priority: 7})
	}
	return h
}

// vimWheel scrolls the buffer by n lines for the mouse wheel.
func (p *Pane) vimWheel(n int) {
	if v := p.readyVim(); v != nil {
		v.f.ScrollLines(n)
	}
}
