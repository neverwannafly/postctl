package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shubham/postctl/internal/ui"
)

// overlay is a screen that takes over the body while it is open: help, the
// palette, the environment switcher and dialogs. Only one is open at a time.
type overlay interface {
	// update handles a key press or paste. Returning a nil overlay closes it.
	update(a *app, msg tea.Msg) (overlay, tea.Cmd)
	// view renders into w x h cells.
	view(a *app, w, h int) string
	// hints are the footer hints while the overlay is open.
	hints(a *app) []ui.Hint
}

func hint(key, label string, priority int) ui.Hint {
	return ui.Hint{Key: key, Label: label, Priority: priority}
}

// ---- confirm ---------------------------------------------------------------

// confirmOverlay is a confirmation dialog. Danger dialogs start on Cancel and
// only act once the user has moved to the action (or pressed y) and pressed
// Enter.
type confirmOverlay struct {
	c     ui.Confirm
	onYes func(a *app) tea.Cmd
}

func (o *confirmOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return o, nil
	}
	switch k.String() {
	case "esc", "n":
		return nil, nil
	case "left", "shift+tab":
		o.c.Move(-1)
	case "right", "tab":
		o.c.Move(1)
	case "y":
		o.c.Focus = len(o.c.Choices) - 1
	case "enter":
		if o.c.Accepted() {
			return nil, o.onYes(a)
		}
		return nil, nil
	}
	return o, nil
}

func (o *confirmOverlay) view(a *app, w, h int) string {
	return a.theme.ConfirmView(o.c, min(w-2, 62))
}

func (o *confirmOverlay) hints(a *app) []ui.Hint {
	arrows := "←→"
	if a.ascii {
		arrows = "Left/Right"
	}
	return []ui.Hint{hint("Enter", "Confirm choice", 0), hint(arrows, "Choose", 1), hint("y", "Select "+strings.ToLower(o.c.Choices[len(o.c.Choices)-1]), 2), hint("Esc", "Cancel", 0)}
}

// ---- prompt ----------------------------------------------------------------

// promptOverlay asks for a name, optionally with a type switch (Tab).
type promptOverlay struct {
	title  string
	note   string
	input  *promptInput
	kinds  []string
	kind   int
	err    string
	submit func(a *app, value string, kind int) (errText string, cmd tea.Cmd)
}

type promptLayout struct {
	width, inner, inputLine, height int
}

func (o *promptOverlay) layout(w int) promptLayout {
	line := 2
	if o.note != "" {
		line++
	}
	if len(o.kinds) > 1 {
		line += 2
	}
	height := line + 3 // input line plus the modal's top and bottom edges
	if o.err != "" {
		height++
	}
	width := min(w-2, 64)
	return promptLayout{width: width, inner: width - 4, inputLine: line, height: height}
}

func newPrompt(a *app, title, note, initial string, kinds []string, submit func(a *app, value string, kind int) (string, tea.Cmd)) (*promptOverlay, tea.Cmd) {
	in, cmd := newPromptInput(a.theme, initial, "Name")
	return &promptOverlay{title: title, note: note, input: in, kinds: kinds, submit: submit}, cmd
}

func (o *promptOverlay) overlayVimMode() string { return o.input.vimMode() }

func (o *promptOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		g := a.geo()
		lay := o.layout(g.width)
		originX := g.inset + max(0, (g.width-lay.width)/2)
		originY := headerRows + max(0, (g.bodyH-lay.height)/3)
		o.input.mouseUpdate(mouse, mouse.Mouse().X-originX-2, mouse.Mouse().Y-originY-lay.inputLine-1, a.now())
		return o, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "tab":
			if len(o.kinds) > 1 {
				o.kind = (o.kind + 1) % len(o.kinds)
				return o, nil
			}
		case "shift+tab":
			if len(o.kinds) > 1 {
				o.kind = (o.kind + len(o.kinds) - 1) % len(o.kinds)
				return o, nil
			}
		}
	}
	cmd, nav := o.input.update(msg)
	switch nav {
	case pickNavClose:
		return nil, nil
	case pickNavSubmit:
		v := strings.TrimSpace(o.input.ed.Value())
		if v == "" {
			o.err = "Enter a name."
			return o, nil
		}
		msg, c := o.submit(a, v, o.kind)
		if msg != "" {
			o.err = msg
			return o, cmd
		}
		return nil, c
	case pickNavChanged:
		o.err = ""
	}
	return o, cmd
}

func (o *promptOverlay) view(a *app, w, h int) string {
	t := a.theme
	lay := o.layout(w)
	o.input.setWidth(max(8, lay.inner-1))
	lines := []string{t.Bold().Render(o.title)}
	if o.note != "" {
		lines = append(lines, t.Render(ui.TextSecondary, t.Truncate(o.note, lay.inner)))
	}
	if len(o.kinds) > 1 {
		var parts []string
		for i, k := range o.kinds {
			on, off := "(•)", "( )"
			if a.ascii {
				on = "(*)"
			}
			if i == o.kind {
				parts = append(parts, t.Render(ui.AccentPrimary, on+" "+k))
			} else {
				parts = append(parts, t.Render(ui.TextSecondary, off+" "+k))
			}
		}
		lines = append(lines, "", strings.Join(parts, "  "))
	}
	lines = append(lines, "", o.input.view(t))
	if o.err != "" {
		lines = append(lines, t.Render(ui.StatusDanger, t.Glyphs.Fail+" "+o.err))
	}
	return t.Modal(strings.Join(lines, "\n"), lay.width, false)
}

func (o *promptOverlay) hints(a *app) []ui.Hint {
	hs := []ui.Hint{hint("Enter", "Save", 0), hint("Esc", "Cancel", 0)}
	if len(o.kinds) > 1 {
		hs = append(hs, hint("Tab", "Change type", 1))
	}
	return hs
}

// ---- picker ----------------------------------------------------------------

// pickItem is one candidate in a picker.
type pickItem struct {
	label   string // searched and shown
	detail  string // quiet text on the right, for example the kind
	method  string // when set, a method badge precedes the label
	payload any
}

// maxPickResults caps the rendered result list.
const maxPickResults = 60

// pickerOverlay is a fuzzy list: the command palette and the move-to picker.
type pickerOverlay struct {
	title    string
	input    *pickInput
	all      []pickItem
	res      []ranked
	cursor   int
	top      int
	empty    string
	onPick   func(a *app, it pickItem) tea.Cmd
	lastPage int

	// exCommand, when set, is offered the typed text on Enter before the
	// highlighted item is chosen (the palette's q, w and wq).
	exCommand func(a *app, input string) (tea.Cmd, bool)
}

func newPicker(a *app, title, placeholder, empty string, items []pickItem, onPick func(a *app, it pickItem) tea.Cmd) (*pickerOverlay, tea.Cmd) {
	in, cmd := newPickInput(a.theme, a.ascii, "", placeholder)
	p := &pickerOverlay{title: title, input: in, all: items, empty: empty, onPick: onPick}
	p.refilter()
	g := a.geo()
	p.setSize(g.width, g.bodyH)
	return p, cmd
}

func (p *pickerOverlay) overlayVimMode() string { return p.input.vimMode() }

func (p *pickerOverlay) refilter() {
	p.res = rankBy(p.input.value(), len(p.all), func(i int) string { return p.all[i].label }, maxPickResults)
	p.cursor, p.top = 0, 0
}

func (p *pickerOverlay) pick(a *app) (overlay, tea.Cmd) {
	if p.exCommand != nil {
		if cmd, ok := p.exCommand(a, p.input.rawValue()); ok {
			return nil, cmd
		}
	}
	if p.cursor < len(p.res) {
		it := p.all[p.res[p.cursor].index]
		return nil, p.onPick(a, it)
	}
	return p, nil
}

func (p *pickerOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		g := a.geo()
		if p.input.mouseUpdate(mouse, mouse.Mouse().X-g.inset, mouse.Mouse().Y-headerRows-1, a.now()) {
			return p, nil
		}
		return p, nil
	}
	page := max(1, p.lastPage-1)
	cmd, nav := p.input.update(msg, page)
	switch nav {
	case pickNavClose:
		return nil, nil
	case pickNavSubmit:
		return p.pick(a)
	case pickNavUp:
		p.cursor = max(0, p.cursor-1)
		p.scroll()
	case pickNavDown:
		if len(p.res) > 0 {
			p.cursor = min(len(p.res)-1, p.cursor+1)
		}
		p.scroll()
	case pickNavPageUp:
		p.cursor = max(0, p.cursor-page)
		p.scroll()
	case pickNavPageDown:
		if len(p.res) > 0 {
			p.cursor = min(len(p.res)-1, p.cursor+page)
		}
		p.scroll()
	case pickNavChanged:
		p.refilter()
	}
	return p, cmd
}

func (p *pickerOverlay) scroll() {
	n := max(1, p.lastPage)
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+n {
		p.top = p.cursor - n + 1
	}
}

func (p *pickerOverlay) setSize(w, h int) {
	p.input.setWidth(max(8, w-4))
	p.lastPage = max(1, h-4)
	p.scroll()
}

func (p *pickerOverlay) view(a *app, w, h int) string {
	t := a.theme
	head := t.Bold().Render(p.title)
	esc := t.Render(ui.TextSecondary, "Esc Close")
	gap := max(1, w-ui.Width(head)-ui.Width(esc))
	lines := []string{head + strings.Repeat(" ", gap) + esc, p.input.view(t), t.Rule(w)}
	if len(p.res) == 0 {
		msg := p.empty
		if p.input.value() != "" {
			msg = "No matches for \"" + p.input.value() + "\"."
		}
		lines = append(lines, t.Render(ui.TextSecondary, msg))
		return strings.Join(lines, "\n")
	}
	end := min(len(p.res), p.top+p.lastPage)
	for i := p.top; i < end; i++ {
		r := p.res[i]
		lines = append(lines, p.row(t, a.ascii, p.all[r.index], r.pos, i == p.cursor, w))
	}
	return strings.Join(lines, "\n")
}

func (p *pickerOverlay) row(t ui.Theme, ascii bool, it pickItem, pos []int, sel bool, w int) string {
	on := func(tok ui.Token) lipgloss.Style { return t.StyleOn(tok, sel) }
	var s seg
	if sel {
		s.add(on(ui.AccentPrimary), t.Glyphs.Select+" ")
	} else {
		s.add(on(ui.TextPrimary), "  ")
	}
	if it.method != "" {
		s.add(on(ui.MethodToken(it.method)), t.Fit(ui.MethodLabel(it.method, methodWidth), methodWidth))
		s.add(on(ui.TextPrimary), " ")
	}
	detail := it.detail
	room := w - s.w
	if detail != "" && room > ui.Width(detail)+12 {
		room -= ui.Width(detail) + 2
	} else {
		detail = ""
	}
	label := []rune(it.label)
	if len(label) > room {
		label = append([]rune(t.Glyphs.Ellipsis), label[len(label)-(room-1):]...)
		shift := len([]rune(it.label)) - (room - 1)
		shifted := make([]int, 0, len(pos))
		for _, q := range pos {
			if q >= shift {
				shifted = append(shifted, q-shift+1)
			}
		}
		pos = shifted
	}
	m := map[int]bool{}
	for _, q := range pos {
		m[q] = true
	}
	start := 0
	for i := 1; i <= len(label); i++ {
		if i == len(label) || m[i] != m[start] {
			tok := ui.TextPrimary
			st := on(tok)
			if m[start] {
				st = on(ui.AccentPrimary).Bold(true)
			}
			s.add(st, string(label[start:i]))
			start = i
		}
	}
	if detail != "" {
		s.add(on(ui.TextQuiet), strings.Repeat(" ", max(2, w-s.w-ui.Width(detail)))+detail)
	}
	if pad := w - s.w; pad > 0 {
		s.add(on(ui.TextPrimary), strings.Repeat(" ", pad))
	}
	return s.sb.String()
}

func (p *pickerOverlay) hints(a *app) []ui.Hint {
	arrows := "j/k"
	if a.ascii {
		arrows = "j/k"
	}
	return []ui.Hint{hint("Enter", "Choose", 0), hint(arrows, "Move", 1), hint("Esc", "Close", 0)}
}
