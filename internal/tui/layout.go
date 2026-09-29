package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/ui"
)

// Chrome heights in rows: header (line + rule) and footer (rule + toast +
// hints).
const (
	headerRows = 2
	footerRows = 3
	gapWidth   = 3 // " │ "
)

// geometry is the screen split for the current size.
type geometry struct {
	layout    ui.Layout
	inset     int
	width     int // content width, inside the inset
	bodyH     int
	sidebarW  int
	requestW  int
	responseW int // two-pane: the single right pane's width lives in requestW
}

func (a *app) geo() geometry {
	g := geometry{layout: a.layoutKind()}
	if !a.accessible && a.width >= 60 {
		g.inset = 1
	}
	g.width = max(1, a.width-2*g.inset)
	g.bodyH = max(3, a.height-headerRows-footerRows)
	switch g.layout {
	case ui.LayoutThreePane:
		g.sidebarW = min(max(30, g.width*22/100), 44)
		rest := g.width - g.sidebarW - 2*gapWidth
		g.requestW = rest * 55 / 100
		g.responseW = rest - g.requestW
	case ui.LayoutTwoPane:
		g.sidebarW = min(max(26, g.width/4), 38)
		g.requestW = g.width - g.sidebarW - gapWidth
		g.responseW = g.requestW
	default:
		g.sidebarW, g.requestW, g.responseW = g.width, g.width, g.width
	}
	return g
}

// layoutPanes pushes the current geometry into the tree and both panes. It is
// called on every resize and whenever the workspace state changes.
func (a *app) layoutPanes() {
	g := a.geo()
	content := g.bodyH - 1 // one row for the pane title
	a.tree.setSize(g.sidebarW, content)
	a.request.SetSize(g.requestW, content)
	a.response.SetSize(g.responseW, content)
}

// View implements tea.Model. It is a pure function of the model.
func (a *app) View() tea.View {
	g := a.geo()
	t := a.theme
	var lines []string
	lines = append(lines, strings.Split(t.Header(g.width, ui.HeaderInfo{Workspace: a.workspaceName(), Env: a.envInfo()}), "\n")...)

	body := a.body(g)
	if a.accessible {
		lines = append(lines, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
	} else {
		lines = append(lines, strings.Split(t.Block(body, g.width, g.bodyH), "\n")...)
	}

	lines = append(lines, t.Rule(g.width))
	if a.toast != nil {
		lines = append(lines, t.Toast(a.toast.kind, a.toast.text, g.width))
	} else if a.saver.busy() && a.phase == phaseReady {
		lines = append(lines, t.Status(ui.StatusRunning, "Saving"))
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, t.KeyHints(g.width, a.hints()))

	if g.inset > 0 {
		pad := strings.Repeat(" ", g.inset)
		for i := range lines {
			lines[i] = pad + lines[i]
		}
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = !a.accessible
	v.WindowTitle = a.windowTitle()
	return v
}

func (a *app) windowTitle() string {
	title := "postctl"
	if a.ws != nil && a.ws.Name != "" {
		title += " " + a.theme.Glyphs.Dot + " " + a.ws.Name
	}
	if e := a.activeEnvironment(); e != nil {
		title += " " + a.theme.Glyphs.Dot + " " + e.Name
	}
	return title
}

// body renders the area between header and footer.
func (a *app) body(g geometry) string {
	t := a.theme
	if a.width < minWidth || a.height < minHeight {
		return t.ErrorState("The window is too small.", "Nothing has changed.", "Make it at least 80 columns by 24 rows.", max(10, g.width))
	}
	switch a.phase {
	case phaseLoading:
		if a.showLoading {
			return t.Busy(a.spinnerFrame(), "Opening workspace "+a.displayRoot())
		}
		return ""
	case phaseFatal:
		return a.fatalView(g)
	}
	if a.ov != nil {
		if d, ok := a.ov.(dialog); ok && d.isDialog() {
			return overlayAt(t.Block(a.mainBody(g), g.width, g.bodyH), a.ov.view(a, g.width, g.bodyH), g)
		}
		return a.ov.view(a, g.width, g.bodyH)
	}
	return a.mainBody(g)
}

// dialog marks overlays drawn as a bordered box over the main screen rather
// than as a full-body screen.
type dialog interface{ isDialog() bool }

func (*confirmOverlay) isDialog() bool { return true }
func (*promptOverlay) isDialog() bool  { return true }

// overlayAt draws box centered horizontally and in the upper third of base.
func overlayAt(base, box string, g geometry) string {
	baseLines := strings.Split(base, "\n")
	boxLines := strings.Split(strings.Trim(box, "\n"), "\n")
	bw := 0
	for _, l := range boxLines {
		bw = max(bw, ui.Width(l))
	}
	x := max(0, (g.width-bw)/2)
	y := max(0, (len(baseLines)-len(boxLines))/3)
	for i, bl := range boxLines {
		row := y + i
		if row >= len(baseLines) {
			break
		}
		line := baseLines[row]
		left := ansi.Truncate(line, x, "")
		if pad := x - ui.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(line, x+ui.Width(bl), "")
		baseLines[row] = left + bl + right
	}
	return strings.Join(baseLines, "\n")
}

func (a *app) spinnerFrame() string {
	if a.accessible {
		return ""
	}
	return a.spin.View()
}

func (a *app) fatalView(g geometry) string {
	what := "Could not open the workspace at " + a.displayRoot() + "."
	return a.theme.ErrorState(what+" "+errText(a.fatal),
		"Nothing was changed on disk.",
		"Fix the file named above and press r to try again, or run postctl --workspace <dir> to open another workspace.", g.width)
}

// mainBody renders the first-run guide or the pane layout.
func (a *app) mainBody(g geometry) string {
	if a.isFirstRun() {
		return a.firstRunView(g.width, g.bodyH)
	}
	t := a.theme
	content := g.bodyH - 1
	switch g.layout {
	case ui.LayoutThreePane:
		sidebar := t.Block(a.paneBlock(SlotSidebar, g.sidebarW, content), g.sidebarW, g.bodyH)
		req := t.Block(a.paneBlock(SlotRequest, g.requestW, content), g.requestW, g.bodyH)
		resp := t.Block(a.paneBlock(SlotResponse, g.responseW, content), g.responseW, g.bodyH)
		return ui.Columns([]string{sidebar, req, resp}, t.Gap())
	case ui.LayoutTwoPane:
		sidebar := t.Block(a.paneBlock(SlotSidebar, g.sidebarW, content), g.sidebarW, g.bodyH)
		right := a.rightShow
		if a.focus != SlotSidebar {
			right = a.focus
		}
		r := t.Block(a.switcherTitle(g.requestW, []Slot{SlotRequest, SlotResponse})+"\n"+a.paneView(right, g.requestW, content), g.requestW, g.bodyH)
		return ui.Columns([]string{sidebar, r}, t.Gap())
	default:
		show := a.focus
		title := a.switcherTitle(g.width, []Slot{SlotSidebar, SlotRequest, SlotResponse})
		return title + "\n" + a.paneView(show, g.width, content)
	}
}

// paneBlock is a pane with its title row, for the side-by-side layouts.
func (a *app) paneBlock(s Slot, w, h int) string {
	return a.paneTitle(s, w) + "\n" + a.paneView(s, w, h)
}

func (a *app) paneTitle(s Slot, w int) string {
	t := a.theme
	focused := a.focus == s
	title := s.String()
	detail := ""
	switch s {
	case SlotSidebar:
		if a.tree.requests > 0 {
			detail = plural(a.tree.requests, "request", "requests")
		}
	case SlotRequest:
		if tt, ok := a.request.(Titled); ok {
			title = tt.Title()
		}
	case SlotResponse:
		if tt, ok := a.response.(Titled); ok {
			title = tt.Title()
		}
	}
	return t.Truncate(t.Section(title, detail, focused), w)
}

// paneView renders one slot's content at w x h.
func (a *app) paneView(s Slot, w, h int) string {
	switch s {
	case SlotSidebar:
		return a.tree.view(a.focus == SlotSidebar)
	case SlotRequest:
		return a.theme.Block(a.request.View(), w, h)
	default:
		return a.theme.Block(a.response.View(), w, h)
	}
}

// switcherTitle is the title row when several slots share one area: each
// slot with its jump key, the focused one marked and bold accent.
func (a *app) switcherTitle(w int, slots []Slot) string {
	t := a.theme
	var parts []string
	for _, s := range slots {
		label := strconv.Itoa(int(s)+1) + " " + s.String()
		switch {
		case a.focus == s:
			parts = append(parts, t.Render(ui.AccentPrimary, t.Glyphs.Select+" "+label))
		case a.focus == SlotSidebar && a.rightShow == s && len(slots) == 2:
			parts = append(parts, t.Bold().Render("  "+label))
		default:
			parts = append(parts, t.Render(ui.TextSecondary, "  "+label))
		}
	}
	return t.Truncate(strings.Join(parts, "  "), w)
}

// ---- footer hints -------------------------------------------------------------

// hints are the footer hints for what has focus. Only valid keys appear.
func (a *app) hints() []ui.Hint {
	switch {
	case a.phase == phaseFatal:
		return []ui.Hint{hint("r", "Try again", 0), hint("q", "Quit", 0)}
	case a.phase == phaseLoading:
		return []ui.Hint{hint("q", "Quit", 0)}
	case a.ov != nil:
		return a.ov.hints(a)
	case a.isFirstRun():
		return a.firstRunHints()
	}
	global := []ui.Hint{
		hint("?", "Help", 0), hint("Ctrl-P", "Palette", 1), hint("E", "Environments", 3), hint("q", "Quit", 6),
	}
	var own []ui.Hint
	switch a.focus {
	case SlotSidebar:
		own = a.sidebarHints()
	case SlotRequest:
		own = a.request.KeyHints()
	default:
		own = a.response.KeyHints()
	}
	if a.focus != SlotSidebar {
		own = append(own, hint("Tab", "Next pane", 4))
	}
	if a.layoutKind() == ui.LayoutSingle && a.focus != SlotSidebar {
		own = append(own, hint("Ctrl-B", "Collections", 2))
	}
	return append(own, global...)
}

func (a *app) sidebarHints() []ui.Hint {
	if a.tree.filtering {
		return []ui.Hint{hint("Enter", "Keep results", 0), hint("Esc", "Clear filter", 0), hint(arrowsUD(a.ascii), "Move", 1)}
	}
	hs := []ui.Hint{}
	r, ok := a.tree.current()
	switch {
	case ok && r.kind == rowRequest:
		hs = append(hs, hint("Enter", "Open", 0))
	case ok && (r.kind == rowFolder || r.kind == rowCollection):
		hs = append(hs, hint("Enter", "Fold", 0))
	}
	hs = append(hs, hint("n", "New", 1), hint("/", "Filter", 1))
	if ok && r.kind != rowProblem {
		hs = append(hs, hint("r", "Rename", 4), hint("d", "Delete", 5), hint("m", "Move", 5), hint("D", "Duplicate", 6))
	}
	if a.tree.query != "" {
		hs = append(hs, hint("Esc", "Clear filter", 0))
	}
	if a.layoutKind() == ui.LayoutSingle {
		hs = append(hs, hint("Esc", "Close", 1))
	}
	return hs
}

func arrowsUD(ascii bool) string {
	if ascii {
		return "Up/Down"
	}
	return "↑↓"
}

// focusTitle names the focused pane for the help screen.
func (a *app) focusTitle() string { return a.focus.String() }

// focusHints are the focused pane's own hints, without the global ones.
func (a *app) focusHints() []ui.Hint {
	switch a.focus {
	case SlotSidebar:
		return a.sidebarHints()
	case SlotRequest:
		return a.request.KeyHints()
	default:
		return a.response.KeyHints()
	}
}
