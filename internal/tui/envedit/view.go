package envedit

import (
	"strings"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

const listW = 28

// wide reports whether there is room for the list beside the table.
func (m *Model) wide() bool { return m.w >= 96 }

// KeyHints implements tui.Screen.
func (m *Model) KeyHints() []ui.Hint {
	switch {
	case m.mode == modeConfirm:
		return []ui.Hint{{Key: "Enter", Label: "Choose", Priority: 1}, {Key: "←→", Label: "Switch", Priority: 2}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	case m.mode == modeName:
		return []ui.Hint{{Key: "Enter", Label: "Save name", Priority: 1}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	case m.edit:
		return []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}, {Key: "Tab", Label: "Next field", Priority: 2}, {Key: "Ctrl-R", Label: "Reveal", Priority: 4}}
	case m.zone == zList && m.tab != tabGlobals:
		hs := []ui.Hint{{Key: "↑↓", Label: "Choose", Priority: 2}, {Key: "Enter", Label: "Variables", Priority: 1}}
		if m.tab == tabEnvs {
			hs = append(hs,
				ui.Hint{Key: "n", Label: "New", Priority: 2},
				ui.Hint{Key: "r", Label: "Rename", Priority: 3},
				ui.Hint{Key: "c", Label: "Duplicate", Priority: 4},
				ui.Hint{Key: "d", Label: "Delete", Priority: 3},
				ui.Hint{Key: "p", Label: "Production", Priority: 4})
		}
		return append(hs, ui.Hint{Key: "[ ]", Label: "Tab", Priority: 5}, ui.Hint{Key: "Esc", Label: "Close", Priority: 1})
	}
	esc := "List"
	if m.tab == tabGlobals {
		esc = "Close"
	}
	return []ui.Hint{
		{Key: "a", Label: "Add", Priority: 2},
		{Key: "Enter", Label: "Edit", Priority: 1},
		{Key: "s", Label: "Secret", Priority: 3},
		{Key: "r", Label: "Reveal", Priority: 3},
		{Key: "Space", Label: "Toggle", Priority: 4},
		{Key: "d", Label: "Delete", Priority: 4},
		{Key: "Esc", Label: esc, Priority: 1},
	}
}

// View implements tui.Screen.
func (m *Model) View() string {
	th := m.th
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	title := th.Bold().Render("Environments and variables")
	label, failed := m.saveLabel()
	right := ""
	switch {
	case failed:
		right = th.Render(ui.StatusDanger, label)
	case m.notice != "":
		right = th.Render(ui.TextSecondary, th.Truncate(m.notice, max(4, m.w/2)))
	case label != "":
		right = th.Render(ui.TextQuiet, label)
	}
	if right != "" {
		title = th.Fit(title, max(1, m.w-ui.Width(right)-1)) + " " + right
	}
	tabs := make([]ui.Tab, len(tabNames))
	for i, n := range tabNames {
		tabs[i].Label = n
	}
	tabs[tabEnvs].Count = len(m.envs())
	tabs[tabCollections].Count = len(m.ws().Collections)
	head := title + "\n" + th.Tabs(m.w, tabs, int(m.tab), m.zone == zList && m.mode == modeNone)
	ch := max(1, m.h-3)
	var body string
	switch m.mode {
	case modeConfirm:
		body = th.Block(th.ConfirmView(m.conf, min(m.w, 66)), m.w, ch)
	case modeName:
		nl := m.nameLine(m.w)
		body = m.content(m.w, max(1, ch-1-strings.Count(nl, "\n"))) + "\n" + nl
	default:
		body = m.content(m.w, ch)
	}
	return th.Block(head+"\n"+th.Block(body, m.w, ch), m.w, m.h)
}

func (m *Model) content(w, h int) string {
	th := m.th
	if m.tab == tabGlobals {
		return m.tablePane(w, h, "Globals", "Available in every request. Environments and collections override them.")
	}
	items := m.itemNames()
	if len(items) == 0 {
		if m.tab == tabEnvs {
			body := th.EmptyState("No environments yet",
				"An environment holds values such as baseUrl for staging or production, so one request works everywhere.",
				"Press n to create one.", w)
			return body
		}
		return th.EmptyState("No collections", "Create a collection first; its variables are edited here.", "", w)
	}
	if m.wide() {
		lw := listW
		left := m.listPane(lw, h)
		right := m.rightPane(w-lw-3, h)
		return joinCols(th, left, right, lw, h)
	}
	sel := m.selectorLine(w)
	return sel + "\n" + m.rightPane(w, h-1)
}

func joinCols(th ui.Theme, left, right string, lw, h int) string {
	l := strings.Split(th.Block(left, lw, h), "\n")
	r := strings.Split(right, "\n")
	var out []string
	gap := th.Gap()
	for i := range h {
		rr := ""
		if i < len(r) {
			rr = r[i]
		}
		out = append(out, l[i]+gap+rr)
	}
	return strings.Join(out, "\n")
}

type item struct {
	name       string
	production bool
	active     bool
}

func (m *Model) items() []item {
	var out []item
	switch m.tab {
	case tabEnvs:
		for _, e := range m.envs() {
			out = append(out, item{name: e.Name, production: e.Production, active: e.ID == m.env.ActiveEnv})
		}
	case tabCollections:
		for _, c := range m.ws().Collections {
			out = append(out, item{name: c.Name})
		}
	}
	return out
}

func (m *Model) itemNames() []string {
	its := m.items()
	out := make([]string, len(its))
	for i, it := range its {
		out[i] = it.name
	}
	return out
}

func (m *Model) tags(it item) string {
	th := m.th
	var parts []string
	if it.production {
		parts = append(parts, th.Render(ui.StatusWarning, "PROD"))
	}
	if it.active {
		parts = append(parts, th.Render(ui.StatusSuccess, th.Glyphs.OK))
	}
	return strings.Join(parts, " ")
}

func (m *Model) listPane(w, h int) string {
	th := m.th
	title := "Environments"
	if m.tab == tabCollections {
		title = "Collections"
	}
	lines := []string{th.Section(title, itoa(len(m.items())), m.zone == zList)}
	its := m.items()
	cur := m.sel[m.tab]
	rowsH := max(1, h-3)
	top := scroll(0, cur, rowsH, len(its))
	for i := top; i < min(len(its), top+rowsH); i++ {
		lines = append(lines, m.itemRow(its[i], i == cur, w))
	}
	if m.tab == tabEnvs {
		lines = append(lines, "", th.Render(ui.TextQuiet, th.Truncate(th.Glyphs.OK+" active  PROD production", w)))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) itemRow(it item, selected bool, w int) string {
	th := m.th
	marker := "  "
	if selected && m.zone == zList {
		marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
	} else if selected {
		marker = th.Render(ui.TextQuiet, th.Glyphs.Select) + " "
	}
	tags := m.tags(it)
	tw := ui.Width(tags)
	nameW := max(4, w-2-tw-1)
	tok := ui.TextPrimary
	if selected {
		tok = ui.AccentPrimary
	}
	name := th.Render(tok, th.Fit(it.name, nameW))
	if tw == 0 {
		return marker + name
	}
	return marker + name + " " + tags
}

func (m *Model) selectorLine(w int) string {
	th := m.th
	its := m.items()
	cur := m.sel[m.tab]
	label := "Environment"
	if m.tab == tabCollections {
		label = "Collection"
	}
	marker := "  "
	l, r := "‹", "›"
	if th.ASCII {
		l, r = "<", ">"
	}
	if m.zone == zList {
		marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
	}
	it := its[min(cur, len(its)-1)]
	line := marker + th.Render(ui.TextSecondary, th.Fit(label, 12)) + th.Render(ui.TextQuiet, l) + " " +
		th.Render(ui.TextPrimary, it.name) + " " + th.Render(ui.TextQuiet, r)
	if t := m.tags(it); t != "" {
		line += "  " + t
	}
	line += "  " + th.Render(ui.TextQuiet, itoa(cur+1)+"/"+itoa(len(its)))
	return th.Truncate(line, w)
}

func (m *Model) nameLine(w int) string {
	th := m.th
	label := map[nameIntent]string{intentNew: "New name", intentRename: "Rename to", intentDuplicate: "Copy name"}[m.intent]
	line := th.Render(ui.TextSecondary, th.Fit(label, 11)) + m.name.View(th, max(4, w-12), true)
	if m.nameEr != "" {
		line += "\n" + th.Render(ui.StatusWarning, th.Truncate(th.Glyphs.Warn+" "+m.nameEr, w))
	}
	return line
}

func (m *Model) rightPane(w, h int) string {
	switch m.tab {
	case tabEnvs:
		e := m.curEnv()
		if e == nil {
			return ""
		}
		note := "Overrides collection variables and globals."
		if e.Production {
			note = m.th.Glyphs.Warn + " Production: sends use live data."
		}
		return m.tablePane(w, h, e.Name, note)
	default:
		c := m.curColl()
		if c == nil {
			return ""
		}
		return m.tablePane(w, h, c.Name, "Collection variables, used by every request in it.")
	}
}

// tablePane draws the header, the variables and the edit line.
func (m *Model) tablePane(w, h int, name, note string) string {
	t := m.th
	rows := m.rows()
	header := t.Section(name, plural(len(rows), "variable"), m.zone == zTable) + "  " + t.Render(ui.TextQuiet, t.Truncate(note, max(4, w-ui.Width(name)-16)))
	header = t.Truncate(header, w)
	if len(rows) == 0 && !m.edit {
		empty := t.Render(ui.TextSecondary, "No variables. Press a to add one.")
		return header + "\n\n" + empty
	}
	rh := max(1, h-4)
	top := scroll(m.top, m.cur, rh, len(rows))
	end := min(len(rows), top+rh)
	kv := make([]ui.KVRow, 0, end-top)
	for _, v := range rows[top:end] {
		kv = append(kv, ui.KVRow{Key: v.Key, Value: v.Value, Description: v.Description, Disabled: v.Disabled, Secret: v.IsSecret()})
	}
	focus := -1
	if m.zone == zTable {
		focus = m.cur - top
	}
	table := t.KVTable(kv, ui.KVTableOptions{Width: w, Focus: focus, Header: true, Reveal: m.reveal})
	pad := strings.Repeat("\n", max(0, rh+1-(strings.Count(table, "\n")+1)))
	return header + "\n" + table + pad + "\n" + m.statusLine(rows, w)
}

func (m *Model) statusLine(rows []model.Variable, w int) string {
	t := m.th
	if m.edit && m.in != nil {
		label := [...]string{"Name", "Value", "Description"}[m.col]
		if m.col == 1 && m.cur < len(rows) && rows[m.cur].IsSecret() {
			label = "Value (secret)"
		}
		return "\n" + t.Render(ui.TextSecondary, t.Fit(label, 15)) + m.in.View(t, max(4, w-15), true)
	}
	secrets, off := 0, 0
	for _, v := range rows {
		if v.IsSecret() {
			secrets++
		}
		if v.Disabled {
			off++
		}
	}
	parts := []string{}
	if secrets > 0 {
		s := itoa(secrets) + " secret"
		if m.reveal {
			s += " (shown)"
		} else {
			s += " (hidden, r to show)"
		}
		parts = append(parts, s)
	}
	if off > 0 {
		parts = append(parts, itoa(off)+" disabled")
	}
	return "\n" + t.Render(ui.TextQuiet, t.Truncate(strings.Join(parts, " "+t.Glyphs.Dot+" "), w))
}

func scroll(top, cur, rows, n int) int {
	if cur < top {
		top = cur
	}
	if cur >= top+rows {
		top = cur - rows + 1
	}
	return max(0, min(top, max(0, n-rows)))
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}
