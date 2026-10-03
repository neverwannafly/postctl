package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// whichKeyDelay is how long a pending prefix waits for its next key before
// the which-key popup lists the choices.
const whichKeyDelay = 300 * time.Millisecond

// Prefix keys that wait for a follow-up.
const (
	prefixLeader = "space"
	prefixGoto   = "g"
)

// pendingKeys is a prefix key waiting for its next key.
type pendingKeys struct {
	prefix string // one of the prefix constants
	seq    int    // identifies the timer that shows the popup
	shown  bool   // the which-key popup is on screen
}

// whichKeyMsg fires whichKeyDelay after a prefix key and asks the shell to
// show the popup if that prefix is still pending.
type whichKeyMsg struct{ seq int }

// leaderBinding maps the keys that follow Space to shell or pane actions. The
// first action in ids that exists and is enabled runs; when none does the key
// is hidden from which-key and does nothing.
type leaderBinding struct {
	keys  []string // follow-up keys; the first one is displayed
	ids   []string // Action.ID candidates, tried in order
	label string   // short which-key label
}

// leaderBindings is the Space table from docs/design-language.md, in display
// order. Action IDs that no registry provides (request.find, history.request,
// response.diff, response.jq on a build without those workstreams) leave their
// key hidden and inert.
var leaderBindings = []leaderBinding{
	{[]string{"f", "space"}, []string{"request.find"}, "Find request"},
	{[]string{"p"}, []string{"palette.open"}, "Command palette"},
	{[]string{"e"}, []string{"env.switch"}, "Switch environment"},
	{[]string{"E"}, []string{"env.edit"}, "Edit environments"},
	{[]string{"h"}, []string{"history.open"}, "History"},
	{[]string{"H"}, []string{"history.request"}, "History of request"},
	{[]string{"a"}, []string{"tree.new"}, "New request or folder"},
	{[]string{"r"}, []string{"tree.rename"}, "Rename"},
	{[]string{"d"}, []string{"tree.delete"}, "Delete"},
	{[]string{"D"}, []string{"tree.duplicate"}, "Duplicate"},
	{[]string{"m"}, []string{"tree.move"}, "Move to folder"},
	{[]string{"R"}, []string{"runner.open"}, "Run collection"},
	{[]string{"y"}, []string{"request.curl"}, "Copy as curl"},
	{[]string{"Y"}, []string{"request.curl.reveal"}, "Copy curl, revealed"},
	{[]string{"o"}, []string{"request.editor"}, "Open in $EDITOR"},
	{[]string{"v"}, []string{"request.reveal"}, "Reveal secrets"},
	{[]string{"i"}, []string{"request.snippet"}, "Insert snippet"},
	{[]string{"c"}, []string{"cookies.open"}, "Cookies"},
	{[]string{"b"}, []string{"sidebar.toggle"}, "Toggle sidebar"},
	{[]string{"l"}, []string{"workspace.reload"}, "Reload workspace"},
	{[]string{"x"}, []string{"response.diff"}, "Diff with previous"},
	{[]string{"j"}, []string{"response.jq"}, "jq filter"},
	{[]string{"s"}, []string{"request.send", "ws.send", "ws.connect"}, "Send"},
	{[]string{"q"}, []string{"app.quit"}, "Quit"},
	{[]string{"["}, []string{"jump.back"}, "Back (jump list)"},
	{[]string{"]"}, []string{"jump.forward"}, "Forward (jump list)"},
	{[]string{"?"}, []string{"help.keys"}, "All keys"},
}

// gotoSection is one g-prefix jump.
type gotoSection struct {
	key     string
	section string
	label   string
	slot    Slot
}

// gotoSections are the section jumps; gg (top) is handled separately.
var gotoSections = []gotoSection{
	{"u", SectionURL, "URL", SlotRequest},
	{"p", SectionParams, "Params", SlotRequest},
	{"a", SectionAuth, "Auth", SlotRequest},
	{"h", SectionHeaders, "Headers", SlotRequest},
	{"b", SectionBody, "Body", SlotRequest},
	{"s", SectionPre, "Pre-request script", SlotRequest},
	{"t", SectionTests, "Tests", SlotRequest},
	{"S", SectionSettings, "Settings", SlotRequest},
	{"m", SectionMessages, "WebSocket messages", SlotRequest},
	{"r", SectionRespBody, "Response body", SlotResponse},
	{"H", SectionRespHistory, "Response history", SlotResponse},
}

// usable reports whether an action can run now.
func usable(act Action) bool {
	return act.Run != nil && (act.Enabled == nil || act.Enabled())
}

// findAction looks an action up by ID: first in the focused pane's own
// actions, then in the registry (shell, screens and every pane).
func (a *app) findAction(id string) (Action, bool) {
	var focused Pane
	switch a.focus {
	case SlotRequest:
		focused = a.request
	case SlotResponse:
		if !a.responseHidden() {
			focused = a.response
		}
	}
	if src, ok := focused.(ActionSource); ok {
		for _, act := range src.Actions() {
			if act.ID == id && usable(act) {
				return act, true
			}
		}
	}
	for _, act := range a.registry.All() {
		if act.ID == id {
			return act, true
		}
	}
	return Action{}, false
}

// resolve returns the first usable action among ids.
func (a *app) resolve(ids []string) (Action, bool) {
	for _, id := range ids {
		if act, ok := a.findAction(id); ok {
			return act, true
		}
	}
	return Action{}, false
}

// runAction runs the action with the given ID if it exists and is enabled.
func (a *app) runAction(ids ...string) tea.Cmd {
	if act, ok := a.resolve(ids); ok {
		return act.Run()
	}
	return nil
}

// whichEntry is one row of the which-key popup.
type whichEntry struct {
	Key   string
	Label string
}

// leaderEntries lists the leader keys whose action is available now.
func (a *app) leaderEntries() []whichEntry {
	var out []whichEntry
	for _, lb := range leaderBindings {
		if _, ok := a.resolve(lb.ids); !ok {
			continue
		}
		keys := displayKeys(lb.keys)
		label := lb.label
		if len(keys) > 1 { // a second key for the same action: first key shown, rest named
			label += " (or " + strings.Join(keys[1:], ", ") + ")"
		}
		out = append(out, whichEntry{Key: keys[0], Label: label})
	}
	return out
}

// displayKeys renders follow-up key names for people.
func displayKeys(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = displayKey(k)
	}
	return out
}

func displayKey(k string) string {
	if k == "space" || k == " " {
		return "Space"
	}
	return k
}

// sectionsOf lists the sections the pane in slot offers right now.
func (a *app) sectionsOf(s Slot) []string {
	var p Pane
	switch s {
	case SlotRequest:
		p = a.request
	case SlotResponse:
		if a.responseHidden() {
			return nil
		}
		p = a.response
	}
	if l, ok := p.(SectionLister); ok {
		return l.Sections()
	}
	return nil
}

func hasSection(list []string, name string) bool {
	for _, s := range list {
		if s == name {
			return true
		}
	}
	return false
}

// gotoEntries lists the g-prefix keys that do something now.
func (a *app) gotoEntries() []whichEntry {
	out := []whichEntry{{Key: "g", Label: "Top of the list"}}
	for _, gs := range gotoSections {
		if hasSection(a.sectionsOf(gs.slot), gs.section) {
			out = append(out, whichEntry{Key: gs.key, Label: gs.label})
		}
	}
	return out
}

// bracketEntries describes the [ and ] prefixes.
func (a *app) bracketEntries() []whichEntry {
	return []whichEntry{{Key: "[", Label: "Previous section tab"}, {Key: "]", Label: "Next section tab"}}
}

// pendingEntries lists the follow-up keys of the pending prefix.
func (a *app) pendingEntries() []whichEntry {
	if a.pend == nil {
		return nil
	}
	switch a.pend.prefix {
	case prefixLeader:
		return a.leaderEntries()
	case prefixGoto:
		return a.gotoEntries()
	default:
		return a.bracketEntries()
	}
}

// startPending arms a prefix and the timer that shows the popup.
func (a *app) startPending(prefix string) tea.Cmd {
	a.pendSeq++
	seq := a.pendSeq
	a.pend = &pendingKeys{prefix: prefix, seq: seq}
	delay := a.whichKeyDelay
	return tea.Tick(delay, func(time.Time) tea.Msg { return whichKeyMsg{seq: seq} })
}

// onWhichKey shows the popup when its prefix is still pending.
func (a *app) onWhichKey(m whichKeyMsg) {
	if a.pend != nil && a.pend.seq == m.seq {
		a.pend.shown = true
	}
}

// pendingLabel is the footer text for the pending prefix.
func (a *app) pendingLabel() string {
	if a.pend == nil {
		return ""
	}
	if a.pend.prefix == prefixLeader {
		return "Space…"
	}
	return a.pend.prefix + "…"
}

// followUp handles the key after a prefix. It reports false when the key is
// not part of the sequence and should be processed as if no prefix was
// pending (the always-available shortcuts, and any key after [ or ]).
func (a *app) followUp(p *pendingKeys, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
	switch s {
	case "ctrl+p", "ctrl+b", "ctrl+s", "ctrl+enter", "alt+1", "alt+2", "alt+3":
		return nil, false
	case "esc", "ctrl+c":
		return nil, true
	}
	switch p.prefix {
	case prefixLeader:
		return a.leaderKey(s), true
	case prefixGoto:
		return a.gotoKey(s), true
	default:
		if s == "[" || s == "]" {
			delta := -1
			if s == "]" {
				delta = 1
			}
			cmd := a.stepSection(delta)
			return tea.Batch(cmd, a.startPending(s)), true
		}
		return a.noBinding(p.prefix + displayKey(s)), true
	}
}

// noBinding cancels a sequence and says so. Only key names are shown.
func (a *app) noBinding(keys string) tea.Cmd {
	return a.toastInfo("No binding for " + keys)
}

// leaderKey runs the action bound to s after Space.
func (a *app) leaderKey(s string) tea.Cmd {
	for _, lb := range leaderBindings {
		if !containsKey(lb.keys, s) {
			continue
		}
		if act, ok := a.resolve(lb.ids); ok {
			return act.Run()
		}
		return a.toastInfo("Space " + displayKey(s) + " (" + lb.label + ") is not available here.")
	}
	return a.noBinding("Space " + displayKey(s))
}

func containsKey(keys []string, s string) bool {
	if s == " " {
		s = "space"
	}
	for _, k := range keys {
		if k == s {
			return true
		}
	}
	return false
}

// gotoKey runs the g-prefix jump for s.
func (a *app) gotoKey(s string) tea.Cmd {
	if s == "g" {
		return a.goTop()
	}
	for _, gs := range gotoSections {
		if gs.key != s {
			continue
		}
		if !hasSection(a.sectionsOf(gs.slot), gs.section) {
			return a.toastInfo("g" + s + " (" + gs.label + ") is not available here.")
		}
		return a.focusSection(gs.slot, gs.section)
	}
	return a.noBinding("g" + s)
}

// focusSection focuses slot and asks its pane to show section.
func (a *app) focusSection(slot Slot, section string) tea.Cmd {
	cmds := []tea.Cmd{a.setFocus(slot)}
	msg := FocusSectionMsg{Section: section}
	var cmd tea.Cmd
	switch slot {
	case SlotRequest:
		a.request, cmd = a.request.Update(msg)
	case SlotResponse:
		a.response, cmd = a.response.Update(msg)
	}
	return tea.Batch(append(cmds, cmd)...)
}

// goTop is gg: the first row of the tree, or the top of the focused pane.
func (a *app) goTop() tea.Cmd {
	if a.focus == SlotSidebar {
		a.tree.home()
		return a.syncSelection(false)
	}
	return a.updateFocused(keyNamed(tea.KeyHome))
}

// keyNamed builds a key press for a named key, used to translate vim motions
// into the keys the existing widgets understand.
func keyNamed(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

// stepSection sends [ or ] to the focused pane.
func (a *app) stepSection(delta int) tea.Cmd {
	msg := SectionStepMsg{Delta: delta}
	var cmd tea.Cmd
	switch a.focus {
	case SlotRequest:
		a.request, cmd = a.request.Update(msg)
	case SlotResponse:
		a.response, cmd = a.response.Update(msg)
	}
	return cmd
}
