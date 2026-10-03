package tui

import (
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// cleanURLPath extracts the URL path from a raw URL string, stripping
// scheme, authority/host (including userinfo and port), query parameters and fragments.
// It is template-aware: '?' and '#' inside {{...}} are not treated as delimiters.
func cleanURLPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// 1. Template-aware cutoff for query ('?') and fragment ('#').
	cutoff := len(raw)
	inTemplate := false
	for i := 0; i < len(raw); i++ {
		if !inTemplate && i+1 < len(raw) && raw[i] == '{' && raw[i+1] == '{' {
			inTemplate = true
			i++
			continue
		}
		if inTemplate && i+1 < len(raw) && raw[i] == '}' && raw[i+1] == '}' {
			inTemplate = false
			i++
			continue
		}
		if !inTemplate && (raw[i] == '?' || raw[i] == '#') {
			cutoff = i
			break
		}
	}
	base := strings.TrimSpace(raw[:cutoff])
	if base == "" {
		return ""
	}

	// 2. Scheme (e.g. http://, https://, ws://, wss://).
	if i := strings.Index(base, "://"); i >= 0 {
		rest := base[i+3:]
		slash := findFirstSlash(rest)
		if slash >= 0 {
			return rest[slash:]
		}
		return "/"
	}

	// 3. Scheme-less:
	// If it starts with '/', it is already a path.
	if strings.HasPrefix(base, "/") {
		return base
	}

	// If the URL has no scheme and does not start with '/',
	// ALWAYS treat the first segment as the authority and drop it.
	slash := findFirstSlash(base)
	if slash >= 0 {
		return base[slash:]
	}
	return "/"
}

// findFirstSlash returns the index of the first '/' outside of {{...}} templates.
func findFirstSlash(s string) int {
	inTemplate := false
	for i := 0; i < len(s); i++ {
		if !inTemplate && i+1 < len(s) && rawIsTemplateStart(s, i) {
			inTemplate = true
			i++
			continue
		}
		if inTemplate && i+1 < len(s) && s[i] == '}' && s[i+1] == '}' {
			inTemplate = false
			i++
			continue
		}
		if !inTemplate && s[i] == '/' {
			return i
		}
	}
	return -1
}

func rawIsTemplateStart(s string, i int) bool {
	return s[i] == '{' && s[i+1] == '{'
}

// sanitizeText strips ANSI escape sequences and C0/C1 control characters.
func sanitizeText(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// recordRecent records an opened request ID in the MRU list (depth 50, newest first, deduplicated),
// pruning any IDs that are no longer in the workspace.
func (a *app) recordRecent(id model.ID) {
	if id == "" {
		return
	}
	if a.ws != nil {
		if _, req := findRequest(a.ws, id); req == nil {
			return
		}
	}
	out := make([]model.ID, 0, min(len(a.recent)+1, 50))
	out = append(out, id)
	for _, r := range a.recent {
		if r == id || len(out) >= 50 {
			continue
		}
		if a.ws != nil {
			if _, req := findRequest(a.ws, r); req == nil {
				continue
			}
		}
		out = append(out, r)
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
	id          model.ID
	name        string
	method      string
	folder      string
	urlPath     string
	searchLabel string
	nameLen     int
	folderStart int
	folderLen   int
	methodStart int
	methodLen   int
	pathStart   int
	pathLen     int
}

func newSwitcherItem(id model.ID, name, method, folder, urlPath string) switcherItem {
	name = sanitizeText(name)
	if strings.TrimSpace(name) == "" {
		name = "(unnamed request)"
	}
	method = sanitizeText(method)
	folder = sanitizeText(folder)
	urlPath = sanitizeText(urlPath)

	nameR := []rune(name)
	folderR := []rune(folder)
	methodR := []rune(method)
	pathR := []rune(urlPath)

	it := switcherItem{
		id:          id,
		name:        name,
		method:      method,
		folder:      folder,
		urlPath:     urlPath,
		nameLen:     len(nameR),
		folderStart: len(nameR) + 1,
		folderLen:   len(folderR),
		methodStart: len(nameR) + 1 + len(folderR) + 1,
		methodLen:   len(methodR),
		pathStart:   len(nameR) + 1 + len(folderR) + 1 + len(methodR) + 1,
		pathLen:     len(pathR),
		searchLabel: name + " " + folder + " " + method + " " + urlPath,
	}
	return it
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
				item := newSwitcherItem(r.ID, r.Name, methodBadge(r), folder, cleanURLPath(r.URL.Raw))
				treeItems = append(treeItems, item)
				itemByID[r.ID] = item
				return nil
			})
		}
	}

	// Prune IDs no longer in workspace from MRU
	var validRecent []model.ID
	for _, id := range a.recent {
		if _, ok := itemByID[id]; ok {
			validRecent = append(validRecent, id)
		}
	}
	a.recent = validRecent

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

	rankedList := rankBy(q, len(s.all), func(i int) string {
		return s.all[i].searchLabel
	}, 0)

	s.res = make([]switcherMatch, len(rankedList))
	for idx, r := range rankedList {
		it := s.all[r.index]
		var posName, posFolder, posMethod, posPath []int
		for _, p := range r.pos {
			if p < it.nameLen {
				posName = append(posName, p)
			} else if p >= it.folderStart && p < it.folderStart+it.folderLen {
				posFolder = append(posFolder, p-it.folderStart)
			} else if p >= it.methodStart && p < it.methodStart+it.methodLen {
				posMethod = append(posMethod, p-it.methodStart)
			} else if p >= it.pathStart && p < it.pathStart+it.pathLen {
				posPath = append(posPath, p-it.pathStart)
			}
		}
		s.res[idx] = switcherMatch{
			index:     r.index,
			score:     r.score,
			posName:   posName,
			posFolder: posFolder,
			posMethod: posMethod,
			posPath:   posPath,
		}
	}
	s.cursor, s.top = 0, 0
}

// ownsKey reports that Ctrl-P moves the cursor up in the switcher (Ctrl-N moves
// it down), as in most fuzzy finders.
func (s *switcherOverlay) ownsKey(k string) bool { return k == "ctrl+p" }

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
	return a.syncSelection(true)
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
		msg := "No requests yet. Press Esc, then Space a to create one."
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
