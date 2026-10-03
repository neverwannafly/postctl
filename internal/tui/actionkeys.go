package tui

import "strings"

// shellKeyActions maps the single keys and short sequences the shell itself
// dispatches (outside the Space and g tables) to the action IDs they run.
// ":q" is the command line.
var shellKeyActions = map[string][]string{
	"ctrl+p":     {"palette.open"},
	"ctrl+b":     {"sidebar.toggle"},
	"ctrl+o":     {"jump.back"},
	"ctrl+i":     {"jump.forward"},
	"ctrl+s":     {"request.send", "ws.send", "ws.connect"},
	"ctrl+enter": {"request.send", "ws.send", "ws.connect"},
	"ctrl+r":     {"request.redo"},
	"u":          {"request.undo"},
	"?":          {"help.keys"},
	"q":          {"app.quit"},
	":q":         {"app.quit"},
	"/":          {"tree.filter"},
	"1":          {"focus.collections"},
	"2":          {"focus.request"},
	"3":          {"focus.response"},
}

// gotoActionIDs are the pane actions a g key stands for (the pane handles the
// key itself through FocusSectionMsg).
var gotoActionIDs = map[string][]string{
	"u": {"request.focusURL", "ws.focusURL"},
	"p": {"request.tab.params", "ws.tab.params"},
	"a": {"request.tab.auth", "ws.tab.auth"},
	"h": {"request.tab.headers", "ws.tab.headers"},
	"b": {"request.tab.body"},
	"s": {"request.tab.pre-request"},
	"t": {"request.tab.tests"},
	"S": {"request.tab.settings", "ws.tab.settings"},
	"m": {"ws.tab.messages"},
}

// UnboundActionKeys checks an [Action.Keys] string against the shell's key
// tables. Keys lists alternatives separated by " / " ("Ctrl-P / Space p");
// the result holds the alternatives that the shell does not dispatch to the
// action with the given ID, so an empty result means every alternative is a
// real binding. Alternatives that belong to a pane (a letter its own Update
// handles) show up in the result and the pane's tests vouch for them. An empty
// keys string is always fine.
func UnboundActionKeys(id, keys string) []string {
	var bad []string
	for _, alt := range strings.Split(keys, " / ") {
		alt = strings.TrimSpace(alt)
		if alt == "" || shellBinds(id, alt) {
			continue
		}
		bad = append(bad, alt)
	}
	return bad
}

func shellBinds(id, alt string) bool {
	has := func(ids []string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	switch {
	case strings.HasPrefix(alt, "Space "):
		k := strings.TrimPrefix(alt, "Space ")
		for _, lb := range leaderBindings {
			for _, lk := range lb.keys {
				if displayKey(lk) == k && has(lb.ids) {
					return true
				}
			}
		}
		return false
	case len(alt) == 2 && alt[0] == 'g':
		return has(gotoActionIDs[alt[1:]])
	}
	norm := alt
	if lower := strings.ToLower(alt); strings.HasPrefix(lower, "ctrl-") || strings.HasPrefix(lower, "alt-") {
		norm = strings.ReplaceAll(lower, "-", "+")
	}
	return has(shellKeyActions[norm])
}
