package envedit

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/listmotion"
	"github.com/shubham/postctl/internal/tui/screenfield"
	"github.com/shubham/postctl/internal/ui"
)

func (m *Model) rows() []model.Variable {
	if l := m.list(); l != nil {
		return *l
	}
	return nil
}

func (m *Model) rowsH() int { return max(1, m.h-envScreenHeaderRows-envTableChromeRows) }

// ensureList returns the current list, creating globals on first use.
func (m *Model) ensureList() *[]model.Variable {
	if m.tab == tabGlobals && m.ws().Globals == nil {
		m.ws().Globals = &model.Globals{ID: model.NewID(), Name: "Globals"}
	}
	return m.list()
}

func (m *Model) changed() tea.Cmd {
	if k, ok := m.saveTarget(); ok {
		return m.touch(k)
	}
	return nil
}

func (m *Model) tableKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	rows := m.rows()
	s := k.String()
	nav := listmotion.View{Cursor: m.cur, Top: m.top}
	if r := listmotion.Key(k, listmotion.Config{Count: len(rows), Page: m.rowsH()}, &nav); r.Handled {
		m.cur, m.top = nav.Cursor, nav.Top
		return m, nil
	}
	switch s {
	case "esc":
		if m.tab == tabGlobals {
			return m.close()
		}
		m.zone = zList
	case "tab", "shift+tab":
		if m.tab != tabGlobals {
			m.zone = zList
		}
	case "a", "insert":
		if l := m.ensureList(); l != nil {
			return m, m.addRow(l)
		}
	case "enter":
		if len(rows) == 0 {
			if l := m.ensureList(); l != nil {
				return m, m.addRow(l)
			}
			return m, nil
		}
		return m, m.beginEditWithHydrate(1)
	case "i":
		if len(rows) == 0 {
			if l := m.ensureList(); l != nil {
				return m, m.addRow(l)
			}
			return m, nil
		}
		return m, m.beginEditWithHydrate(m.col)
	case "space", " ":
		if l := m.list(); l != nil && m.cur < len(*l) {
			(*l)[m.cur].Disabled = !(*l)[m.cur].Disabled
			return m, m.changed()
		}
	case "d", "delete", "backspace":
		if l := m.list(); l != nil && m.cur < len(*l) {
			*l = append((*l)[:m.cur:m.cur], (*l)[m.cur+1:]...)
			m.cur = min(m.cur, max(0, len(*l)-1))
			return m, m.changed()
		}
	case "alt+up", "shift+up":
		return m, m.move(-1)
	case "alt+down", "shift+down":
		return m, m.move(1)
	case "s":
		return m, m.toggleSecret()
	case "r":
		return m, m.toggleReveal()
	}
	return m, nil
}

func (m *Model) move(dir int) tea.Cmd {
	l := m.list()
	j := m.cur + dir
	if l == nil || m.cur >= len(*l) || j < 0 || j >= len(*l) {
		return nil
	}
	(*l)[m.cur], (*l)[j] = (*l)[j], (*l)[m.cur]
	m.cur = j
	return m.changed()
}

func (m *Model) toggleReveal() tea.Cmd {
	if m.reveal {
		m.reveal = false
		return nil
	}
	m.reveal = true
	l := m.list()
	if l == nil {
		return nil
	}
	need := false
	for _, v := range *l {
		if v.IsSecret() && v.Value == "" && v.SecretRef != "" {
			need = true
		}
	}
	if !need {
		return nil
	}
	return m.hydrate(m.ownerKey(), cloneVars(*l), thenReveal)
}

func (m *Model) toggleSecret() tea.Cmd {
	l := m.list()
	if l == nil || m.cur >= len(*l) {
		return nil
	}
	v := &(*l)[m.cur]
	if !v.IsSecret() {
		v.Type = model.VarSecret
		return tea.Batch(m.changed(), m.setNotice("Now secret: kept in the Keychain, never written to the file"))
	}
	m.conf = ui.NewConfirm("Store as plain text?",
		"“"+v.Key+"” will be written to the environment file in plain text, where anyone with the file can read it.",
		"Make plain", true)
	if m.th.ASCII {
		m.conf.Body = `"` + v.Key + `" will be written to the environment file in plain text, where anyone with the file can read it.`
	}
	m.confFn = func() tea.Cmd {
		l := m.list()
		if l == nil || m.cur >= len(*l) || !(*l)[m.cur].IsSecret() {
			return nil
		}
		cur := &(*l)[m.cur]
		if cur.Value == "" && cur.SecretRef != "" {
			// The value has not been read yet; read it before it is written out.
			return m.hydrate(m.ownerKey(), cloneVars(*l), thenUnsecret)
		}
		cur.Type = model.VarDefault
		cur.SecretRef = ""
		return m.changed()
	}
	m.mode = modeConfirm
	return nil
}

func (m *Model) addRow(l *[]model.Variable) tea.Cmd {
	at := len(*l)
	if m.cur < len(*l) {
		at = m.cur + 1
	}
	*l = append(*l, model.Variable{})
	copy((*l)[at+1:], (*l)[at:])
	(*l)[at] = model.Variable{Disabled: true}
	m.cur = at
	m.draft = true
	m.beginEdit(0)
	return m.changed()
}

func (m *Model) field(v model.Variable, col int) string {
	switch col {
	case 0:
		return v.Key
	case 1:
		return v.Value
	}
	return v.Description
}

func (m *Model) beginEditWithHydrate(col int) tea.Cmd {
	m.beginEdit(col)
	return m.hydrateIfSecretValueEmpty()
}

func (m *Model) hydrateIfSecretValueEmpty() tea.Cmd {
	l := m.list()
	if l == nil || m.cur >= len(*l) {
		return nil
	}
	v := (*l)[m.cur]
	if m.col == 1 && v.IsSecret() && v.Value == "" && v.SecretRef != "" && !m.reveal {
		return m.hydrate(m.ownerKey(), cloneVars(*l), thenEdit)
	}
	return nil
}

func (m *Model) beginEdit(col int) {
	l := m.list()
	if l == nil || m.cur >= len(*l) {
		return
	}
	v := (*l)[m.cur]
	m.edit = true
	m.col = col
	val := m.field(v, col)
	masked := col == 1 && v.IsSecret() && !m.reveal
	ph := [...]string{"name", "value", "description"}[col]
	if col == 1 && v.IsSecret() && v.Value == "" && v.SecretRef != "" && !m.reveal {
		val = ""
		ph = "unchanged (hidden)"
	}
	f, _ := screenfield.New(m.th, val, ph, masked)
	m.in = f
	m.in.SetWidth(max(4, m.w-envValueLabelWidth))
	m.editBaseline = val
	m.editSkipValue = col == 1 && v.IsSecret() && v.Value == "" && v.SecretRef != "" && !m.reveal
}

func (m *Model) applyTextIfChanged() tea.Cmd {
	if m.in == nil {
		return nil
	}
	t := m.in.Value()
	if t == m.editBaseline {
		return nil
	}
	if m.editSkipValue && t == "" {
		return nil
	}
	return m.applyText()
}

func (m *Model) applyText() tea.Cmd {
	l := m.list()
	if l == nil || m.cur >= len(*l) {
		return nil
	}
	v := &(*l)[m.cur]
	t := m.in.Value()
	switch m.col {
	case 0:
		v.Key = t
	case 1:
		v.Value = t
	default:
		v.Description = t
	}
	if m.draft {
		v.Disabled = v.Key == "" && v.Value == ""
	}
	return m.changed()
}

// finish leaves cell editing, dropping a new row that stayed empty.
func (m *Model) finish() tea.Cmd {
	var cmd tea.Cmd
	if m.edit && m.draft {
		if l := m.list(); l != nil && m.cur < len(*l) {
			v := (*l)[m.cur]
			switch {
			case v.Key == "" && v.Value == "" && v.Description == "":
				*l = append((*l)[:m.cur:m.cur], (*l)[m.cur+1:]...)
				m.cur = min(m.cur, max(0, len(*l)-1))
				cmd = m.changed()
			case v.Disabled && v.Key != "":
				(*l)[m.cur].Disabled = false
				cmd = m.changed()
			}
		}
	}
	m.edit = false
	m.draft = false
	return cmd
}

func (m *Model) editKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if s == "ctrl+r" {
		l := m.list()
		if l != nil && m.cur < len(*l) && m.col == 1 && (*l)[m.cur].IsSecret() {
			m.reveal = !m.reveal
			m.beginEdit(m.col)
			if m.reveal {
				return m.toggleRevealFromEdit()
			}
			return nil
		}
	}
	submit, changed, cmd, leave, normal := m.in.UpdateEx(k)
	if leave {
		return tea.Batch(cmd, m.finish())
	}
	if submit || (s == "enter" && normal) {
		return tea.Batch(cmd, m.applyTextIfChanged(), m.finish())
	}
	if changed {
		return tea.Batch(cmd, m.applyTextIfChanged())
	}
	switch s {
	case "tab":
		if m.col < 2 {
			return m.beginEditWithHydrate(m.col + 1)
		}
		wasDraft := m.draft
		cmd := m.finish()
		if wasDraft {
			return cmd
		}
		l := m.list()
		if l != nil && m.cur+1 < len(*l) {
			m.cur++
			m.beginEdit(0)
			return cmd
		}
		if l != nil {
			return tea.Batch(cmd, m.addRow(l))
		}
		return cmd
	case "shift+tab":
		if m.col > 0 {
			return m.beginEditWithHydrate(m.col - 1)
		}
		return nil
	case "up", "down":
		col := m.col
		cmd := m.finish()
		next := m.cur + 1
		if s == "up" {
			next = m.cur - 1
		}
		if l := m.list(); l != nil && next >= 0 && next < len(*l) {
			m.cur = next
			m.beginEdit(col)
		}
		return cmd
	}
	return cmd
}

// toggleRevealFromEdit hydrates after Ctrl-R inside a field.
func (m *Model) toggleRevealFromEdit() tea.Cmd {
	l := m.list()
	if l == nil {
		return nil
	}
	for _, v := range *l {
		if v.IsSecret() && v.Value == "" && v.SecretRef != "" {
			return m.hydrate(m.ownerKey(), cloneVars(*l), thenReveal)
		}
	}
	return nil
}
