package tui

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// cleanURLPath extracts the URL path from a raw URL string, stripping
// scheme, host, userinfo, query parameters and fragments.
func cleanURLPath(raw string) string {
	if raw == "" {
		return ""
	}
	base, _, _ := strings.Cut(raw, "?")
	base, _, _ = strings.Cut(base, "#")
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}

	// Scheme (e.g. http://, https://, ws://, wss://)
	if i := strings.Index(base, "://"); i >= 0 {
		rest := base[i+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			return rest[slash:]
		}
		return "/"
	}

	// Template host (e.g. {{baseUrl}}/path or {{host}})
	if strings.HasPrefix(base, "{{") {
		if end := strings.Index(base, "}}"); end >= 0 {
			rest := base[end+2:]
			if slash := strings.IndexByte(rest, '/'); slash >= 0 {
				return rest[slash:]
			}
			return "/"
		}
	}

	// Leading slash is already a path
	if strings.HasPrefix(base, "/") {
		return base
	}

	// Slash present: if the segment before slash looks like a host (domain, port, localhost), strip it.
	if slash := strings.IndexByte(base, '/'); slash >= 0 {
		head := base[:slash]
		if strings.Contains(head, ".") || strings.Contains(head, ":") || head == "localhost" {
			return base[slash:]
		}
		return "/" + base
	}

	// No slash: if it looks like a host (domain, port, localhost), path is /
	if strings.Contains(base, ".") || strings.Contains(base, ":") || base == "localhost" {
		return "/"
	}

	return "/" + base
}

// recordRecent records an opened request ID in the MRU list (depth 50, newest first, deduplicated).
func (a *app) recordRecent(id model.ID) {
	if id == "" {
		return
	}
	out := make([]model.ID, 0, min(len(a.recent)+1, 50))
	out = append(out, id)
	for _, r := range a.recent {
		if r != id && len(out) < 50 {
			out = append(out, r)
		}
	}
	a.recent = out
}

// openSwitcher opens the request quick switcher overlay.
func (a *app) openSwitcher() tea.Cmd {
	s, cmd := newSwitcherOverlay(a)
	a.ov = s
	return cmd
}

// switcherItem is one request candidate in the switcher.
type switcherItem struct {
	id      model.ID
	name    string
	method  string
	folder  string
	urlPath string
}

// switcherMatch is one scored result in the switcher.
type switcherMatch struct {
	index     int
	score     int
	posName   []int
	posMethod []int
	posFolder []int
	posPath   []int
}

// switcherOverlay is a dedicated overlay for finding and opening requests.
type switcherOverlay struct {
	input    textinput.Model
	all      []switcherItem
	res      []switcherMatch
	cursor   int
	top      int
	lastPage int
}

func newSwitcherOverlay(a *app) (*switcherOverlay, tea.Cmd) {
	in := textinput.New()
	in.Prompt = "› "
	if a.ascii {
		in.Prompt = "> "
	}
	in.Placeholder = "search requests"
	in.CharLimit = 80
	styleInput(&in, a.theme)
	cmd := in.Focus()

	var treeItems []switcherItem
	itemByID := make(map[model.ID]switcherItem)
	arrow := " " + a.theme.Glyphs.Arrow + " "

	if a.ws != nil {
		for _, c := range a.ws.Collections {
			_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
				r, ok := it.(*model.Request)
				if !ok {
					return nil
				}
				if a.tree != nil {
					if p := a.tree.problemByID(r.ID); p != nil {
						return nil
					}
				}
				var folder string
				if a.tree != nil && a.tree.nodes != nil {
					if n := a.tree.nodes[r.ID]; n != nil && len(n.path) > 1 {
						folder = strings.Join(n.path[:len(n.path)-1], arrow)
					}
				}
				if folder == "" && c != nil {
					folder = c.Name
				}
				item := switcherItem{
					id:      r.ID,
					name:    r.Name,
					method:  methodBadge(r),
					folder:  folder,
					urlPath: cleanURLPath(r.URL.Raw),
				}
				treeItems = append(treeItems, item)
				itemByID[r.ID] = item
				return nil
			})
		}
	}

	var all []switcherItem
	seen := make(map[model.ID]bool)
	// MRU order first
	for _, id := range a.recent {
		if item, ok := itemByID[id]; ok && !seen[id] {
			all = append(all, item)
			seen[id] = true
		}
	}
	// Rest in tree order
	for _, item := range treeItems {
		if !seen[item.id] {
			all = append(all, item)
			seen[item.id] = true
		}
	}

	s := &switcherOverlay{
		input: in,
		all:   all,
	}
	s.refilter()
	g := a.geo()
	s.setSize(g.width, g.bodyH)
	return s, cmd
}

func (s *switcherOverlay) refilter() {
	q := strings.TrimSpace(s.input.Value())
	if q == "" {
		s.res = make([]switcherMatch, len(s.all))
		for i := range s.all {
			s.res[i] = switcherMatch{index: i}
		}
		s.cursor, s.top = 0, 0
		return
	}

	var matches []switcherMatch
	for i, item := range s.all {
		m, ok := matchSwitcherItem(q, item)
		if ok {
			m.index = i
			matches = append(matches, m)
		}
	}
	sort.SliceStable(matches, func(a, b int) bool {
		return matches[a].score > matches[b].score
	})
	s.res = matches
	s.cursor, s.top = 0, 0
}

func matchSwitcherItem(q string, item switcherItem) (switcherMatch, bool) {
	sName, posName, okName := fuzzyMatch(q, item.name)
	sMethod, posMethod, okMethod := fuzzyMatch(q, item.method)
	sFolder, posFolder, okFolder := fuzzyMatch(q, item.folder)
	sPath, posPath, okPath := fuzzyMatch(q, item.urlPath)

	comb := item.method + " " + item.name + " " + item.folder + " " + item.urlPath
	sComb, _, okComb := fuzzyMatch(q, comb)

	if !okName && !okMethod && !okFolder && !okPath && !okComb {
		return switcherMatch{}, false
	}

	bestScore := -1 << 30
	if okName && sName > bestScore {
		bestScore = sName
	}
	if okMethod && sMethod > bestScore {
		bestScore = sMethod
	}
	if okFolder && sFolder > bestScore {
		bestScore = sFolder
	}
	if okPath && sPath > bestScore {
		bestScore = sPath
	}
	if okComb && sComb > bestScore {
		bestScore = sComb
	}

	// For multi-word queries, also match words to provide per-field highlights
	if !okName || !okMethod || !okFolder || !okPath {
		words := strings.Fields(q)
		if len(words) > 1 {
			for _, w := range words {
				if len(posName) == 0 {
					if _, p, ok := fuzzyMatch(w, item.name); ok {
						posName = p
					}
				}
				if len(posMethod) == 0 {
					if _, p, ok := fuzzyMatch(w, item.method); ok {
						posMethod = p
					}
				}
				if len(posFolder) == 0 {
					if _, p, ok := fuzzyMatch(w, item.folder); ok {
						posFolder = p
					}
				}
				if len(posPath) == 0 {
					if _, p, ok := fuzzyMatch(w, item.urlPath); ok {
						posPath = p
					}
				}
			}
		}
	}

	return switcherMatch{
		score:     bestScore,
		posName:   posName,
		posMethod: posMethod,
		posFolder: posFolder,
		posPath:   posPath,
	}, true
}

func (s *switcherOverlay) update(a *app, msg tea.Msg) (overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		page := max(1, s.lastPage-1)
		switch k.String() {
		case "esc":
			return nil, nil
		case "enter":
			if s.cursor >= 0 && s.cursor < len(s.res) {
				it := s.all[s.res[s.cursor].index]
				return nil, s.openItem(a, it)
			}
			return s, nil
		case "up", "ctrl+p":
			s.cursor = max(0, s.cursor-1)
			s.scroll()
			return s, nil
		case "down", "ctrl+n":
			if len(s.res) > 0 {
				s.cursor = min(len(s.res)-1, s.cursor+1)
			}
			s.scroll()
			return s, nil
		case "pgup":
			s.cursor = max(0, s.cursor-page)
			s.scroll()
			return s, nil
		case "pgdown":
			if len(s.res) > 0 {
				s.cursor = min(len(s.res)-1, s.cursor+page)
			}
			s.scroll()
			return s, nil
		}
	}

	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if s.input.Value() != before {
		s.refilter()
	}
	return s, cmd
}

func (s *switcherOverlay) openItem(a *app, it switcherItem) tea.Cmd {
	if a.tree == nil || !a.tree.reveal(it.id) {
		return a.toastWarn("That request no longer exists.")
	}
	a.layoutPanes()
	return tea.Batch(a.syncSelection(true), a.setFocus(SlotRequest))
}

func (s *switcherOverlay) scroll() {
	n := max(1, s.lastPage)
	if s.cursor < s.top {
		s.top = s.cursor
	}
	if s.cursor >= s.top+n {
		s.top = s.cursor - n + 1
	}
}

func (s *switcherOverlay) setSize(w, h int) {
	s.input.SetWidth(max(8, w-4))
	s.lastPage = max(1, h-4)
	s.scroll()
}

func (s *switcherOverlay) view(a *app, w, h int) string {
	t := a.theme
	head := t.Bold().Render("Find request")
	countText := strconv.Itoa(len(s.res)) + " of " + strconv.Itoa(len(s.all))
	cnt := t.Render(ui.TextQuiet, countText)
	esc := t.Render(ui.TextSecondary, "Esc Close")

	gap := max(1, w-ui.Width(head)-2-ui.Width(cnt)-ui.Width(esc))
	topLine := head + "  " + cnt + strings.Repeat(" ", gap) + esc

	lines := []string{topLine, s.input.View(), t.Rule(w)}
	if len(s.res) == 0 {
		msg := "No requests yet. Space a creates one."
		if strings.TrimSpace(s.input.Value()) != "" {
			msg = "No matches for \"" + strings.TrimSpace(s.input.Value()) + "\"."
		}
		lines = append(lines, t.Render(ui.TextSecondary, msg))
		return strings.Join(lines, "\n")
	}

	end := min(len(s.res), s.top+s.lastPage)
	for i := s.top; i < end; i++ {
		r := s.res[i]
		lines = append(lines, s.row(t, a.ascii, s.all[r.index], r, i == s.cursor, w))
	}
	return strings.Join(lines, "\n")
}

func (s *switcherOverlay) row(t ui.Theme, ascii bool, it switcherItem, m switcherMatch, sel bool, w int) string {
	on := func(tok ui.Token) lipgloss.Style { return t.StyleOn(tok, sel) }
	var segs seg

	if sel {
		segs.add(on(ui.AccentPrimary), t.Glyphs.Select+" ")
	} else {
		segs.add(on(ui.TextPrimary), "  ")
	}

	mLabel := ui.MethodLabel(it.method, 5)
	mPadded := t.Fit(mLabel, 5)
	if len(m.posMethod) > 0 {
		renderHighlighted(&segs, mPadded, m.posMethod, ui.MethodToken(it.method), on)
	} else {
		segs.add(on(ui.MethodToken(it.method)), mPadded)
	}
	segs.add(on(ui.TextPrimary), " ")

	room := w - segs.w
	if room <= 0 {
		return segs.sb.String()
	}

	name := it.name
	posName := m.posName
	folder := it.folder
	posFolder := m.posFolder
	urlPath := it.urlPath
	posPath := m.posPath

	nameW := ui.Width(name)
	folderW := 0
	if folder != "" {
		folderW = 2 + ui.Width(folder)
	}
	urlW := 0
	if urlPath != "" {
		urlW = 2 + ui.Width(urlPath)
	}

	total := nameW + folderW + urlW
	if total > room {
		excess := total - room
		if urlW > 14 && excess > 0 {
			trim := min(urlW-14, excess)
			urlPath, posPath = truncateString(urlPath, ui.Width(urlPath)-trim, posPath, t.Glyphs.Ellipsis)
			urlW = 2 + ui.Width(urlPath)
			excess = nameW + folderW + urlW - room
		}
		if folderW > 16 && excess > 0 {
			trim := min(folderW-16, excess)
			folder, posFolder = truncateString(folder, ui.Width(folder)-trim, posFolder, t.Glyphs.Ellipsis)
			folderW = 2 + ui.Width(folder)
			excess = nameW + folderW + urlW - room
		}
		if excess > 0 {
			name, posName = truncateString(name, max(4, nameW-excess), posName, t.Glyphs.Ellipsis)
		}
	}

	renderHighlighted(&segs, name, posName, ui.TextPrimary, on)

	if folder != "" && segs.w < w {
		segs.add(on(ui.TextPrimary), "  ")
		renderHighlighted(&segs, folder, posFolder, ui.TextQuiet, on)
	}

	if urlPath != "" && segs.w < w {
		segs.add(on(ui.TextPrimary), "  ")
		renderHighlighted(&segs, urlPath, posPath, ui.TextSecondary, on)
	}

	if pad := w - segs.w; pad > 0 {
		segs.add(on(ui.TextPrimary), strings.Repeat(" ", pad))
	}
	return segs.sb.String()
}

func renderHighlighted(s *seg, text string, pos []int, defTok ui.Token, on func(ui.Token) lipgloss.Style) {
	if text == "" {
		return
	}
	if len(pos) == 0 {
		s.add(on(defTok), text)
		return
	}
	runes := []rune(text)
	m := make(map[int]bool, len(pos))
	for _, p := range pos {
		if p >= 0 && p < len(runes) {
			m[p] = true
		}
	}
	start := 0
	for i := 1; i <= len(runes); i++ {
		if i == len(runes) || m[i] != m[start] {
			st := on(defTok)
			if m[start] {
				st = on(ui.AccentPrimary).Underline(true)
			}
			s.add(st, string(runes[start:i]))
			start = i
		}
	}
}

func truncateString(s string, maxW int, pos []int, ellipsis string) (string, []int) {
	if ui.Width(s) <= maxW {
		return s, pos
	}
	runes := []rune(s)
	ellRunes := []rune(ellipsis)
	if maxW <= len(ellRunes) {
		if maxW <= 0 {
			return "", nil
		}
		return string(ellRunes[:maxW]), nil
	}
	keepLen := maxW - len(ellRunes)
	if keepLen > len(runes) {
		keepLen = len(runes)
	}
	kept := runes[:keepLen]
	var newPos []int
	for _, p := range pos {
		if p < keepLen {
			newPos = append(newPos, p)
		}
	}
	return string(append(kept, ellRunes...)), newPos
}

func (s *switcherOverlay) hints(a *app) []ui.Hint {
	arrows := "↑↓"
	if a.ascii {
		arrows = "Up/Down"
	}
	return []ui.Hint{
		hint("Enter", "Open", 0),
		hint(arrows, "Move", 1),
		hint("Esc", "Close", 0),
	}
}
