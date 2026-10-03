package wspane

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/tui"
)

// state is the current request's connection state.
func (p *Pane) state() connState {
	if l := p.peek(); l != nil {
		return l.state
	}
	return stIdle
}

// Actions implements tui.ActionSource.
func (p *Pane) Actions() []tui.Action {
	ok := p.editable
	idle := func() bool { return p.editable() && p.state() == stIdle }
	active := func() bool { return p.editable() && p.state() != stIdle }
	open := func() bool { return p.editable() && p.state() == stOpen }
	hasLog := func() bool {
		l := p.peek()
		return p.editable() && l != nil && len(l.log.entries) > 0
	}
	sendKeys := "Ctrl-S"
	if p.enhanced {
		sendKeys = "Ctrl-Enter"
	}
	acts := []tui.Action{
		{ID: "ws.connect", Title: "Connect", Group: "WebSocket", Keys: sendKeys, Enabled: idle, Run: p.connect},
		{ID: "ws.disconnect", Title: "Disconnect", Group: "WebSocket", Enabled: active, Run: func() tea.Cmd {
			if p.state() == stConnecting {
				return p.cancelConnect()
			}
			return p.disconnect()
		}},
		{ID: "ws.reconnect", Title: "Reconnect", Group: "WebSocket", Enabled: ok, Run: p.reconnect},
		{ID: "ws.send", Title: "Send message", Group: "WebSocket", Keys: sendKeys, Enabled: open, Run: p.sendComposer},
		{ID: "ws.ping", Title: "Send ping", Group: "WebSocket", Enabled: open, Run: p.ping},
		{ID: "ws.formatJSON", Title: "Format JSON message", Group: "WebSocket", Keys: "Ctrl-R", Enabled: ok, Run: p.formatJSON},
		{ID: "ws.format", Title: "Change message format (text, JSON, binary)", Group: "WebSocket", Keys: "Ctrl-T", Enabled: ok, Run: p.cycleFormat},
		{ID: "ws.saveMessage", Title: "Save message as template", Group: "WebSocket", Keys: "Ctrl-X", Enabled: ok, Run: p.saveTemplate},
		{ID: "ws.copyMessage", Title: "Copy selected log message", Group: "WebSocket", Keys: "c", Enabled: hasLog, Run: p.copySelected},
		{ID: "ws.filterLog", Title: "Filter log by direction", Group: "WebSocket", Keys: "f", Enabled: hasLog, Run: p.cycleFilter},
		{ID: "ws.searchLog", Title: "Search log", Group: "WebSocket", Keys: "/", Enabled: hasLog, Run: func() tea.Cmd {
			p.tab, p.sect = tabMessages, sLog
			p.setZone(zBody)
			p.beginSearch()
			return msgCmd(tui.FocusPaneMsg{Slot: tui.SlotRequest})
		}},
		{ID: "ws.clearLog", Title: "Clear message log", Group: "WebSocket", Keys: "x", Enabled: hasLog, Run: p.clearLog},
		{ID: "ws.focusURL", Title: "Edit WebSocket URL", Group: "WebSocket", Keys: "gu", Enabled: ok, Run: func() tea.Cmd {
			p.stopEditing()
			p.setZone(zBar)
			return msgCmd(tui.FocusPaneMsg{Slot: tui.SlotRequest})
		}},
	}
	for i := range numTabs {
		acts = append(acts, tui.Action{
			ID: "ws.tab." + strings.ToLower(tabNames[i]), Title: "Go to " + tabNames[i] + " tab (WebSocket)", Group: "WebSocket", Keys: "g" + tabKeys[i], Enabled: ok,
			Run: func() tea.Cmd {
				p.stopEditing()
				p.tab = i
				p.enterBody()
				return msgCmd(tui.FocusPaneMsg{Slot: tui.SlotRequest})
			},
		})
	}
	return acts
}
