package wspane

import (
	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/complete"
	"github.com/shubham/postctl/internal/tui/vimfield"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// fieldSpec says what a text field of the pane looks like.
type fieldSpec struct {
	// single makes a one-line field: Enter submits instead of adding a line.
	single bool
	// lang names a chroma lexer ("json"); empty for none.
	lang string
	// secret fields yank a mask instead of their text. They are masked by
	// the owner with SetMask while the value is a literal.
	secret      bool
	placeholder string
	// plain drops variable highlighting and completion (prompts such as the
	// log search and the template name).
	plain bool
}

// newField builds a vim field wired to the pane: {{variable}} highlighting and
// completion read the pane's current scope, yanks go through the pane's
// clipboard, secret fields never yank their text.
func (p *Pane) newField(spec fieldSpec) *vimfield.Field {
	o := vimtext.Options{
		SingleLine:  spec.single,
		Placeholder: spec.placeholder,
		Clipboard:   p.deps.Clipboard,
	}
	if spec.plain {
		return vimfield.New(p.th, o)
	}
	var base vimtext.Highlighter
	if spec.lang != "" {
		base = vimtext.ChromaHighlighter(p.th, spec.lang)
	}
	o.Highlighter = vimfield.VarHighlighter(p.th, base, func(name string) bool { return p.sc.known(name) })
	o.Completer = complete.Vars(p.layers, complete.Glyphs{Mask: p.th.Glyphs.Mask, Dot: p.th.Glyphs.Dot, Ellipsis: p.th.Glyphs.Ellipsis})
	if spec.secret {
		mask := p.th.Glyphs.Mask
		o.Redact = func(s string) string {
			if s == "" {
				return ""
			}
			return mask
		}
		o.Completer = nil
	}
	return vimfield.New(p.th, o)
}

// layers lists the variable scopes the open request resolves against, lowest
// precedence first: globals, the collection, each folder (outermost first),
// then the active environment.
func (p *Pane) layers() []complete.Layer {
	var out []complete.Layer
	if p.ws != nil && p.ws.Globals != nil {
		out = append(out, complete.Layer{Scope: "global", Variables: p.ws.Globals.Variables})
	}
	for _, c := range p.chain {
		switch v := c.(type) {
		case *model.Collection:
			out = append(out, complete.Layer{Scope: "collection", Variables: v.Variables})
		case *model.Folder:
			out = append(out, complete.Layer{Scope: "folder " + v.Name, Variables: v.Variables})
		}
	}
	if p.activeEnv != nil {
		out = append(out, complete.Layer{Scope: "environment", Variables: p.activeEnv.Variables})
	}
	return out
}

// forward turns a normal-mode field event into the message that hands the key
// (or the two-key sequence) to the shell. Insert and visual mode keep them:
// there Space, ":" or "q" are text.
func forward(f *vimfield.Field, ev vimtext.Event) tea.Cmd {
	if f == nil || f.Mode() != vimtext.Normal {
		return nil
	}
	switch ev.Kind {
	case vimtext.Unhandled:
		return msgCmd(tui.ForwardKeyMsg{Key: ev.Key})
	case vimtext.Seq:
		return msgCmd(tui.ForwardSeqMsg{Seq: ev.Seq})
	}
	return nil
}

// batch drops nil commands.
func batch(cmds ...tea.Cmd) tea.Cmd { return tea.Batch(cmds...) }

// activeField is the text field that has the keyboard: the URL bar, the
// composer, or the open tab's focused field. Nil when none has it.
func (p *Pane) activeField() *vimfield.Field {
	if !p.editable() || p.confirm != nil {
		return nil
	}
	if p.searching {
		return p.search
	}
	if p.rename != nil {
		return p.rename
	}
	if p.urlE {
		return p.url
	}
	if p.zone != zBody {
		return nil
	}
	switch p.tab {
	case tabMessages:
		if p.sect == sComposer {
			return p.comp
		}
	case tabParams:
		return p.params.field()
	case tabHeaders:
		return p.headers.field()
	case tabAuth:
		return p.auth.field()
	default:
		return p.settings.field()
	}
	return nil
}

// fieldHints are the footer hints for a focused field. How to leave depends on
// the mode: Esc goes to normal mode first, then (labelled leave) out of the
// field.
func fieldHints(f *vimfield.Field, leave string) []ui.Hint {
	if f != nil && f.Typing() {
		return []ui.Hint{{Key: "Esc", Label: "Normal mode", Priority: 1}}
	}
	return []ui.Hint{{Key: "Esc", Label: leave, Priority: 1}, {Key: "i", Label: "Insert", Priority: 2}}
}
