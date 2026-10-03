//go:generate go run . -out ../../docs/keys.md

// Command genkeys generates the complete key reference for postctl in docs/keys.md.
//
// Usage:
//
//	go run ./cmd/genkeys [-out docs/keys.md]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyEntry describes a single keyboard shortcut.
type KeyEntry struct {
	Key         string
	Action      string
	Description string
}

// Section groups key entries by screen or context.
type Section struct {
	Title       string
	Description string
	Entries     []KeyEntry
}

// KeyCatalog defines the complete key reference across all contexts.
var KeyCatalog = []Section{
	{
		Title:       "Global Shortcuts",
		Description: "Available everywhere unless a text field is being edited. Ctrl-P, Ctrl-B, Alt-1/2/3, Ctrl-S and Ctrl-Enter work even while typing. Ctrl-P, Alt-1/2/3, Ctrl-S and Ctrl-Enter also work in the palette, request switcher, help, environment switcher and which-key popup (the popup closes, then the key acts); Ctrl-B does not. Confirmation dialogs and prompts must be answered first and ignore them; full-screen screens (environments, history, runner, cookies) keep their own keys, except Ctrl-P, which closes them. Ctrl-Enter and Ctrl-i need a terminal with enhanced keyboard reporting.",
		Entries: []KeyEntry{
			{"?", "Help", "Open the keyboard shortcut and context help overlay"},
			{"Ctrl-P", "Command palette", "Open the fuzzy command palette (press again to close; inside the request switcher it moves the cursor up)"},
			{":", "Command line", "Open the palette as a command line: q or wq quits, q! quits without waiting for saves, w saves now"},
			{"Space", "Leader", "Start a key sequence; after 300 ms the which-key popup lists the keys that are available (see Leader keys)"},
			{"g", "Go to", "Start a g sequence: gg top, gu URL, gp Params, ... (see Go-to and section keys)"},
			{"[, ]", "Previous / next section tab", "Step through the tabs of the focused pane; press again to keep stepping"},
			{"q", "Quit", "Quit postctl (waits for pending saves)"},
			{"Ctrl-C", "Quit", "Press twice within a second to quit; in a text field it acts as Esc, and it cancels a pending Space or g"},
			{"Tab", "Next pane", "Cycle focus forward: Collections → Request → Response"},
			{"Shift-Tab", "Previous pane", "Cycle focus backward: Response → Request → Collections"},
			{"Ctrl-B", "Toggle sidebar", "Show or hide the Collections sidebar"},
			{"/", "Filter / search", "Contextual: with Collections focused it filters requests by name/path; in the Response pane it searches the body; in the WebSocket pane it searches the log; elsewhere it does nothing"},
			{"1, Alt-1", "Collections pane", "Jump directly to the Collections sidebar"},
			{"2, Alt-2", "Request pane", "Jump directly to the Request editor pane"},
			{"3, Alt-3", "Response pane", "Jump directly to the Response viewer pane"},
			{"Ctrl-h", "Pane to the left", "Move focus to the neighbouring pane on the left (the edge hands the move to the host, for example tmux)"},
			{"Ctrl-j", "Pane below", "Move focus to the pane below (two- and single-pane layouts stack Request above Response)"},
			{"Ctrl-k", "Pane above", "Move focus to the pane above"},
			{"Ctrl-l", "Pane to the right", "Move focus to the neighbouring pane on the right"},
			{"Ctrl-o", "Jump back", "Go back to the previously opened request (jump list, 100 entries)"},
			{"Ctrl-i", "Jump forward (needs enhanced keyboard)", "Go forward in the jump list; on terminals without enhanced keyboard reporting Ctrl-i is Tab, use Space ] instead"},
			{"Esc", "Back / cancel", "Leave a text field, then close overlays and dialogs, then cancel a running send or OAuth login (from any pane)"},
		},
	},
	{
		Title:       "Leader keys (Space)",
		Description: "Press Space, then one of these keys. A key is listed and works only when its action exists and is enabled for the focused pane; otherwise it is hidden and does nothing. Every row is also a command-palette action. Keys whose feature has not shipped yet are not listed.",
		Entries: []KeyEntry{
			{"Space f, Space Space", "Find request", "Open the request switcher: fuzzy-find any request in the workspace, most recent first"},
			{"Space p", "Command palette", "Open the command palette"},
			{"Space e", "Switch environment", "Open the active environment switcher"},
			{"Space E", "Edit environments", "Open the environment and variable editor"},
			{"Space h", "History", "Open the request send history screen"},
			{"Space a", "New", "Create a request, folder or collection (on the first-run screen: create a workspace)"},
			{"Space r", "Rename", "Rename the selected collection, folder or request"},
			{"Space d", "Delete", "Delete the selected item (confirms; Cancel is the default)"},
			{"Space D", "Duplicate", "Duplicate the selected request or folder with fresh IDs"},
			{"Space m", "Move", "Move the selected item to another folder"},
			{"Space R", "Run collection", "Open the collection runner for the selected collection or folder"},
			{"Space y", "Copy as curl", "Copy the request as curl with secrets masked"},
			{"Space Y", "Copy curl (with secrets)", "Copy the request as curl with secret values revealed"},
			{"Space o", "Open in $EDITOR", "Edit the current request tab in your external editor"},
			{"Space v", "Reveal secrets", "Show or hide masked secret fields in the Auth tab"},
			{"Space i", "Insert snippet", "Open the snippet list on a script tab"},
			{"Space c", "Cookies", "Open the cookie manager"},
			{"Space b", "Toggle sidebar", "Show or hide the Collections sidebar"},
			{"Space l", "Reload workspace", "Reload workspace YAML and script files from disk"},
			{"Space s", "Send", "Send the request, or connect and send for a WebSocket request"},
			{"Space [, Space ]", "Jump back / forward", "Walk the jump list of opened requests"},
			{"Space q", "Quit", "Quit postctl"},
			{"Space ?", "All keys", "Open the help overlay"},
			{"Esc", "Cancel", "Cancel the pending sequence without doing anything"},
		},
	},
	{
		Title:       "Go-to and section keys (g, [, ])",
		Description: "A g key lists only the sections the focused pane really has. Pressing a section key focuses that pane and shows the section.",
		Entries: []KeyEntry{
			{"gg", "Top", "First row of the tree, or the top of the focused pane"},
			{"G", "Bottom", "Last row of the tree, or the end of the focused pane"},
			{"gu", "URL", "Edit the request URL"},
			{"gp", "Params", "Request: Params tab"},
			{"ga", "Auth", "Request: Auth tab"},
			{"gh", "Headers", "Request: Headers tab"},
			{"gb", "Body", "Request: Body tab"},
			{"gs", "Pre-request script", "Request: Pre-request tab"},
			{"gt", "Tests", "Request: Tests tab"},
			{"gS", "Settings", "Request: Settings tab"},
			{"gm", "Messages", "WebSocket: Messages tab"},
			{"gr", "Response body", "Focus the response pane on its Body tab"},
			{"[, ]", "Previous / next tab", "Step through the tabs of the focused pane; repeat without the prefix"},
		},
	},
	{
		Title:       "Collections Sidebar (Tree)",
		Description: "Navigating, organizing, and executing collections, folders, and requests.",
		Entries: []KeyEntry{
			{"↑, k", "Move up", "Move selection to the previous item in the tree"},
			{"↓, j", "Move down", "Move selection to the next item in the tree"},
			{"PgUp, PageUp, Ctrl-u", "Page up", "Scroll selection up one page"},
			{"PgDn, PageDown, Ctrl-d", "Page down", "Scroll selection down one page"},
			{"Home, gg", "First row", "Jump to the very first item in the tree"},
			{"End, G", "Last row", "Jump to the very last item in the tree"},
			{"←, h", "Fold folder or go to parent", "Collapse expanded folder, or jump to parent container"},
			{"→, l", "Unfold folder", "Expand collapsed folder to reveal children"},
			{"Enter", "Open / toggle", "Open request in editor, or expand/collapse folder"},
			{"Alt-↑, Alt-Up", "Move up in folder", "Reorder item up among siblings in its folder"},
			{"Alt-↓, Alt-Down", "Move down in folder", "Reorder item down among siblings in its folder"},
			{"Space a / r / D / m / d / R", "Organize", "New, rename, duplicate, move, delete and run act on the selected row through the leader (see Leader keys)"},
			{"u, Ctrl-r", "Undo / redo", "Undo or redo the last edit of the open request"},
			{"/", "Filter requests", "Activate fuzzy filter across collection requests"},
			{"Esc", "Clear filter / Close", "Clear filter text; close the overlay in single-pane mode; otherwise stop the selected request's running send"},
		},
	},
	{
		Title:       "Request Pane — General & URL Bar",
		Description: "Editing request target, method, and global pane shortcuts.",
		Entries: []KeyEntry{
			{"Ctrl-S, Ctrl-Enter", "Send request", "Send the active request immediately (Ctrl-S always works; Ctrl-Enter needs enhanced keyboard reporting)"},
			{"u", "Undo", "Undo the previous edit (when not typing)"},
			{"Ctrl-r", "Redo", "Redo the previously undone edit (when not typing)"},
			{"Ctrl-Z", "Undo (typing)", "Undo the previous edit while typing in a field"},
			{"Ctrl-Y", "Redo (typing)", "Redo the previously undone edit while typing in a field"},
			{"gu", "Focus URL bar", "Jump cursor directly to the URL input line"},
			{"Esc", "Cancel send / OAuth", "Cancel in-flight HTTP request or pending OAuth2 browser login"},
			{"Enter (URL bar)", "Send request", "Send request immediately while editing URL"},
			{"Shift-Tab (URL bar)", "Method selector", "Open the HTTP method dropdown selector"},
			{"Tab, Down (URL bar)", "Focus tabs", "Move focus down from URL bar to the request tab row"},
			{"Esc (URL bar)", "Focus tabs", "Move focus from URL bar to the request tab row"},
			{"←, →, ↑, ↓ (Method)", "Change method", "Cycle through GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS"},
			{"Enter, Tab (Method)", "Choose method", "Select the highlighted HTTP method"},
			{"Esc (Method)", "Cancel selector", "Dismiss the method selector without changes"},
			{"A–Z (Method)", "Typeahead jump", "Jump to the next method starting with typed letter"},
		},
	},
	{
		Title:       "Request Pane — Tab Navigation & Curl Export",
		Description: "Navigating request sections and generating curl commands.",
		Entries: []KeyEntry{
			{"←, →, [, ]", "Switch tab", "Cycle between Params, Auth, Headers, Body, Pre, Tests, Settings (gp ga gh gb gs gt gS jump straight to one)"},
			{"Home, End", "First / last tab", "Jump to first tab (Params) or last tab (Settings)"},
			{"↑", "Focus URL", "Move focus back up to the URL address bar"},
			{"↓, Enter", "Focus tab content", "Move focus into the active tab's fields or editor"},
			{"m", "Method selector", "Open the HTTP method dropdown selector"},
			{"Space y", "Copy as curl", "Copy shell-quoted curl command to clipboard (secrets masked)"},
			{"Space Y", "Copy curl (with secrets)", "Copy curl command with revealed secret variable values"},
		},
	},
	{
		Title:       "Request Pane — Key-Value Tables (Params, Headers, Form)",
		Description: "Managing query parameters, headers, URL-encoded fields, and form-data parts.",
		Entries: []KeyEntry{
			{"↑, ↓", "Move row", "Move selection between table rows"},
			{"PgUp, PgDn", "Page up / down", "Scroll table up or down by a page"},
			{"Home, End", "First / last row", "Jump to first or last table row"},
			{"a, Insert", "Add row", "Insert a new row at the bottom of the table"},
			{"Enter", "Edit field", "Edit active cell (or add new row if table is empty)"},
			{"x", "Toggle enabled", "Enable or disable current row without deleting it (Space is the leader)"},
			{"d, Delete", "Delete row", "Delete the selected row"},
			{"Alt-↑, Shift-↑", "Move row up", "Shift current row up in evaluation order"},
			{"Alt-↓, Shift-↓", "Move row down", "Shift current row down in evaluation order"},
			{"b", "Bulk edit", "Open raw multiline text area to paste key:value pairs"},
			{"t", "Toggle Text / File", "Switch form-data part between text value and file upload"},
			{"Tab (editing)", "Next field", "Advance editing focus: Key → Value → Description"},
			{"Shift-Tab (editing)", "Previous field", "Move editing focus backward"},
			{"Enter, Esc (editing)", "Done editing", "Commit changes and return to row selection"},
			{"Esc (bulk edit)", "Apply bulk edit", "Parse multiline text into table rows and close"},
		},
	},
	{
		Title:       "Request Pane — Body Tab",
		Description: "Configuring request payload across raw, form, binary, and GraphQL modes.",
		Entries: []KeyEntry{
			{"←, → (Type row)", "Body type", "Cycle: none, raw, form-urlencoded, form-data, file, graphql"},
			{"Enter (Type)", "Next type", "Advance to next body type"},
			{"←, → (Lang row)", "Language", "Cycle raw syntax: JSON, XML, HTML, Text, JavaScript"},
			{"Enter, i", "Edit body", "Focus inline textarea to edit raw or GraphQL body"},
			{"f", "Format JSON", "Pretty-print and format JSON body (preserves {{variables}})"},
			{"Space o", "Open in $EDITOR", "Edit full payload in external editor and reload on save"},
			{"Tab (GraphQL)", "Query / Vars", "Switch editor focus between GraphQL Query and Variables"},
			{"Esc", "Exit editor", "Leave body textarea and return to field navigation"},
		},
	},
	{
		Title:       "Request Pane — Auth Tab",
		Description: "Configuring authentication schemes and OAuth 2.0 token acquisition.",
		Entries: []KeyEntry{
			{"←, → (Type row)", "Auth type", "Cycle: inherit, none, bearer, basic, apikey, digest, oauth2, awsv4, hawk, ntlm"},
			{"Enter (Type)", "Next auth type", "Advance to next authentication scheme"},
			{"Enter", "Edit field", "Focus form field for editing"},
			{"Tab, Shift-Tab", "Navigate fields", "Move focus forward or backward through auth inputs"},
			{"Space v", "Reveal secrets", "Toggle visibility of masked secret inputs in the form (not while typing)"},
			{"Enter (Get new token)", "Get OAuth token", "Initiate browser login for OAuth2 with local loopback listener"},
			{"c", "Copy sign-in link", "Copy OAuth authorization URL to clipboard while waiting"},
			{"Esc", "Cancel OAuth / Done", "Cancel waiting OAuth login or commit field editing"},
		},
	},
	{
		Title:       "Request Pane — Scripts & Settings Tabs",
		Description: "Authoring pre-request and test scripts, code snippets, and request options.",
		Entries: []KeyEntry{
			{"Enter, i", "Edit script", "Focus script editor textarea"},
			{"Space i", "Snippets picker", "Open dropdown of Chai assertions and pm.* snippet templates"},
			{"Space o", "Open in $EDITOR", "Edit script in external editor (saved as .pre.js or .test.js)"},
			{"Tab (editing script)", "Indent", "Insert two spaces at cursor"},
			{"Esc (editing script)", "Exit editor", "Commit script changes and leave textarea"},
			{"↑, ↓ (Snippets)", "Select snippet", "Highlight code snippet in dropdown picker"},
			{"Enter (Snippets)", "Insert snippet", "Insert snippet code at cursor in script editor"},
			{"Esc (Snippets)", "Close snippets", "Dismiss snippets picker without inserting"},
			{"↑, ↓ (Settings)", "Select setting", "Move through request options (Timeout, TLS, Redirects)"},
			{"Enter (Settings)", "Toggle or edit", "Toggle boolean options (Follow redirects, Verify TLS) or edit numeric and duration settings (e.g. timeout: 30s)"},
		},
	},
	{
		Title:       "Response Pane",
		Description: "Inspecting response payload, headers, timings, test results, and searching.",
		Entries: []KeyEntry{
			{"]", "Next tab", "Cycle forward: Body → Headers → Cookies → Tests → Console → Timeline"},
			{"[", "Previous tab", "Cycle backward through response tabs"},
			{"gr", "Body tab", "Focus the response pane on its Body tab"},
			{"↑, k", "Scroll up", "Scroll response viewer up one line"},
			{"↓, j", "Scroll down", "Scroll response viewer down one line"},
			{"PgUp, Ctrl-u", "Page up", "Scroll response up by half screen"},
			{"PgDn, Ctrl-d", "Page down", "Scroll response down by half screen"},
			{"Home, gg", "Top", "Scroll to the top of the response"},
			{"End, G", "Bottom", "Scroll to the end of the response"},
			{"w", "Toggle wrap", "Toggle soft line wrapping on long response lines"},
			{"r", "Pretty / raw", "Toggle formatted JSON with syntax colors vs raw response text"},
			{"y", "Copy body", "Copy entire response body to system clipboard"},
			{"/", "Search response", "Open search bar to find text across response"},
			{"n", "Next match", "Jump to next occurrence of search term"},
			{"N", "Previous match", "Jump to previous occurrence of search term"},
			{".", "Path filter", "Filter JSON response using gjson path syntax (e.g. users.#.id)"},
			{"Enter", "Toggle folding", "Expand or collapse JSON object or array at cursor"},
			{"h, ←", "Fold JSON node", "Collapse JSON block at cursor"},
			{"l, →", "Unfold JSON node", "Expand JSON block at cursor"},
			{"f", "Filter tests", "On Tests tab: toggle showing failed assertions only"},
			{"u", "Reveal secrets", "Reveal redacted secrets in timeline and header traces"},
			{"Ctrl-S", "Retry request", "Retry sending when transport error occurred"},
			{"Esc", "Cancel send", "Cancel active HTTP request in flight"},
		},
	},
	{
		Title:       "Environment and Variable Editor (Space E)",
		Description: "Managing workspace environments, globals, and collection variables.",
		Entries: []KeyEntry{
			{"[, ]", "Switch tab", "Cycle between Environments, Globals, and Collections tabs"},
			{"1, 2, 3", "Jump to tab", "1: Environments, 2: Globals, 3: Collections"},
			{"↑, ↓, Left, Right", "Select environment", "In list: move selection between environments/collections"},
			{"Enter, Tab", "Focus variables", "Move focus from environment list to variables table"},
			{"n", "New environment", "Create and name a new environment (Environments tab)"},
			{"r, F2", "Rename environment", "Rename the highlighted environment"},
			{"c", "Duplicate environment", "Duplicate environment (safely generates fresh secret refs)"},
			{"d, Delete", "Delete environment", "Delete environment and stored secrets (confirms first)"},
			{"p", "Toggle production", "Flag environment as production (shows warning badge ! prod)"},
			{"a, Insert", "Add variable", "Add a new variable row to current environment or scope"},
			{"Enter", "Edit variable", "Edit variable key, value, or description"},
			{"Space", "Toggle variable", "Enable or disable variable without removing it"},
			{"d, Delete, Backspace", "Delete variable", "Delete variable row (removes secret from Keychain)"},
			{"Alt-↑, Shift-↑", "Move variable up", "Shift variable up in scope table"},
			{"Alt-↓, Shift-↓", "Move variable down", "Shift variable down in scope table"},
			{"s", "Toggle secret", "Convert variable to secret (value moved to macOS Keychain)"},
			{"r", "Reveal secrets", "Temporarily reveal secret variable values in table"},
			{"Ctrl-R (editing)", "Reveal secret", "Toggle secret mask while typing in value field"},
			{"Tab (editing)", "Next field", "Advance between Key, Value, and Description"},
			{"Esc", "Back / Close", "In table: return to list; in list or Globals: close editor"},
			{"Enter, y (dialog)", "Confirm", "Confirm deletion or duplicate prompt"},
			{"Esc, n (dialog)", "Cancel", "Cancel deletion or confirmation dialog"},
		},
	},
	{
		Title:       "Cookie Manager",
		Description: "Inspecting and clearing persistent workspace cookies (.postctl/cookies.json).",
		Entries: []KeyEntry{
			{"↑, ↓", "Move selection", "Select previous or next cookie in list"},
			{"PgUp, PgDn", "Page up / down", "Scroll cookie list by page"},
			{"Home, End", "First / last cookie", "Jump to first or last stored cookie"},
			{"r", "Reveal cookie value", "Toggle display of obscured cookie value"},
			{"d, Delete", "Delete cookie", "Delete selected cookie from persistent jar"},
			{"x", "Clear domain", "Clear all cookies belonging to selected domain (confirms first)"},
			{"X", "Clear all cookies", "Clear entire cookie jar across all domains (confirms first)"},
			{"Enter, y (dialog)", "Confirm clear", "Confirm clearing domain or all cookies"},
			{"Esc, n (dialog)", "Cancel", "Cancel clear confirmation dialog"},
			{"Esc, q", "Close cookies", "Close cookie manager and return to main screen"},
		},
	},
	{
		Title:       "Collection Runner (Space R)",
		Description: "Configuring, executing, and analyzing multi-request runs like newman.",
		Entries: []KeyEntry{
			{"Tab, Shift-Tab", "Navigate fields", "Cycle through Iterations, Data file, Delay, Bail, Persist"},
			{"Space", "Toggle option", "Toggle Bail (stop on failure) and Persist variables options"},
			{"Ctrl-Enter", "Start run", "Begin executing the collection run (needs enhanced keyboard reporting)"},
			{"Enter (last field)", "Start run", "Begin collection run from bottom of setup form"},
			{"Esc (live run)", "Cancel run", "Prompt confirmation to cancel running collection"},
			{"f (live run)", "Failures only", "Filter live progress list to failed requests only"},
			{"Enter (live run)", "Request detail", "Inspect live outcome detail for selected request"},
			{"r (summary)", "Re-run collection", "Execute the entire collection run again"},
			{"R (summary)", "Re-run failed", "Re-run only requests that failed during previous run"},
			{"e (summary)", "Export JSON", "Export complete run summary and test results to JSON file"},
			{"Enter (export)", "Confirm export", "Save JSON report to destination path"},
			{"Tab (detail)", "Next section", "Switch between summary, headers, response, and tests"},
			{"Esc (detail)", "Back", "Return from request detail view to run summary"},
			{"Esc, q", "Close runner", "Exit collection runner screen and return to workspace"},
		},
	},
	{
		Title:       "Request Send History (Space h)",
		Description: "Reviewing, filtering, and re-executing past requests (.postctl/history/).",
		Entries: []KeyEntry{
			{"↑, ↓", "Move selection", "Select previous or next historical request entry"},
			{"PgUp, PgDn", "Page up / down", "Scroll history list by page"},
			{"Home, End", "First / last entry", "Jump to top or bottom of history list"},
			{"/", "Filter history", "Open filter bar to search by request name, path, or URL"},
			{"f", "Status filter", "Cycle HTTP status filter: All → 2xx → 3xx → 4xx → 5xx"},
			{"Enter", "View detail", "Open full detail view for selected historical request"},
			{"o", "Open request", "Jump to and select the original request in the Collections tree"},
			{"s", "Re-send request", "Immediately re-send the historical request"},
			{"c", "Clear history", "Prompt confirmation to delete all send history"},
			{"Tab (detail)", "Next section", "Switch between Request, Response, Headers, and Timeline"},
			{"y, c (detail)", "Copy curl", "Copy historical request as curl command"},
			{"↑, ↓ (detail)", "Scroll content", "Scroll detail body or headers"},
			{"Enter (filter)", "Apply filter", "Apply current filter text and return to list"},
			{"Esc (filter)", "Clear filter", "Dismiss filter bar and clear search query"},
			{"Enter (confirm)", "Confirm clear", "Confirm deleting all request history"},
			{"Esc (confirm)", "Cancel clear", "Cancel clear prompt and keep history"},
			{"Esc, q", "Close history", "Exit history viewer and return to workspace"},
		},
	},
	{
		Title:       "Request switcher (Space f)",
		Description: "Fuzzy-find a request by name, path or URL host. Opens on the most recently used requests.",
		Entries: []KeyEntry{
			{"Type text", "Filter", "Narrow the list; Space and letters are text here"},
			{"↑, ↓, Ctrl-p, Ctrl-n", "Move", "Move the cursor (Ctrl-p is a cursor key here, not the palette)"},
			{"PgUp, PgDn", "Page", "Move a page at a time"},
			{"Enter", "Open", "Reveal the request in the tree and open it"},
			{"Esc", "Close", "Close the switcher without opening anything"},
			{"Ctrl-S, Alt-1/2/3", "Always-keys", "Close the switcher, then send or jump as usual"},
		},
	},
	{
		Title:       "Command Palette (Ctrl-P)",
		Description: "Quick-opening requests, switching environments, and executing actions.",
		Entries: []KeyEntry{
			{"Type text", "Fuzzy search", "Filter actions, environments, and requests across workspace"},
			{"↑, ↓", "Navigate items", "Move selection through matched items"},
			{"Enter", "Execute / Open", "Execute highlighted action, switch to environment, or open request"},
			{"Ctrl-P, Esc", "Close palette", "Dismiss command palette and return to previous focus"},
		},
	},
	{
		Title:       "WebSocket Pane",
		Description: "Connecting, composing, saving, and inspecting WebSocket messages.",
		Entries: []KeyEntry{
			{"Ctrl-S, Ctrl-Enter", "Connect / send", "Connect when idle; send the composer message when connected"},
			{"gu", "Focus URL bar", "Jump to the WebSocket URL input"},
			{"gm, gp, gh, ga, gS", "Go to tab", "Messages, Params, Headers, Auth and Settings tabs"},
			{"[, ]", "Previous / next tab", "Step through the WebSocket tabs"},
			{"Enter (URL bar)", "Connect / disconnect", "Toggle the current WebSocket connection"},
			{"Esc (connecting)", "Cancel connection", "Cancel an in-progress connection attempt"},
			{"←, →, Home, End", "Switch tab", "Navigate Messages, Params, Headers, Auth, and Settings"},
			{"c (tab bar)", "Connect / disconnect", "Toggle the current WebSocket connection"},
			{"Ctrl-T (composer)", "Message format", "Cycle text, JSON, and binary message formats"},
			{"Ctrl-R (composer)", "Format JSON", "Pretty-print the composer as JSON"},
			{"Ctrl-X (composer)", "Save message", "Save the composer as a reusable message"},
			{"Tab, Shift-Tab", "Switch message section", "Move among composer, saved messages, and live log"},
			{"↑, ↓, Home, End (saved)", "Select saved message", "Move through reusable messages"},
			{"Enter (saved)", "Send saved message", "Send the selected reusable message"},
			{"e, r, d, a (saved)", "Manage saved message", "Edit, rename, delete, or save a reusable message"},
			{"Alt-↑, Alt-↓ (saved)", "Reorder saved message", "Move the selected reusable message"},
			{"↑, ↓, PgUp, PgDn, Home, End (log)", "Select log event", "Navigate the live message log"},
			{"Enter (log)", "Open detail", "Inspect the selected message or connection event"},
			{"f, / (log)", "Filter / search", "Cycle direction filters or search the message log"},
			{"c, x, p (log)", "Copy / clear / ping", "Copy a message, clear the log, or send a ping"},
			{"←, → (detail)", "Previous / next message", "Move between message details"},
			{"↑, ↓, PgUp, PgDn, Home, End (detail)", "Scroll detail", "Scroll the selected message detail"},
			{"c (detail)", "Copy message", "Copy the selected message"},
			{"Esc", "Back", "Return to the previous WebSocket section"},
		},
	},
}

// GenerateMarkdown renders the full catalog as a Markdown document.
func GenerateMarkdown() string {
	var b bytes.Buffer
	b.WriteString("# Keyboard Shortcut Reference\n\n")
	b.WriteString("`postctl` is designed for fast, keyboard-first navigation without requiring a mouse.\n")
	b.WriteString("Keys follow plan §0 U1-U4: vim motions in lists and panes, actions behind the `Space` leader (press it and wait 300 ms to see the choices), `g` and `[ ]` prefixes for sections. Arrow keys, Enter and Escape keep working everywhere, so nobody is ever stuck. Text fields insert what you type, including Space, `g`, `q` and `:`; modal editing inside text fields is a separate workstream and is not advertised here until it ships.\n\n")
	b.WriteString("## Quick Reference Cheat Sheet\n\n")
	b.WriteString("| Key | Action |\n")
	b.WriteString("| --- | --- |\n")
	b.WriteString("| `Ctrl-S` / `Ctrl-Enter` | Send active request |\n")
	b.WriteString("| `Space` | Leader: wait to see the actions (`Space e` environment, `Space a` new, `Space q` quit) |\n")
	b.WriteString("| `Ctrl-P` / `:` | Command palette / command line |\n")
	b.WriteString("| `?` | Context help overlay |\n")
	b.WriteString("| `Tab` / `Shift-Tab` | Next / previous pane |\n")
	b.WriteString("| `1` / `2` / `3` | Focus Collections / Request / Response |\n")
	b.WriteString("| `Ctrl-h` `Ctrl-j` `Ctrl-k` `Ctrl-l` | Move to the neighbouring pane |\n")
	b.WriteString("| `g` then `u` `p` `h` `b` `r` ... | Go to the URL, Params, Headers, Body, Response, ... |\n")
	b.WriteString("| `[` / `]` | Previous / next section tab |\n")
	b.WriteString("| `Ctrl-o` | Back to the previous request |\n")
	b.WriteString("| `Esc` | Back, close, cancel a send |\n")
	b.WriteString("| `Ctrl-C` twice / `q` | Quit postctl |\n\n")

	for _, sec := range KeyCatalog {
		b.WriteString("## " + sec.Title + "\n\n")
		if sec.Description != "" {
			b.WriteString(sec.Description + "\n\n")
		}
		b.WriteString("| Key | Action | Description |\n")
		b.WriteString("| --- | --- | --- |\n")
		for _, e := range sec.Entries {
			keyStr := "`" + strings.ReplaceAll(e.Key, "`", "\\`") + "`"
			fmt.Fprintf(&b, "| %s | %s | %s |\n", keyStr, e.Action, e.Description)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func main() {
	outPath := flag.String("out", "docs/keys.md", "output path for keys reference")
	stdout := flag.Bool("stdout", false, "write to stdout instead of file")
	flag.Parse()

	content := GenerateMarkdown()
	if *stdout {
		fmt.Print(content)
		return
	}

	target := *outPath
	if !filepath.IsAbs(target) {
		// If running from repo root or worktree
		target = filepath.Clean(target)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", target, err)
		os.Exit(1)
	}
	fmt.Printf("Wrote key reference to %s (%d bytes)\n", target, len(content))
}
