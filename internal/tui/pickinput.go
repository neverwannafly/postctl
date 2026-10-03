package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui/vimtext"
	"github.com/shubham/postctl/internal/ui"
)

// pickNav is returned from pickInput.update for list overlays.
type pickNav int

const (
	pickNavNone pickNav = iota
	pickNavUp
	pickNavDown
	pickNavPageUp
	pickNavPageDown
	pickNavSubmit
	pickNavClose
	pickNavChanged
)

// pickInput is a single-line vimtext field for fuzzy picker overlays.
type pickInput struct {
	ed     *vimtext.Model
	prompt string
	width  int
	mouse  vimtext.Mouse
}

func (p *pickInput) mouseUpdate(msg tea.MouseMsg, x, y int, now time.Time) bool {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		p.mouse.Release()
		return m.Button == tea.MouseLeft && p.mouse.Press(p.ed, x-ui.Width(p.prompt), y, now)
	case tea.MouseMotionMsg:
		return m.Button == tea.MouseLeft && p.mouse.Motion(x-ui.Width(p.prompt), y)
	case tea.MouseReleaseMsg:
		return p.mouse.Release()
	case tea.MouseWheelMsg:
		return p.mouse.Active()
	}
	return false
}

func newPickInput(th ui.Theme, ascii bool, prompt, placeholder string) (*pickInput, tea.Cmd) {
	if prompt == "" {
		prompt = "› "
		if ascii {
			prompt = "> "
		}
	}
	ed := vimtext.New(th, vimtext.Options{
		SingleLine:  true,
		StartInsert: true,
		Placeholder: placeholder,
	})
	p := &pickInput{ed: ed, prompt: prompt, width: 8}
	return p, ed.Focus()
}

func (p *pickInput) setPrompt(s string) { p.prompt = s }

func (p *pickInput) setWidth(w int) {
	p.width = max(8, w)
	p.ed.SetSize(p.width, 1)
}

func (p *pickInput) value() string { return strings.TrimSpace(p.ed.Value()) }

// Value returns the trimmed filter text (tests and query sync).
func (p *pickInput) Value() string { return p.value() }

func (p *pickInput) rawValue() string { return p.ed.Value() }

func (p *pickInput) vimMode() string {
	if !p.ed.Focused() {
		return ""
	}
	return p.ed.Mode().String()
}

func (p *pickInput) inserting() bool {
	if !p.ed.Focused() {
		return false
	}
	switch p.ed.Mode() {
	case vimtext.Insert, vimtext.Visual, vimtext.VisualLine:
		return true
	default:
		return false
	}
}

func (p *pickInput) view(th ui.Theme) string {
	prefix := th.Render(ui.AccentPrimary, p.prompt)
	body := p.ed.View()
	// ed.View is width cells; prefix sits before it on the same line.
	return th.Fit(prefix+body, p.width+ui.Width(p.prompt))
}

func (p *pickInput) update(msg tea.Msg, page int) (tea.Cmd, pickNav) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "j", "down", "ctrl+n":
			if p.ed.Mode() == vimtext.Normal || p.ed.Mode() == vimtext.Insert {
				return nil, pickNavDown
			}
		case "k", "up", "ctrl+p":
			if p.ed.Mode() == vimtext.Normal || p.ed.Mode() == vimtext.Insert {
				return nil, pickNavUp
			}
		case "pgup":
			if p.ed.Mode() == vimtext.Normal || p.ed.Mode() == vimtext.Insert {
				return nil, pickNavPageUp
			}
		case "pgdown":
			if p.ed.Mode() == vimtext.Normal || p.ed.Mode() == vimtext.Insert {
				return nil, pickNavPageDown
			}
		}
		if p.ed.Mode() == vimtext.Normal {
			switch k.String() {
			case "esc", "q":
				return nil, pickNavClose
			case "enter":
				return nil, pickNavSubmit
			}
		}
	}
	if p, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: collapsePaste(p.Content)}
	}
	cmd, ev := p.ed.Update(msg)
	switch ev.Kind {
	case vimtext.Submit:
		return cmd, pickNavSubmit
	case vimtext.Changed:
		return cmd, pickNavChanged
	case vimtext.Leave:
		return cmd, pickNavClose
	default:
		_ = page
		return cmd, pickNavNone
	}
}

func collapsePaste(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

// promptInput is like pickInput but for modal name prompts (no list navigation).
type promptInput struct {
	pickInput
}

func newPromptInput(th ui.Theme, initial, placeholder string) (*promptInput, tea.Cmd) {
	ed := vimtext.New(th, vimtext.Options{
		SingleLine:  true,
		StartInsert: true,
		Placeholder: placeholder,
	})
	ed.SetValue(initial)
	p := &promptInput{pickInput: pickInput{ed: ed, prompt: "", width: 8}}
	return p, ed.Focus()
}

func (p *promptInput) update(msg tea.Msg) (tea.Cmd, pickNav) {
	if k, ok := msg.(tea.KeyPressMsg); ok && p.ed.Mode() == vimtext.Normal {
		switch k.String() {
		case "esc", "q":
			return nil, pickNavClose
		case "enter":
			return nil, pickNavSubmit
		}
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		msg = tea.PasteMsg{Content: collapsePaste(paste.Content)}
	}
	cmd, ev := p.ed.Update(msg)
	switch ev.Kind {
	case vimtext.Submit:
		return cmd, pickNavSubmit
	case vimtext.Changed:
		return cmd, pickNavChanged
	case vimtext.Leave:
		return cmd, pickNavClose
	default:
		return cmd, pickNavNone
	}
}

func (p *promptInput) view(th ui.Theme) string {
	return p.ed.View()
}

// overlayVimMode is implemented by overlays that show INSERT/NORMAL in the footer.
type overlayVimMode interface {
	overlayVimMode() string
}
