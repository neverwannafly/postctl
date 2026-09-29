# Keyboard Shortcut Reference

`postctl` is designed for fast, keyboard-first navigation without requiring a mouse.
Shortcuts use standard navigation and editing keys (arrows, Tab, Enter, Esc). In accordance with plan §0 (Q11), there are no modal vim bindings.

## Quick Reference Cheat Sheet

| Key | Action |
| --- | --- |
| `Ctrl-S` / `Ctrl-Enter` | Send active request |
| `Ctrl-P` | Command palette |
| `?` | Context help overlay |
| `Tab` / `Shift-Tab` | Next / previous pane |
| `1` / `2` / `3` | Focus Collections / Request / Response |
| `Ctrl-B` | Show / hide Collections sidebar |
| `E` | Switch active environment |
| `Ctrl-E` | Open Environment & Variable Editor |
| `R` | Run collection or folder |
| `H` | Open request send history |
| `q` | Quit postctl |

## Global Shortcuts

Available everywhere unless a modal text input or editor captures input.

| Key | Action | Description |
| --- | --- | --- |
| `?` | Help | Open the keyboard shortcut and context help overlay |
| `Ctrl-P` | Command palette | Open the fuzzy command palette (press again to close) |
| `E` | Environments | Open the active environment switcher overlay |
| `Ctrl-E` | Edit environments | Open the full-screen environment and variable editor |
| `q` | Quit | Quit postctl (prompts if unsaved changes are pending) |
| `Ctrl-C` | Force quit | Immediately terminate postctl |
| `Tab` | Next pane | Cycle focus forward: Collections → Request → Response |
| `Shift-Tab` | Previous pane | Cycle focus backward: Response → Request → Collections |
| `Ctrl-B` | Toggle sidebar | Show or hide the Collections sidebar |
| `/` | Filter requests | Focus the sidebar and start filtering requests by name/path |
| `Ctrl-R` | Reload workspace | Reload workspace YAML and script files from disk |
| `H` | History | Open the request send history screen |
| `1, Alt-1` | Collections pane | Jump directly to the Collections sidebar |
| `2, Alt-2` | Request pane | Jump directly to the Request editor pane |
| `3, Alt-3` | Response pane | Jump directly to the Response viewer pane |
| `Esc` | Back / cancel | Close open overlays, dismiss dialogs, or return focus |

## Collections Sidebar (Tree)

Navigating, organizing, and executing collections, folders, and requests.

| Key | Action | Description |
| --- | --- | --- |
| `↑, Up` | Move up | Move selection to the previous item in the tree |
| `↓, Down` | Move down | Move selection to the next item in the tree |
| `PgUp, PageUp` | Page up | Scroll selection up one page |
| `PgDn, PageDown` | Page down | Scroll selection down one page |
| `Home` | First row | Jump to the very first item in the tree |
| `End` | Last row | Jump to the very last item in the tree |
| `←, Left` | Fold folder | Collapse expanded folder, or jump to parent container |
| `→, Right` | Unfold folder | Expand collapsed folder to reveal children |
| `Enter` | Open / toggle | Open request in editor, or expand/collapse folder |
| `Space` | Toggle folder | Fold or unfold the selected folder |
| `n` | New item | Prompt to create a new Request, Folder, or Collection |
| `r` | Rename | Rename the selected collection, folder, or request |
| `D` | Duplicate | Duplicate selected request or folder (assigns fresh IDs) |
| `m` | Move | Open folder picker to move item to another container |
| `d` | Delete | Delete item and associated files (confirms before deletion) |
| `Alt-↑, Alt-Up` | Move up in folder | Reorder item up among siblings in its folder |
| `Alt-↓, Alt-Down` | Move down in folder | Reorder item down among siblings in its folder |
| `R` | Run collection | Open collection runner setup for selected collection or folder |
| `/` | Filter requests | Activate fuzzy filter across collection requests |
| `Esc` | Clear filter / Close | Clear filter text; close overlay in single-pane mode |

## Request Pane — General & URL Bar

Editing request target, method, and global pane shortcuts.

| Key | Action | Description |
| --- | --- | --- |
| `Ctrl-S, Ctrl-Enter` | Send request | Send the active request immediately (Ctrl-S always works) |
| `Ctrl-Z` | Undo | Undo the previous edit in the request editor |
| `Ctrl-Y` | Redo | Redo the previously undone edit |
| `e, Ctrl-O` | Open in $EDITOR | Open the current request tab in your external $EDITOR when not typing |
| `Ctrl-L` | Focus URL bar | Jump cursor directly to the URL input line |
| `Esc` | Cancel send / OAuth | Cancel in-flight HTTP request or pending OAuth2 browser login |
| `Enter (URL bar)` | Send request | Send request immediately while editing URL |
| `Shift-Tab (URL bar)` | Method selector | Open the HTTP method dropdown selector |
| `Tab, Down (URL bar)` | Focus tabs | Move focus down from URL bar to the request tab row |
| `Esc (URL bar)` | Focus tabs | Move focus from URL bar to the request tab row |
| `←, →, ↑, ↓ (Method)` | Change method | Cycle through GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS |
| `Enter, Tab (Method)` | Choose method | Select the highlighted HTTP method |
| `Esc (Method)` | Cancel selector | Dismiss the method selector without changes |
| `A–Z (Method)` | Typeahead jump | Jump to the next method starting with typed letter |

## Request Pane — Tab Navigation & Curl Export

Navigating request sections and generating curl commands.

| Key | Action | Description |
| --- | --- | --- |
| `←, →` | Switch tab | Cycle between Params, Auth, Headers, Body, Pre, Tests, Settings |
| `Home, End` | First / last tab | Jump to first tab (Params) or last tab (Settings) |
| `↑` | Focus URL | Move focus back up to the URL address bar |
| `↓, Enter` | Focus tab content | Move focus into the active tab's fields or editor |
| `m` | Method selector | Open the HTTP method dropdown selector |
| `y` | Copy as curl | Copy shell-quoted curl command to clipboard (secrets masked) |
| `Y, Shift-Y` | Copy curl (with secrets) | Copy curl command with revealed secret variable values |

## Request Pane — Key-Value Tables (Params, Headers, Form)

Managing query parameters, headers, URL-encoded fields, and form-data parts.

| Key | Action | Description |
| --- | --- | --- |
| `↑, ↓` | Move row | Move selection between table rows |
| `PgUp, PgDn` | Page up / down | Scroll table up or down by a page |
| `Home, End` | First / last row | Jump to first or last table row |
| `a, Insert` | Add row | Insert a new row at the bottom of the table |
| `Enter` | Edit field | Edit active cell (or add new row if table is empty) |
| `Space` | Toggle enabled | Enable or disable current row without deleting it |
| `d, Delete, Backspace` | Delete row | Delete the selected row |
| `Alt-↑, Shift-↑` | Move row up | Shift current row up in evaluation order |
| `Alt-↓, Shift-↓` | Move row down | Shift current row down in evaluation order |
| `b` | Bulk edit | Open raw multiline text area to paste key:value pairs |
| `t` | Toggle Text / File | Switch form-data part between text value and file upload |
| `Tab (editing)` | Next field | Advance editing focus: Key → Value → Description |
| `Shift-Tab (editing)` | Previous field | Move editing focus backward |
| `Enter, Esc (editing)` | Done editing | Commit changes and return to row selection |
| `Esc (bulk edit)` | Apply bulk edit | Parse multiline text into table rows and close |

## Request Pane — Body Tab

Configuring request payload across raw, form, binary, and GraphQL modes.

| Key | Action | Description |
| --- | --- | --- |
| `←, → (Type row)` | Body type | Cycle: none, raw, form-urlencoded, form-data, file, graphql |
| `Enter, Space (Type)` | Next type | Advance to next body type |
| `←, → (Lang row)` | Language | Cycle raw syntax: JSON, XML, HTML, Text, JavaScript |
| `Enter, i` | Edit body | Focus inline textarea to edit raw or GraphQL body |
| `f` | Format JSON | Pretty-print and format JSON body (preserves {{variables}}) |
| `Ctrl-O` | Open in $EDITOR | Edit full payload in external editor and reload on save |
| `Tab (GraphQL)` | Query / Vars | Switch editor focus between GraphQL Query and Variables |
| `Esc` | Exit editor | Leave body textarea and return to field navigation |

## Request Pane — Auth Tab

Configuring authentication schemes and OAuth 2.0 token acquisition.

| Key | Action | Description |
| --- | --- | --- |
| `←, → (Type row)` | Auth type | Cycle: inherit, none, bearer, basic, apikey, digest, oauth2, awsv4, hawk, ntlm |
| `Enter, Space (Type)` | Next auth type | Advance to next authentication scheme |
| `Enter` | Edit field | Focus form field for editing |
| `Tab, Shift-Tab` | Navigate fields | Move focus forward or backward through auth inputs |
| `r` | Reveal secrets | Toggle visibility of masked secret inputs in the form |
| `Ctrl-R` | Reveal while typing | Toggle secret masking while actively typing in input |
| `g` | Get OAuth token | Initiate browser login for OAuth2 with local loopback listener |
| `c` | Copy sign-in link | Copy OAuth authorization URL to clipboard while waiting |
| `Esc` | Cancel OAuth / Done | Cancel waiting OAuth login or commit field editing |

## Request Pane — Scripts & Settings Tabs

Authoring pre-request and test scripts, code snippets, and request options.

| Key | Action | Description |
| --- | --- | --- |
| `Enter, i` | Edit script | Focus script editor textarea |
| `s, Ctrl-G` | Snippets picker | Open dropdown of Chai assertions and pm.* snippet templates |
| `Ctrl-O` | Open in $EDITOR | Edit script in external editor (saved as .pre.js or .test.js) |
| `Tab (editing script)` | Indent | Insert two spaces at cursor |
| `Esc (editing script)` | Exit editor | Commit script changes and leave textarea |
| `↑, ↓ (Snippets)` | Select snippet | Highlight code snippet in dropdown picker |
| `Enter (Snippets)` | Insert snippet | Insert snippet code at cursor in script editor |
| `Esc (Snippets)` | Close snippets | Dismiss snippets picker without inserting |
| `↑, ↓ (Settings)` | Select setting | Move through request options (Timeout, TLS, Redirects) |
| `Space (Settings)` | Toggle boolean | Toggle boolean options (Follow redirects, Verify TLS) |
| `Enter (Settings)` | Edit value | Edit numeric or duration settings (e.g. timeout: 30s) |

## Response Pane

Inspecting response payload, headers, timings, test results, and searching.

| Key | Action | Description |
| --- | --- | --- |
| `Tab, →` | Next tab | Cycle forward: Body → Headers → Tests → Timeline → Raw |
| `Shift-Tab, ←` | Previous tab | Cycle backward through response tabs |
| `1, 2, 3, 4, 5, 6` | Jump to tab | Directly activate tab 1–6 |
| `↑, k` | Scroll up | Scroll response viewer up one line |
| `↓, j` | Scroll down | Scroll response viewer down one line |
| `PgUp` | Page up | Scroll response up by half screen |
| `PgDn` | Page down | Scroll response down by half screen |
| `w` | Toggle wrap | Toggle soft line wrapping on long response lines |
| `r` | Pretty / raw | Toggle formatted JSON with syntax colors vs raw response text |
| `y` | Copy body | Copy entire response body to system clipboard |
| `/` | Search response | Open search bar to find text across response |
| `n` | Next match | Jump to next occurrence of search term |
| `N` | Previous match | Jump to previous occurrence of search term |
| `.` | Path filter | Filter JSON response using gjson path syntax (e.g. users.#.id) |
| `Space` | Toggle folding | Expand or collapse JSON object or array at cursor |
| `h, ←` | Fold JSON node | Collapse JSON block at cursor |
| `l, →` | Unfold JSON node | Expand JSON block at cursor |
| `f` | Filter tests | On Tests tab: toggle showing failed assertions only |
| `u` | Reveal secrets | Reveal redacted secrets in timeline and header traces |
| `Ctrl-S` | Retry request | Retry sending when transport error occurred |
| `Esc` | Cancel send | Cancel active HTTP request in flight |

## Environment and Variable Editor (Ctrl-E)

Managing workspace environments, globals, and collection variables.

| Key | Action | Description |
| --- | --- | --- |
| `[, ]` | Switch tab | Cycle between Environments, Globals, and Collections tabs |
| `1, 2, 3` | Jump to tab | 1: Environments, 2: Globals, 3: Collections |
| `↑, ↓, Left, Right` | Select environment | In list: move selection between environments/collections |
| `Enter, Tab` | Focus variables | Move focus from environment list to variables table |
| `n` | New environment | Create and name a new environment (Environments tab) |
| `r, F2` | Rename environment | Rename the highlighted environment |
| `c` | Duplicate environment | Duplicate environment (safely generates fresh secret refs) |
| `d, Delete` | Delete environment | Delete environment and stored secrets (confirms first) |
| `p` | Toggle production | Flag environment as production (shows warning badge ! prod) |
| `a, Insert` | Add variable | Add a new variable row to current environment or scope |
| `Enter` | Edit variable | Edit variable key, value, or description |
| `Space` | Toggle variable | Enable or disable variable without removing it |
| `d, Delete, Backspace` | Delete variable | Delete variable row (removes secret from Keychain) |
| `Alt-↑, Shift-↑` | Move variable up | Shift variable up in scope table |
| `Alt-↓, Shift-↓` | Move variable down | Shift variable down in scope table |
| `s` | Toggle secret | Convert variable to secret (value moved to macOS Keychain) |
| `r` | Reveal secrets | Temporarily reveal secret variable values in table |
| `Ctrl-R (editing)` | Reveal secret | Toggle secret mask while typing in value field |
| `Tab (editing)` | Next field | Advance between Key, Value, and Description |
| `Esc` | Back / Close | In table: return to list; in list or Globals: close editor |
| `Enter, y (dialog)` | Confirm | Confirm deletion or duplicate prompt |
| `Esc, n (dialog)` | Cancel | Cancel deletion or confirmation dialog |

## Cookie Manager

Inspecting and clearing persistent workspace cookies (.postctl/cookies.json).

| Key | Action | Description |
| --- | --- | --- |
| `↑, ↓` | Move selection | Select previous or next cookie in list |
| `PgUp, PgDn` | Page up / down | Scroll cookie list by page |
| `Home, End` | First / last cookie | Jump to first or last stored cookie |
| `r` | Reveal cookie value | Toggle display of obscured cookie value |
| `d, Delete` | Delete cookie | Delete selected cookie from persistent jar |
| `x` | Clear domain | Clear all cookies belonging to selected domain (confirms first) |
| `X` | Clear all cookies | Clear entire cookie jar across all domains (confirms first) |
| `Enter, y (dialog)` | Confirm clear | Confirm clearing domain or all cookies |
| `Esc, n (dialog)` | Cancel | Cancel clear confirmation dialog |
| `Esc, q` | Close cookies | Close cookie manager and return to main screen |

## Collection Runner (R)

Configuring, executing, and analyzing multi-request runs like newman.

| Key | Action | Description |
| --- | --- | --- |
| `Tab, Shift-Tab` | Navigate fields | Cycle through Iterations, Data file, Delay, Bail, Persist |
| `Space` | Toggle option | Toggle Bail (stop on failure) and Persist variables options |
| `Ctrl-Enter` | Start run | Begin executing the collection run |
| `Enter (last field)` | Start run | Begin collection run from bottom of setup form |
| `Esc (live run)` | Cancel run | Prompt confirmation to cancel running collection |
| `f (live run)` | Failures only | Filter live progress list to failed requests only |
| `Enter (live run)` | Request detail | Inspect live outcome detail for selected request |
| `r (summary)` | Re-run collection | Execute the entire collection run again |
| `R (summary)` | Re-run failed | Re-run only requests that failed during previous run |
| `e (summary)` | Export JSON | Export complete run summary and test results to JSON file |
| `Enter (export)` | Confirm export | Save JSON report to destination path |
| `Tab (detail)` | Next section | Switch between summary, headers, response, and tests |
| `Esc (detail)` | Back | Return from request detail view to run summary |
| `Esc, q` | Close runner | Exit collection runner screen and return to workspace |

## Request Send History (H)

Reviewing, filtering, and re-executing past requests (.postctl/history/).

| Key | Action | Description |
| --- | --- | --- |
| `↑, ↓` | Move selection | Select previous or next historical request entry |
| `PgUp, PgDn` | Page up / down | Scroll history list by page |
| `Home, End` | First / last entry | Jump to top or bottom of history list |
| `/` | Filter history | Open filter bar to search by request name, path, or URL |
| `f` | Status filter | Cycle HTTP status filter: All → 2xx → 3xx → 4xx → 5xx |
| `Enter` | View detail | Open full detail view for selected historical request |
| `o` | Open request | Jump to and select the original request in the Collections tree |
| `s` | Re-send request | Immediately re-send the historical request |
| `c` | Clear history | Prompt confirmation to delete all send history |
| `Tab (detail)` | Next section | Switch between Request, Response, Headers, and Timeline |
| `y, c (detail)` | Copy curl | Copy historical request as curl command |
| `↑, ↓ (detail)` | Scroll content | Scroll detail body or headers |
| `Enter (filter)` | Apply filter | Apply current filter text and return to list |
| `Esc (filter)` | Clear filter | Dismiss filter bar and clear search query |
| `Enter (confirm)` | Confirm clear | Confirm deleting all request history |
| `Esc (confirm)` | Cancel clear | Cancel clear prompt and keep history |
| `Esc, q` | Close history | Exit history viewer and return to workspace |

## Command Palette (Ctrl-P)

Quick-opening requests, switching environments, and executing actions.

| Key | Action | Description |
| --- | --- | --- |
| `Type text` | Fuzzy search | Filter actions, environments, and requests across workspace |
| `↑, ↓` | Navigate items | Move selection through matched items |
| `Enter` | Execute / Open | Execute highlighted action, switch to environment, or open request |
| `Ctrl-P, Esc` | Close palette | Dismiss command palette and return to previous focus |

## WebSocket Pane

Connecting, composing, saving, and inspecting WebSocket messages.

| Key | Action | Description |
| --- | --- | --- |
| `Ctrl-S, Ctrl-Enter` | Connect / send | Connect when idle; send the composer message when connected |
| `Ctrl-L` | Focus URL bar | Jump to the WebSocket URL input |
| `Enter (URL bar)` | Connect / disconnect | Toggle the current WebSocket connection |
| `Esc (connecting)` | Cancel connection | Cancel an in-progress connection attempt |
| `←, →, Home, End` | Switch tab | Navigate Messages, Params, Headers, Auth, and Settings |
| `c (tab bar)` | Connect / disconnect | Toggle the current WebSocket connection |
| `Ctrl-T (composer)` | Message format | Cycle text, JSON, and binary message formats |
| `Ctrl-R (composer)` | Format JSON | Pretty-print the composer as JSON |
| `Ctrl-X (composer)` | Save message | Save the composer as a reusable message |
| `Tab, Shift-Tab` | Switch message section | Move among composer, saved messages, and live log |
| `↑, ↓, Home, End (saved)` | Select saved message | Move through reusable messages |
| `Enter (saved)` | Send saved message | Send the selected reusable message |
| `e, r, d, a (saved)` | Manage saved message | Edit, rename, delete, or save a reusable message |
| `Alt-↑, Alt-↓ (saved)` | Reorder saved message | Move the selected reusable message |
| `↑, ↓, PgUp, PgDn, Home, End (log)` | Select log event | Navigate the live message log |
| `Enter (log)` | Open detail | Inspect the selected message or connection event |
| `f, / (log)` | Filter / search | Cycle direction filters or search the message log |
| `c, x, p (log)` | Copy / clear / ping | Copy a message, clear the log, or send a ping |
| `←, → (detail)` | Previous / next message | Move between message details |
| `↑, ↓, PgUp, PgDn, Home, End (detail)` | Scroll detail | Scroll the selected message detail |
| `c (detail)` | Copy message | Copy the selected message |
| `Esc` | Back | Return to the previous WebSocket section |

