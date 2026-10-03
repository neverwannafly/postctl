package wspane

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
)

// editorIntent remembers what an open external editor is for.
type editorIntent struct {
	id     model.ID
	before string
	// apply writes the edited text back into the request.
	apply func(p *Pane, text string)
	// line collapses the result to one line (URL, cells, form rows).
	line bool
}

// editorDoneMsg reports the external editor exiting; text is the edited file.
type editorDoneMsg struct {
	text string
	err  error
}

// editorCmd builds the editor command for a file: Deps.EditorCommand, else
// $VISUAL, $EDITOR or vi, run through sh so "code --wait" style values work.
func (d Deps) editorCmd(path string) *exec.Cmd {
	if d.EditorCommand != nil {
		return d.EditorCommand(path)
	}
	ed := os.Getenv("VISUAL")
	if strings.TrimSpace(ed) == "" {
		ed = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(ed) == "" {
		ed = "vi"
	}
	return exec.Command("sh", "-c", ed+` "$1"`, "sh", path) //nolint:gosec // the user's own editor command
}

func (d Deps) exec(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd {
	if d.Exec != nil {
		return d.Exec(cmd, done)
	}
	return tea.ExecProcess(cmd, done)
}

// externalTarget picks what Space o edits: the URL while the URL bar has the
// keyboard, the composer's message, or the single-line cell or row being
// edited. A non-empty notice means the target may not be opened.
func (p *Pane) externalTarget() (ext, text string, apply func(p *Pane, text string), line bool, notice string) {
	nothing := "Nothing to open in $EDITOR here"
	if p.confirm != nil || p.searching || p.rename != nil || p.detail != nil {
		return "", "", nil, false, nothing
	}
	if p.zone == zBar {
		return ".txt", p.req.URL.Raw, func(p *Pane, t string) {
			applyRawURL(&p.req.URL, t)
			p.params.reload(p)
		}, true, ""
	}
	if p.zone != zBody {
		return "", "", nil, false, nothing
	}
	switch p.tab {
	case tabMessages:
		if p.sect != sComposer {
			return "", "", nil, false, nothing
		}
		ext = ".txt"
		if p.format() == model.WSJSON {
			ext = ".json"
		}
		return ext, p.comp.Text(), func(p *Pane, t string) {
			d := p.draft()
			d.Body = t
			p.setDraft(d)
		}, false, ""
	case tabParams:
		return p.params.fieldExternal(p, nothing)
	case tabHeaders:
		return p.headers.fieldExternal(p, nothing)
	case tabAuth:
		return p.auth.f.fieldExternal(func() []formRow { return p.auth.rows(p) }, nothing)
	default:
		return p.settings.f.fieldExternal(func() []formRow { return p.settings.rows(p) }, nothing)
	}
}

// fieldExternal returns the open table cell for $EDITOR.
func (k *kvView) fieldExternal(p *Pane, nothing string) (ext, text string, apply func(*Pane, string), line bool, notice string) {
	if !k.edit || k.in == nil {
		return "", "", nil, false, nothing
	}
	row, col, draft := k.cur, k.col, k.draft
	return ".txt", k.in.Text(), func(p *Pane, t string) {
		items := k.items(p)
		if row >= len(items) {
			return
		}
		it := &items[row]
		if col == 0 {
			it.Key = t
		} else {
			it.Value = t
		}
		if draft {
			it.Disabled = it.Key == "" && it.Value == ""
		}
		storeItems(k.kind, p.req, items)
		if k.kind == kvParams {
			p.syncURL()
		}
	}, true, ""
}

// fieldExternal returns the open form row for $EDITOR. A hidden secret
// literal never leaves the app: the notice says how to reveal it.
func (f *form) fieldExternal(rows func() []formRow, nothing string) (ext, text string, apply func(*Pane, string), line bool, notice string) {
	if !f.edit || f.in == nil {
		return "", "", nil, false, nothing
	}
	rs := rows()
	f.clamp(rs)
	r := rs[f.cur]
	if f.maskFor(r, f.in.Text()) {
		return "", "", nil, false, "Reveal the secret first (Ctrl-R), then open it in $EDITOR"
	}
	cur := f.cur
	return ".txt", f.in.Text(), func(p *Pane, t string) {
		rs := rows()
		if cur < len(rs) && rs[cur].set != nil {
			rs[cur].set(t)
		}
	}, true, ""
}

// openEditor hands the terminal to $EDITOR on a private temporary file that
// holds the text of the field with the keyboard. The result is applied when
// the editor exits and the file is removed as soon as it has been read.
func (p *Pane) openEditor() tea.Cmd {
	if !p.editable() {
		return nil
	}
	ext, text, apply, line, notice := p.externalTarget()
	if notice != "" {
		return p.setNotice(notice, false)
	}
	dir := p.deps.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "postctl-*"+ext)
	if err != nil {
		return p.setNotice("Could not create a temporary file", true)
	}
	path := f.Name()
	_, werr := f.WriteString(text)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(path)
		return p.setNotice("Could not write the temporary file", true)
	}
	p.stopEditing()
	p.editorReq = &editorIntent{id: p.req.ID, before: text, apply: apply, line: line}
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
		return p.setNotice("The editor did not finish; nothing changed", true)
	}
	if p.req == nil || p.req.ID != in.id {
		return p.setNotice("The request changed while editing; nothing applied", true)
	}
	text := m.text
	if in.line {
		text = collapseLine(text)
	} else if !strings.HasSuffix(in.before, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	if text == in.before {
		return p.setNotice("No changes", false)
	}
	wasBar := p.zone == zBar
	in.apply(p, text)
	p.stopEditing()
	p.reloadAll()
	if wasBar {
		p.setZone(zBar) // back in the URL field, in normal mode
	}
	return tea.Batch(p.touched(), p.setNotice("Updated from the editor", false))
}

// collapseLine turns text from $EDITOR into one line for a single-line field:
// the final newline goes, inner line breaks become spaces.
func collapseLine(s string) string {
	s = strings.TrimRight(s, "\r\n")
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}
