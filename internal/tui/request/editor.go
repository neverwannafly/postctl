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

// fieldEditable is implemented by tabs whose open single-line field (a table
// cell, a form row) can round-trip through $EDITOR. apply is nil when no such
// field is open; notice, when set, explains why it may not leave the app (a
// hidden secret).
type fieldEditable interface {
	fieldExternal(p *Pane) (text string, apply func(p *Pane, text string), notice string)
}

// editorIntent remembers what an open external editor is for.
type editorIntent struct {
	id     model.ID
	tab    tabID
	before string
	// apply writes the edited text for a single-line target (the URL, a cell,
	// a form row); nil means the whole tab, through externalEditable.
	apply func(p *Pane, text string)
	// line collapses the result to one line.
	line bool
}

// externalTarget picks what Space o edits: the URL while the URL bar has the
// keyboard, the single-line cell or row being edited, otherwise the open
// tab's whole text (body, script, bulk rows). A non-empty notice means the
// target may not be opened.
func (p *Pane) externalTarget() (ext, text string, apply func(p *Pane, text string), line bool, notice string) {
	if p.msel != nil || p.confirm != nil {
		return "", "", nil, false, "Nothing to open in $EDITOR here"
	}
	if p.zone == zBar {
		return ".txt", p.req.URL.Raw, func(p *Pane, t string) { applyRawURL(&p.req.URL, t) }, true, ""
	}
	if fe, ok := p.views[p.tab].(fieldEditable); ok && p.views[p.tab].editing() {
		t, ap, n := fe.fieldExternal(p)
		if n != "" {
			return "", "", nil, false, n
		}
		if ap != nil {
			return ".txt", t, ap, true, ""
		}
	}
	if ee, ok := p.views[p.tab].(externalEditable); ok {
		if e, t, ok := ee.external(p); ok {
			return e, t, nil, false, ""
		}
	}
	return "", "", nil, false, "Nothing to open in $EDITOR here"
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
	ext, text, apply, line, notice := p.externalTarget()
	if notice != "" {
		return p.setNotice(notice)
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
	p.url.Blur()
	p.editorReq = &editorIntent{id: p.req.ID, tab: p.tab, before: text, apply: apply, line: line}
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

// resumeBar hands the keyboard back to the URL field after a round trip that
// applied nothing: openEditor blurred the field, and a bar that stays blurred
// ignores every key.
func (p *Pane) resumeBar() {
	if p.zone == zBar {
		p.setZone(zBar)
	}
}

func (p *Pane) onEditorDone(m editorDoneMsg) tea.Cmd {
	in := p.editorReq
	p.editorReq = nil
	if in == nil {
		return nil
	}
	if m.err != nil {
		p.resumeBar()
		return p.setNotice("The editor did not finish; nothing changed")
	}
	if p.req == nil || p.req.ID != in.id {
		return p.setNotice("The request changed while editing; nothing applied")
	}
	text := m.text
	if in.line {
		text = collapseLine(text)
	} else if !strings.HasSuffix(in.before, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	if text == in.before {
		p.resumeBar()
		return p.setNotice("No changes")
	}
	pre := p.req.Clone()
	if in.apply != nil {
		in.apply(p, text)
	} else {
		ee, ok := p.views[in.tab].(externalEditable)
		if !ok {
			return nil
		}
		ee.applyExternal(p, text)
	}
	wasBar := p.zone == zBar
	p.stopAll()
	p.reloadAll()
	if wasBar {
		p.setZone(zBar) // back in the URL field, in normal mode
	}
	return tea.Batch(p.record(pre, true), p.setNotice("Updated from the editor"))
}
