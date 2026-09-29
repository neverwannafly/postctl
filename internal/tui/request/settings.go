package request

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

// settingsView is the Settings tab: follow redirects, max redirects, TLS
// verification and timeout.
type settingsView struct {
	f       form
	timeErr bool
	maxErr  bool
}

func newSettingsView() *settingsView { return &settingsView{} }

func (s *settingsView) reload(p *Pane) { s.f.reset() }
func (s *settingsView) stop()          { s.f.reset() }
func (s *settingsView) editing() bool  { return s.f.edit }
func (s *settingsView) enter(p *Pane)  { s.f.reveal = false }
func (s *settingsView) atTop() bool    { return s.f.cur == 0 && !s.f.edit }

func (s *settingsView) paste(p *Pane, m tea.PasteMsg) bool {
	return s.f.paste(p, func() []formRow { return s.rows(p) }, m)
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// parseTimeout accepts "30s", "2m", "1h30m", "500ms" or a bare number of
// seconds.
func parseTimeout(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n < 0 {
			return 0, false
		}
		return time.Duration(n * float64(time.Second)), true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, false
	}
	return d, true
}

func (s *settingsView) rows(p *Pane) []formRow {
	set := &p.req.Settings
	follow := set.FollowRedirects == nil || *set.FollowRedirects
	verify := set.VerifyTLS == nil || *set.VerifyTLS

	rows := []formRow{{
		label: "Follow redirects", kind: rowChoice, value: onOff(follow),
		cycle: func(int) { v := !follow; set.FollowRedirects = &v },
	}}
	maxV := ""
	if set.MaxRedirects != nil {
		maxV = strconv.Itoa(*set.MaxRedirects)
	}
	maxRow := formRow{
		label: "Max redirects", kind: rowText, value: maxV, placeholder: "10 (default)",
		set: func(text string) bool {
			text = strings.TrimSpace(text)
			if text == "" {
				set.MaxRedirects = nil
				s.maxErr = false
				return true
			}
			n, err := strconv.Atoi(text)
			if err != nil || n < 0 {
				s.maxErr = true
				return false
			}
			s.maxErr = false
			set.MaxRedirects = &n
			return true
		},
	}
	switch {
	case s.maxErr:
		maxRow.note, maxRow.warn = p.th.Glyphs.Warn+" whole number", true
	case !follow:
		maxRow.note = "ignored: not following"
	}
	rows = append(rows, maxRow)
	tls := formRow{
		label: "Verify TLS", kind: rowChoice, value: onOff(verify),
		cycle: func(int) { v := !verify; set.VerifyTLS = &v },
	}
	if !verify {
		tls.note, tls.warn = p.th.Glyphs.Warn+" certificates are not checked", true
	}
	rows = append(rows, tls)
	tv := ""
	if set.Timeout != nil {
		tv = set.Timeout.String()
	}
	to := formRow{
		label: "Timeout", kind: rowText, value: tv, placeholder: "default",
		set: func(text string) bool {
			if strings.TrimSpace(text) == "" {
				set.Timeout = nil
				s.timeErr = false
				return true
			}
			d, ok := parseTimeout(text)
			if !ok {
				s.timeErr = true
				return false
			}
			s.timeErr = false
			set.Timeout = &d
			return true
		},
	}
	if s.timeErr {
		to.note, to.warn = p.th.Glyphs.Warn+" use 30s or 2m", true
	}
	rows = append(rows, to)
	return rows
}

func (s *settingsView) key(p *Pane, m tea.KeyPressMsg) (bool, bool, tea.Cmd) {
	return s.f.key(p, func() []formRow { return s.rows(p) }, m)
}

func (s *settingsView) view(p *Pane, w, h int) string {
	return s.f.view(p, s.rows(p), w, h)
}

func (s *settingsView) hints(p *Pane) []ui.Hint {
	if s.f.edit {
		return []ui.Hint{{Key: "Esc", Label: "Done", Priority: 1}, {Key: "Tab", Label: "Next field", Priority: 3}}
	}
	return []ui.Hint{
		{Key: "Space", Label: "Toggle", Priority: 2},
		{Key: "Enter", Label: "Edit", Priority: 2},
		{Key: "↑↓", Label: "Field", Priority: 4},
	}
}
