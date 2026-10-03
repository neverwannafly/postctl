package response

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

const revealSentinel = "reveal-sentinel-0001"

func revealPane(t *testing.T) *Pane {
	t.Helper()
	id := model.NewID()
	req := &model.Request{ID: id, Method: "GET", URL: model.URL{Raw: "https://example.test/x"}}
	pp := New(tui.PaneEnv{Theme: ui.NewTheme(true, true, false), Actions: &tui.Registry{}})
	p := pp.(*Pane)
	p.SetSize(120, 30)
	p.Update(tui.SelectionMsg{Ref: tui.RequestRef{Request: req}})
	p.Update(tui.SendResultMsg{Result: tui.SendResult{
		RequestID: id,
		Response: &httpx.Response{
			StatusCode: 200, Status: "200 OK", Body: []byte("{}"),
			Headers: []model.KV{{Key: "X-Api-Key", Value: revealSentinel}},
		},
	}})
	_ = p.Focus()
	p.Update(tui.SectionStepMsg{Delta: 1}) // Headers
	return p
}

// TestRevealLastsOneTabVisit checks that u unmasks the headers tab and that
// changing tab, blurring, selecting another request or a new result masks it
// again. Frames are compared, never printed.
func TestRevealLastsOneTabVisit(t *testing.T) {
	reveal := func(p *Pane) {
		p.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	}
	steps := map[string]func(*Pane){
		"tab change": func(p *Pane) {
			p.Update(tui.SectionStepMsg{Delta: 1})
			p.Update(tui.SectionStepMsg{Delta: -1})
		},
		"blur": func(p *Pane) {
			p.Blur()
			_ = p.Focus()
		},
		"selection": func(p *Pane) {
			p.Update(tui.SelectionMsg{Ref: p.ref})
		},
		"new result": func(p *Pane) {
			p.Update(tui.SendResultMsg{Result: tui.SendResult{
				RequestID: p.ref.Request.ID,
				Response: &httpx.Response{StatusCode: 200, Status: "200 OK", Body: []byte("{}"),
					Headers: []model.KV{{Key: "X-Api-Key", Value: revealSentinel}}},
			}})
		},
	}
	for name, away := range steps {
		t.Run(name, func(t *testing.T) {
			p := revealPane(t)
			masked := p.View()
			if strings.Contains(masked, revealSentinel) {
				t.Fatal("credential header is not masked before reveal")
			}
			reveal(p)
			if !strings.Contains(p.View(), revealSentinel) {
				t.Fatal("u did not reveal the header")
			}
			away(p)
			if strings.Contains(p.View(), revealSentinel) {
				t.Fatalf("still revealed after %s", name)
			}
		})
	}
}
