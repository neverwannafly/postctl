package qa_test

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/request"
	"github.com/shubham/postctl/internal/tui/wspane"
	"github.com/shubham/postctl/internal/ui"
)

// uniqueSentinel returns a secret whose unique part comes first. The
// completion menu shortens long values, so a sentinel with its unique part at
// the end could be cut and a leak would pass unnoticed; the check below also
// searches the first 12 characters.
func uniqueSentinel(t *testing.T, label string) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(buf) + "-" + label + "-leak-sentinel"
}

// TestSecretDoesNotLeakThroughCompletionMenus opens the {{variable}}
// completion menu on secret variables of every scope (global, collection,
// folder, environment) in the request pane, and on an environment secret in
// the WebSocket pane, and searches every frame for the secrets: the full
// value and its first 12 characters, raw and with escape sequences stripped.
func TestSecretDoesNotLeakThroughCompletionMenus(t *testing.T) {
	envSecret := uniqueSentinel(t, "env")
	folderSecret := uniqueSentinel(t, "folder")
	collSecret := uniqueSentinel(t, "coll")
	globalSecret := uniqueSentinel(t, "global")
	secrets := []string{envSecret, folderSecret, collSecret, globalSecret}

	req := model.NewRequest("Complete here", http.MethodGet, "https://example.test/items")
	req.Headers = []model.KV{{Key: "X-Probe", Value: "x"}}
	wsReq := model.NewRequest("Socket", http.MethodGet, "wss://example.test/stream")
	wsReq.Kind = model.KindWebSocket
	folder := model.NewFolder("Folder")
	folder.Variables = []model.Variable{{Key: "folderToken", Value: folderSecret, Type: model.VarSecret}}
	folder.Items = []model.Item{req, wsReq}
	coll := model.NewCollection("Collection")
	coll.Variables = []model.Variable{{Key: "collToken", Value: collSecret, Type: model.VarSecret}}
	coll.Items = []model.Item{folder}
	env := &model.Environment{ID: model.NewID(), Name: "Env", Variables: []model.Variable{
		{Key: "accessToken", Value: envSecret, Type: model.VarSecret},
		{Key: "plainHost", Value: "example.test"},
	}}
	ws := &model.Workspace{
		ID: model.NewID(), Name: "Completion sweep",
		Collections:  []*model.Collection{coll},
		Environments: []*model.Environment{env},
		Globals: &model.Globals{Variables: []model.Variable{
			{Key: "globalToken", Value: globalSecret, Type: model.VarSecret},
		}},
	}

	theme := ui.NewTheme(true, true, false)
	paneEnv := tui.PaneEnv{Theme: theme, Actions: &tui.Registry{}}
	ref := tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, folder.Name, req.Name}}
	wsRef := tui.RequestRef{Collection: coll, Request: wsReq, Path: []string{coll.Name, folder.Name, wsReq.Name}}
	bind := func(p tui.Pane, r tui.RequestRef) {
		p.SetSize(140, 40)
		_, _ = p.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
		_, _ = p.Update(tui.EnvironmentChangedMsg{Env: env})
		_, _ = p.Update(tui.SelectionMsg{Ref: r})
		_ = p.Focus()
	}
	press := func(p tui.Pane, keys ...tea.KeyPressMsg) {
		for _, k := range keys {
			_, _ = p.Update(k)
		}
	}
	typeText := func(p tui.Pane, text string) {
		for _, r := range text {
			press(p, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	key := func(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }
	up := tea.KeyPressMsg{Code: tea.KeyUp}
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	right := tea.KeyPressMsg{Code: tea.KeyRight}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	tab := tea.KeyPressMsg{Code: tea.KeyTab}

	type frame struct{ name, shown, text string }
	var frames []frame
	capture := func(name string, p tui.Pane, variable string) {
		frames = append(frames, frame{name: name, shown: variable, text: p.View()})
	}
	requestURL := func(typed string) tui.Pane {
		p := request.New(paneEnv, request.Deps{})
		bind(p, ref)
		press(p, up, key('A'))
		typeText(p, typed)
		return p
	}

	capture("request url, environment secret", requestURL("{{acc"), "accessToken")
	capture("request url, folder secret", requestURL("{{folderT"), "folderToken")
	capture("request url, collection secret", requestURL("{{collT"), "collToken")
	capture("request url, global secret", requestURL("{{globalT"), "globalToken")

	header := request.New(paneEnv, request.Deps{})
	bind(header, ref)
	press(header, right, right, down, enter, tab, key('A'))
	typeText(header, "{{acc")
	capture("request header value, environment secret", header, "accessToken")

	socket := wspane.New(paneEnv, wspane.Deps{})
	bind(socket, wsRef)
	press(socket, up, key('A'))
	typeText(socket, "{{acc")
	capture("websocket url, environment secret", socket, "accessToken")

	socketGlobal := wspane.New(paneEnv, wspane.Deps{})
	bind(socketGlobal, wsRef)
	press(socketGlobal, up, key('A'))
	typeText(socketGlobal, "{{globalT")
	capture("websocket url, global secret", socketGlobal, "globalToken")

	// The searches come first, so a leak is reported even when the menu also
	// lacks its mask glyph.
	for _, f := range frames {
		for _, secret := range secrets {
			assertAbsent(t, secret, f.name+" frame", []byte(f.text))
			assertAbsent(t, secret[:12], f.name+" frame (first 12 characters)", []byte(f.text))
		}
	}
	for _, f := range frames {
		stripped := ansi.Strip(f.text)
		if !strings.Contains(stripped, f.shown) {
			t.Errorf("%s: the menu does not list %s, so the completion was not exercised", f.name, f.shown)
		}
		if !strings.Contains(stripped, theme.Glyphs.Mask) {
			t.Errorf("%s: the menu shows no masked detail", f.name)
		}
	}
}
