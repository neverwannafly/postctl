package history

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

func (m *Model) requestExists(id model.ID) bool {
	if m.env.Workspace == nil {
		return false
	}
	for _, c := range m.env.Workspace.Collections {
		if _, _, _, ok := c.Find(id); ok {
			return true
		}
	}
	return false
}

func (m *Model) beginRestore() (tui.Screen, tea.Cmd) {
	e := m.detail
	if e == nil {
		return m, nil
	}
	if reason := restoreDisabledReason(e); reason != "" {
		m.notice, m.fail = reason, true
		return m, nil
	}
	m.phase = phConfirmRestore
	m.restoreNew = !m.requestExists(e.RequestID)
	body := "Replace method, URL, query parameters, headers and body.\nAuth, scripts and settings stay as they are."
	if m.restoreNew {
		body += "\nThe original request was deleted; restore will create a new request named \"" + restoreName(e.RequestName) + "\"."
		if tmplURLForNew(e) == "" {
			body += "\nThe saved template has no restorable URL; the new request starts with an empty URL."
		}
	}
	m.conf = ui.Confirm{
		Title:   "Restore into request",
		Body:    body,
		Choices: restoreChoices(m.restoreNew),
		Focus:   0,
	}
	return m, nil
}

func restoreDisabledReason(e *history.Entry) string {
	if e == nil || e.Template == nil {
		return "Restore unavailable: this entry has no request template (recorded before restore support)."
	}
	if e.Template.Kind == model.KindWebSocket {
		return "Restore unavailable: WebSocket requests cannot be restored from history yet."
	}
	return ""
}

func restoreChoices(asNew bool) []string {
	if asNew {
		return []string{"Cancel", "Restore as new request"}
	}
	return []string{"Cancel", "Restore into request"}
}

func restoreName(name string) string {
	if strings.HasSuffix(name, " (restored)") {
		return name
	}
	return name + " (restored)"
}

func (m *Model) confirmRestoreKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.phase = phDetail
	case "left", "h":
		m.conf.Focus = 0
	case "right", "l":
		m.conf.Focus = 1
	case "enter":
		if m.conf.Focus == 0 {
			m.phase = phDetail
			return m, nil
		}
		return m.runRestore()
	}
	return m, nil
}

func (m *Model) runRestore() (tui.Screen, tea.Cmd) {
	e := m.detail
	if e == nil || e.Template == nil {
		return m, nil
	}
	if reason := restoreDisabledReason(e); reason != "" {
		m.notice, m.fail = reason, true
		m.phase = phDetail
		return m, nil
	}
	tmpl := *e.Template
	targetID := e.RequestID
	var cmds []tea.Cmd
	if m.restoreNew {
		coll, parent, _ := insertParent(m.env.Workspace, e)
		if coll == nil {
			m.notice, m.fail = "Could not create a request in the workspace.", true
			m.phase = phDetail
			return m, nil
		}
		m.phase = phDetail
		m.notice = "Restoring into a new request…"
		return nil, msgCmd(tui.CreateRequestFromHistoryMsg{
			CollectionID: coll.ID,
			ParentID:     parent.ContainerID(),
			Name:         restoreName(e.RequestName),
			Template:     tmpl,
		})
	}
	cmds = append(cmds,
		msgCmd(tui.SelectRequestMsg{RequestID: targetID}),
		msgCmd(tui.ApplyRequestPatchMsg{RequestID: targetID, Template: tmpl}),
	)
	m.phase = phDetail
	m.notice = "Restoring into request…"
	return nil, tea.Sequence(cmds...)
}

func tmplURLForNew(e *history.Entry) string {
	if e.Template == nil {
		return ""
	}
	if history.FieldMasked(e.Template.URLMasked, e.Template.URL) || !history.RestoreSafeValue(e.Template.URL) {
		return ""
	}
	return e.Template.URL
}

func insertParent(ws *model.Workspace, e *history.Entry) (*model.Collection, model.Container, int) {
	var coll *model.Collection
	for _, c := range ws.Collections {
		if e.CollectionID != "" && c.ID == e.CollectionID {
			coll = c
			break
		}
	}
	if coll == nil && len(ws.Collections) > 0 {
		coll = ws.Collections[0]
	}
	if coll == nil {
		return nil, nil, 0
	}
	parent := model.Container(coll)
	names := e.Path
	if len(names) > 2 {
		names = names[1 : len(names)-1]
	} else {
		names = nil
	}
	for _, name := range names {
		var next model.Container
		for _, it := range parent.Children() {
			if f, ok := it.(*model.Folder); ok && f.Name == name {
				next = f
				break
			}
		}
		if next == nil {
			break
		}
		parent = next
	}
	return coll, parent, -1
}

func msgCmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// restoreHint explains why restore is disabled in the detail footer.
func restoreHint(e *history.Entry) string {
	return restoreDisabledReason(e)
}
