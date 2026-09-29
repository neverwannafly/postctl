package request

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// externalEditable is implemented by tabs whose content can round-trip
// through $EDITOR.
type externalEditable interface {
	// external returns a file extension (with dot) and the current text, or
	// ok=false when there is nothing to edit.
	external(p *Pane) (ext, text string, ok bool)
	// applyExternal writes edited text back into the request.
	applyExternal(p *Pane, text string)
}

// editorIntent remembers what an open external editor is for.
type editorIntent struct {
	id     model.ID
	tab    tabID
	before string
}

// editorDoneMsg reports the external editor exiting; text is the edited file.
type editorDoneMsg struct {
	text string
	err  error
}

// openEditor hands the terminal to $EDITOR on a temporary file holding the
// current tab's text (raw or GraphQL body, script, or bulk rows). The result
// is applied when the editor exits. The temporary file is private and removed
// as soon as it has been read.
func (p *Pane) openEditor() tea.Cmd {
	if !p.editable() {
		return nil
	}
	ee, ok := p.views[p.tab].(externalEditable)
	if !ok {
		return p.setNotice("Nothing to open in $EDITOR on this tab")
	}
	ext, text, ok := ee.external(p)
	if !ok {
		return p.setNotice("Nothing to open in $EDITOR here")
	}
	dir := p.deps.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "postctl-*"+ext)
	if err != nil {
		return p.setNotice("Could not create a temporary file")
	}
	path := f.Name()
	_, werr := f.WriteString(text)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(path)
		return p.setNotice("Could not write the temporary file")
	}
	p.stopViews()
	p.editorReq = &editorIntent{id: p.req.ID, tab: p.tab, before: text}
	cmd := p.deps.editorCmd(path)
	return p.deps.exec(cmd, func(err error) tea.Msg {
		defer func() { _ = os.Remove(filepath.Clean(path)) }()
		if err != nil {
			return editorDoneMsg{err: err}
		}
		data, rerr := os.ReadFile(path) //nolint:gosec // the private temp file created above
		return editorDoneMsg{text: string(data), err: rerr}
	})
}

func (p *Pane) onEditorDone(m editorDoneMsg) tea.Cmd {
	in := p.editorReq
	p.editorReq = nil
	if in == nil {
		return nil
	}
	if m.err != nil {
		return p.setNotice("The editor did not finish; nothing changed")
	}
	if p.req == nil || p.req.ID != in.id {
		return p.setNotice("The request changed while editing; nothing applied")
	}
	text := m.text
	if !strings.HasSuffix(in.before, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	if text == in.before {
		return p.setNotice("No changes")
	}
	ee, ok := p.views[in.tab].(externalEditable)
	if !ok {
		return nil
	}
	pre := p.req.Clone()
	ee.applyExternal(p, text)
	p.stopAll()
	p.reloadAll()
	return tea.Batch(p.record(pre, true), p.setNotice("Updated from the editor"))
}
