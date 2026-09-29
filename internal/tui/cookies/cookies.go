// Package cookies is the cookie manager screen: cookies grouped by domain,
// one cookie in detail, delete, and clear a domain or everything behind a
// confirmation. Values are masked until revealed, since session cookies are
// credentials.
package cookies

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// Jar is the part of [httpx.CookieJar] the manager uses.
type Jar interface {
	List(domain string) []httpx.StoredCookie
	Delete(domain, path, name string) error
	Clear(domain string) error
}

// Factory returns a [tui.ScreenFactory] over jar. A nil jar shows an empty
// manager.
func Factory(jar Jar) tui.ScreenFactory {
	return func(env tui.ScreenEnv) tui.Screen { return New(env, jar) }
}

type mode int

const (
	modeList mode = iota
	modeConfirm
)

// changedMsg reports a finished jar operation with the fresh list.
type changedMsg struct {
	list []httpx.StoredCookie
	text string
	err  error
}

// Model is the cookie manager.
type Model struct {
	th   ui.Theme
	jar  Jar
	w, h int

	list   []httpx.StoredCookie
	cur    int
	top    int
	reveal bool

	mode   mode
	conf   ui.Confirm
	confFn func() tea.Cmd

	notice string
	failed bool
	now    func() time.Time
}

// New builds the manager.
func New(env tui.ScreenEnv, jar Jar) *Model {
	m := &Model{th: env.Theme, jar: jar, now: time.Now}
	if jar != nil {
		m.list = jar.List("")
	}
	return m
}

// SetSize implements tui.Screen.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// KeyHints implements tui.Screen.
func (m *Model) KeyHints() []ui.Hint {
	if m.mode == modeConfirm {
		return []ui.Hint{{Key: "Enter", Label: "Choose", Priority: 1}, {Key: "←→", Label: "Switch", Priority: 2}, {Key: "Esc", Label: "Cancel", Priority: 1}}
	}
	return []ui.Hint{
		{Key: "↑↓", Label: "Choose", Priority: 2},
		{Key: "r", Label: "Reveal", Priority: 2},
		{Key: "d", Label: "Delete", Priority: 1},
		{Key: "x", Label: "Clear domain", Priority: 3},
		{Key: "X", Label: "Clear all", Priority: 4},
		{Key: "Esc", Label: "Close", Priority: 1},
	}
}

func (m *Model) cookie() *httpx.StoredCookie {
	if m.cur < 0 || m.cur >= len(m.list) {
		return nil
	}
	return &m.list[m.cur]
}

// Update implements tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch v := msg.(type) {
	case tui.ThemeChangedMsg:
		m.th = v.Theme
	case changedMsg:
		m.list = v.list
		m.cur = min(m.cur, max(0, len(m.list)-1))
		m.reveal = false
		m.notice, m.failed = v.text, v.err != nil
		if v.err != nil {
			m.notice = "Not saved: " + v.err.Error()
		}
	case tea.KeyPressMsg:
		if m.mode == modeConfirm {
			return m, m.confirmKey(v)
		}
		return m.listKey(v)
	}
	return m, nil
}

func (m *Model) listKey(k tea.KeyPressMsg) (tui.Screen, tea.Cmd) {
	m.notice = ""
	rows := max(1, m.listRows())
	switch k.String() {
	case "esc", "q":
		return nil, nil
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "pgup":
		m.move(-rows)
	case "pgdown":
		m.move(rows)
	case "home":
		m.move(-len(m.list))
	case "end":
		m.move(len(m.list))
	case "r":
		m.reveal = !m.reveal
	case "d", "delete":
		if c := m.cookie(); c != nil {
			d, p, n := c.Domain, c.Path, c.Name
			return m, m.run("Deleted "+n, func() error { return m.jar.Delete(d, p, n) })
		}
	case "x":
		if c := m.cookie(); c != nil {
			dom := c.Domain
			count := 0
			for _, o := range m.list {
				if o.Domain == dom {
					count++
				}
			}
			m.ask("Clear cookies for "+dom, fmt.Sprintf("Removes %s for %s. Sessions there will need to sign in again.", plural(count), dom), "Clear",
				func() tea.Cmd { return m.run("Cleared "+dom, func() error { return m.jar.Clear(dom) }) })
		}
	case "X":
		if len(m.list) > 0 {
			m.ask("Clear all cookies", fmt.Sprintf("Removes %s from every domain. Sessions will need to sign in again.", plural(len(m.list))), "Clear all",
				func() tea.Cmd { return m.run("Cleared all cookies", func() error { return m.jar.Clear("") }) })
		}
	}
	return m, nil
}

func plural(n int) string {
	if n == 1 {
		return "1 cookie"
	}
	return strconv.Itoa(n) + " cookies"
}

func (m *Model) move(d int) {
	m.cur = min(max(m.cur+d, 0), max(0, len(m.list)-1))
	m.reveal = false
}

func (m *Model) ask(title, body, action string, fn func() tea.Cmd) {
	m.conf = ui.NewConfirm(title, body, action, true)
	m.confFn = fn
	m.mode = modeConfirm
}

func (m *Model) confirmKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "left", "shift+tab", "up":
		m.conf.Move(-1)
	case "right", "tab", "down":
		m.conf.Move(1)
	case "esc", "n":
		m.mode = modeList
	case "enter", "y":
		m.mode = modeList
		if k.String() == "y" || m.conf.Accepted() {
			return m.confFn()
		}
	}
	return nil
}

// run performs a jar change off the update goroutine and reports the list.
func (m *Model) run(text string, op func() error) tea.Cmd {
	if m.jar == nil {
		return nil
	}
	jar := m.jar
	return func() tea.Msg {
		err := op()
		return changedMsg{list: jar.List(""), text: text, err: err}
	}
}

// ---- view -----------------------------------------------------------------------------

// wide reports whether the detail fits beside the list.
func (m *Model) wide() bool { return m.w >= 96 }

const detailH = 9

func (m *Model) listRows() int {
	body := m.h - 3
	if !m.wide() {
		body -= detailH + 1
	}
	return max(1, body-1)
}

// View implements tui.Screen.
func (m *Model) View() string {
	th := m.th
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	title := th.Bold().Render("Cookies")
	if len(m.list) > 0 {
		title += "  " + th.Render(ui.TextQuiet, plural(len(m.list))+" in "+domainCount(m.list))
	}
	if m.notice != "" {
		tok := ui.TextSecondary
		if m.failed {
			tok = ui.StatusDanger
		}
		r := th.Render(tok, th.Truncate(m.notice, max(4, m.w/2)))
		title = th.Fit(title, max(1, m.w-ui.Width(r)-1)) + " " + r
	}
	ch := max(1, m.h-2)
	var body string
	switch {
	case m.mode == modeConfirm:
		body = th.Block(th.ConfirmView(m.conf, min(m.w, 66)), m.w, ch)
	case len(m.list) == 0:
		body = th.EmptyState("No cookies",
			"Cookies set by responses are kept here per workspace and sent back to the same domain.", "Send a request that sets one.", m.w)
	case m.wide():
		lw := 40
		list := m.listView(lw, ch)
		det := m.detailView(m.w-lw-3, ch)
		body = joinCols(th, list, det, lw, ch)
	default:
		list := m.listView(m.w, ch-detailH-1)
		body = th.Block(list, m.w, ch-detailH-1) + "\n" + th.Render(ui.TextQuiet, strings.Repeat(th.Glyphs.Rule, m.w)) + "\n" + m.detailView(m.w, detailH)
	}
	return th.Block(title+"\n\n"+th.Block(body, m.w, ch-1), m.w, m.h)
}

func domainCount(list []httpx.StoredCookie) string {
	seen := map[string]bool{}
	for _, c := range list {
		seen[c.Domain] = true
	}
	if len(seen) == 1 {
		return "1 domain"
	}
	return strconv.Itoa(len(seen)) + " domains"
}

func joinCols(th ui.Theme, left, right string, lw, h int) string {
	l := strings.Split(th.Block(left, lw, h), "\n")
	r := strings.Split(right, "\n")
	gap := th.Gap()
	out := make([]string, 0, h)
	for i := range h {
		rr := ""
		if i < len(r) {
			rr = r[i]
		}
		out = append(out, l[i]+gap+rr)
	}
	return strings.Join(out, "\n")
}

// listView draws domain headings and cookies, keeping the cursor visible.
func (m *Model) listView(w, h int) string {
	th := m.th
	type line struct {
		text string
		idx  int // cookie index, -1 for a heading
	}
	var lines []line
	last := "\x00"
	curLine := 0
	for i, c := range m.list {
		if c.Domain != last {
			last = c.Domain
			lines = append(lines, line{th.Render(ui.TextSecondary, th.Bold().Render(th.Truncate(displayDomain(c), w-2))), -1})
		}
		if i == m.cur {
			curLine = len(lines)
		}
		marker := "  "
		tok := ui.TextPrimary
		if i == m.cur {
			marker = th.Render(ui.AccentPrimary, th.Glyphs.Select) + " "
			tok = ui.AccentPrimary
		}
		name := th.Fit(c.Name, max(4, w-2-2))
		lines = append(lines, line{marker + " " + th.Render(tok, name), i})
	}
	rows := max(1, h)
	top := m.top
	if curLine < top {
		top = curLine
	}
	if curLine >= top+rows {
		top = curLine - rows + 1
	}
	// Show the heading with its first cookie.
	if top > 0 && top < len(lines) && lines[top].idx >= 0 && lines[top-1].idx == -1 && curLine-top+1 < rows {
		top--
	}
	m.top = max(0, min(top, max(0, len(lines)-rows)))
	end := min(len(lines), m.top+rows)
	out := make([]string, 0, rows)
	for _, l := range lines[m.top:end] {
		out = append(out, th.Truncate(l.text, w))
	}
	return strings.Join(out, "\n")
}

func displayDomain(c httpx.StoredCookie) string {
	d := strings.TrimPrefix(c.Domain, ".")
	if !c.HostOnly {
		return "." + d
	}
	return d
}

func (m *Model) detailView(w, h int) string {
	th := m.th
	c := m.cookie()
	if c == nil {
		return ""
	}
	const lw = 9
	value := th.Glyphs.Mask
	tok := ui.TextQuiet
	if m.reveal {
		value = c.Value
		tok = ui.TextPrimary
		if value == "" {
			value, tok = "(empty)", ui.TextQuiet
		}
	}
	var flags []string
	if c.Secure {
		flags = append(flags, "Secure")
	}
	if c.HTTPOnly {
		flags = append(flags, "HttpOnly")
	}
	if c.HostOnly {
		flags = append(flags, "HostOnly")
	}
	if s := sameSite(c.SameSite); s != "" {
		flags = append(flags, "SameSite="+s)
	}
	if len(flags) == 0 {
		flags = []string{"none"}
	}
	row := func(label, val string, tok ui.Token) string {
		return th.Label(lw, label, th.Render(tok, th.Truncate(val, max(1, w-lw-2))))
	}
	if !m.reveal {
		value += "  (r to show)"
	}
	lines := []string{
		th.Section(c.Name, "", false),
		row("Domain", displayDomain(*c), ui.TextPrimary),
		row("Path", c.Path, ui.TextPrimary),
		row("Value", value, tok),
		row("Expires", m.expires(*c), ui.TextPrimary),
		row("Flags", strings.Join(flags, ", "), ui.TextPrimary),
		row("Size", strconv.Itoa(len(c.Name)+len(c.Value))+" bytes", ui.TextQuiet),
	}
	return th.Block(strings.Join(lines, "\n"), w, h)
}

func (m *Model) expires(c httpx.StoredCookie) string {
	if c.Expires.IsZero() {
		return "when postctl closes (session)"
	}
	d := c.Expires.Sub(m.now())
	switch {
	case d <= 0:
		return "expired"
	case d < time.Hour:
		return "in " + strconv.Itoa(int(d.Minutes())+1) + " min"
	case d < 48*time.Hour:
		return "in " + strconv.Itoa(int(d.Hours())) + " h"
	default:
		return c.Expires.Local().Format("2006-01-02") + " (in " + strconv.Itoa(int(d.Hours()/24)) + " days)"
	}
}

func sameSite(v int) string {
	switch http.SameSite(v) {
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteNoneMode:
		return "None"
	}
	return ""
}
