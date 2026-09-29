# postctl — design language

## Design intent

The interface should feel like a calm, precise instrument for developers working with APIs. It should communicate the active environment, what is about to be sent, and what came back before it adds decoration.

The visual theme is named **Clear Signal**. It uses a restrained teal accent, neutral terminal-native surfaces, and separate semantic colors for success, warning, error, and information. The theme must work in dark and light terminals, in reduced color, and in a text-only reading mode.

This is a terminal design language, not a web page translated into boxes. A terminal gives us fixed cells, the user's chosen font and background, varied color support, and limited screen area. Layouts must adapt to these constraints rather than assume a particular terminal emulator.

The theme tokens below are implemented in `internal/ui/theme.go`; the two must stay in step (a test checks the token count and the contrast claims).

## Principles

1. **Make the next step obvious.** Every state has one primary action and a short explanation.
2. **Keep the active environment visible.** The header always shows which environment a request will run against. An environment flagged `production` is shown with a warning badge.
3. **Use color to reinforce words.** Never use color as the only way to identify status, selection, an HTTP method, or an error.
4. **Use space and alignment before decoration.** A small set of consistent spacing and type treatments creates hierarchy without heavy borders.
5. **Show progress as real steps.** Describe what is happening: `… Running pre-request script`, `… Waiting for response 1.2s`, `… Running tests`. Do not fake progress or make a spinner the only explanation.
6. **Respect the terminal.** Preserve user background where practical, degrade to ANSI colors, honor `NO_COLOR`, resize cleanly, and keep the main flow usable with keyboard only.
7. **Keep secrets invisible.** Secret variable values, tokens, and credentials never appear in visible content, logs, history, or diagnostics unless the user explicitly reveals them.

## Theme tokens

### Color palettes

Hex values are source palette tokens. Lip Gloss/Bubble Tea selects the dark or light semantic value after background detection and downsamples for the terminal's color profile. Keep the application background transparent by default so the user's terminal theme remains visible. Use explicit background fills only for small selection or status regions, and always provide a readable foreground.

| Token | Dark terminal | Light terminal | Use |
| --- | --- | --- | --- |
| `text.primary` | `#E6EDF3` | `#1F2933` | Main labels, headings, values |
| `text.secondary` | `#9EABB8` | `#526170` | Supporting description, metadata |
| `text.quiet` | `#7F8C99` | `#667482` | Timestamps and low-priority hints; never essential instructions |
| `accent.primary` | `#53D6C3` | `#006A70` | Current selection, primary action label, active navigation |
| `status.success` | `#47D18C` | `#16794F` | Passed, ready, healthy |
| `status.warning` | `#FFD166` | `#8B5E00` | Attention needed, degraded but recoverable |
| `status.danger` | `#FF7583` | `#B42332` | Failed, destructive confirmation |
| `status.info` | `#7CC4FF` | `#175CD3` | Informational state, neutral progress |
| `border.subtle` | `#394652` | `#D7DEE5` | Section separator, card edge when needed |
| `surface.selected` | `#1E343A` | `#E5F3F2` | Selected row/action background when supported |

The palette is intentionally small. Components consume semantic tokens; they must not introduce one-off RGB values. Status meaning always includes a text label or recognizable icon as well as color, for example `● Passed`, `! Slow`, `× Failed`.

The accent, status, and body colors are designed to exceed the WCAG 2.2 normal-text 4.5:1 contrast target against representative dark (`#101820`) and light (`#F7F9FB`) backgrounds (verified in `internal/ui/theme_test.go`). This is a palette check, not a claim of WCAG conformance: a terminal can use an arbitrary user background. Verify actual rendering in target terminals and include monochrome fallbacks. See [WCAG 2.2 contrast minimum](https://www.w3.org/TR/wcag/#contrast-minimum).

### Derived tokens

These add API-specific roles. They reuse the palette above, so they introduce no new colors.

**HTTP methods.** Always show the method *text* (`GET`, `POST`, ...) in a fixed-width column; color reinforces it.

| Token | Maps to | Methods |
| --- | --- | --- |
| `method.get` | `status.info` | GET |
| `method.post` | `status.success` | POST |
| `method.put` | `status.warning` | PUT |
| `method.patch` | `status.warning` | PATCH |
| `method.delete` | `status.danger` | DELETE (abbreviated `DEL` in narrow tree rows) |
| `method.other` | `text.secondary` | HEAD, OPTIONS, custom methods |

**Response status classes.** Always show the numeric code and reason (`201 Created`), never only color.

| Token | Maps to | Codes |
| --- | --- | --- |
| `status.2xx` | `status.success` | 200–299 |
| `status.3xx` | `status.info` | 300–399 (and 1xx) |
| `status.4xx` | `status.warning` | 400–499 |
| `status.5xx` | `status.danger` | 500–599, and transport failures (no response) |

**Syntax (JSON and highlighted bodies).**

| Token | Maps to |
| --- | --- |
| `syntax.key` | `status.info` |
| `syntax.string` | `status.success` |
| `syntax.number` | `status.warning` |
| `syntax.bool` | `accent.primary` |
| `syntax.null` | `text.quiet` |

Syntax colors are decoration: the body must remain fully readable in no-color mode. Highlighters (Chroma) are mapped onto these tokens, never used with their own styles.

**Production environment badge.** `env.production` maps to `status.warning`. An environment with `production: true` shows `! prod` (ASCII: `! prod`) in the header in warning color and bold. It is a warning only: postctl does not ask for confirmation before sending. Non-production environments show `● staging` in `text.secondary`.

### Color behavior

- Request terminal background information through Bubble Tea and derive styles from the response. Avoid hidden global I/O during theme initialization.
- Let Bubble Tea v2 downsample colors. Components ask for semantic roles, never guess whether the terminal supports true color.
- In no-color mode, keep hierarchy through labels, boldness, prefixes, and spacing. Honor `NO_COLOR` and expose an explicit accessible mode (`--accessible`); `--ascii` swaps Unicode glyphs for ASCII.
- Do not paint a full-screen background by default. Terminals own their surface; large background blocks can clash with user themes or create visual noise.
- Keep selected and focused states distinct in both color and shape/prefix (for example, `›` plus accent color).

## Type and text hierarchy

The application cannot choose a terminal font or font size. “Typography” here means using the terminal's current font with consistent text treatments.

| Role | Treatment | Example |
| --- | --- | --- |
| Product mark | Plain wordmark, bold; no large ASCII art | `postctl` |
| Page title | Bold, one line when possible | `Import Postman data` |
| Section title | Bold or accent, sentence case | `Collections`, `Headers`, `Tests` |
| Body | Regular, high-contrast | `No requests yet.` |
| Primary action | Accent plus explicit verb | `[ Ctrl-Enter ] Send` |
| Metadata | Secondary, compact, labeled | `Environment  staging` |
| Key hint | Secondary text with key in bold | `? Help   Ctrl-P Palette   q Quit` |
| Error detail | Danger label followed by plain explanation and recovery | `× Could not connect to api.test:443. Check the URL or your network, then send again.` |

Use sentence case. Prefer short, literal phrases and concrete verbs. Do not put important content in faint, italic, blinking, or all-caps text. Avoid relying on emoji rendering, which differs across terminals and may occupy two cells.

## Spacing and layout

Spacing values are terminal cells, not pixels.

| Token | Value | Use |
| --- | --- | --- |
| `space.xs` | 1 cell | Separate icon and label, compact inline metadata |
| `space.sm` | 2 cells | Gap between related controls or rows |
| `space.md` | 3 cells | Gap between sections |
| `space.lg` | 4 cells | Screen edge inset or major grouping |

### Responsive layout

An API client is inherently multi-pane, so postctl adapts to the terminal width instead of forcing one column. Layout is chosen from the current width on every resize (`ui.LayoutFor`).

| Width | Layout |
| --- | --- |
| ≥ 140 columns | **Three panes:** `Sidebar │ Request │ Response` |
| 100–139 columns | **Two panes:** `Sidebar │ Request and Response` stacked or tabbed (`Tab` switches) |
| < 100 columns | **Single pane** with focus switching (`Tab`, or `1` `2` `3`); the sidebar is an overlay opened with `Ctrl-B` or `/` |

Layout rules:

- The target comfortable size is 140 by 40; the product must remain usable at 80 by 24.
- At < 100 columns keep the main content to one pane; never squeeze labels, URLs, or error text into side-by-side panels. Remove secondary metadata before truncating the method, URL, or environment label.
- Keep a consistent left edge for title, main content, and messages. Wrap explanatory text and let content scroll vertically.
- Prefer whitespace and a single subtle rule (`border.subtle`) over nested boxes. Reserve borders for an explicit selected region, a modal, or a separate status strip.
- The header carries product name, workspace, and the environment badge. The main area carries the current task. The footer carries only relevant key hints.
- Never put long URLs or names in a column that forces the whole view wider. Wrap, shorten with an explicit ellipsis, or show the full value in a detail view. Pane widths follow content priority: URL and method before headers, headers before descriptions.
- Focus is shown by an accent `›` marker and accent title on the focused pane, not by color alone.

## Shared components

### Header

Show `postctl`, the workspace name, and the persistent environment badge (`● staging`, or `! prod` for production environments). When no environment is active show `env  none` in `text.quiet`. The header must not display environment state solely through a colored dot.

### Method badge

Fixed-width method text (`GET `, `POST`, `PUT `, `PATCH`, `DEL `) in the method token with bold; the URL or name follows. In the tree the badge precedes the request name; in the request bar it precedes the URL.

### Primary action

Use a clear verb and show the key binding: `Ctrl-Enter Send`. One action is visually primary per screen. `Enter` on a request in the tree opens it; `Ctrl-Enter` or `Ctrl-S` sends.

### Status line

Each status combines a stable symbol and text. Examples: `● Ready`, `… Running pre-request script`, `… Waiting for response 1.2s`, `! Slow response`, `× Request failed`. Use a spinner only for active work and pair it with explanatory text.

### Response status

`201 Created  184 ms  1.2 KB` — the code in its status-class token (bold), then time and size in `text.secondary`. Test results summarize as `Tests 3/3` (success) or `Tests 2/3` (danger), with a `✓` / `×` per test in the Tests tab.

### Key-value table

Headers, query params, urlencoded and form fields share one table: enabled marker, key, value, description. A disabled row shows `[ ]` (ASCII and Unicode) and secondary text; an enabled row shows `[x]`. `Space` toggles, `n` adds, `d` deletes. Secret values render as `••••` (ASCII `****`) and reveal only while the reveal key is held.

### Modal

Confirmations (delete, discard changes) use a bordered modal naming the affected item. Destructive modals use the danger border, focus `Cancel` by default, and act only after the user moves to the destructive choice (or presses `y`) and confirms.

### Empty state

Explain whether there is no data yet or it has not loaded, and give the next action. An empty tree is not a failure (see the first-run screen).

### Error state

Use this order: what happened, what remains intact (when useful), next action. Keep raw transport details available in a secondary detail view. Never expose tokens, secret values, or credential-bearing URLs in error text.

### Confirmation

Use a confirmation prompt only for a consequential action, such as deleting a request, folder, collection, or environment. Name the item, make the safe choice the default, and support Escape to cancel. Sending a request never needs confirmation, including in a production environment (a warning badge is enough).

## Screen direction

These are content sketches, not fixed pixel layouts.

### Main workspace (three panes, ≥ 140 columns)

```text
postctl  payments-api                                                    env  ! prod
────────────────────────────────────────────────────────────────────────────────────────────
Collections          │ POST  {{baseUrl}}/v1/charges                    │ 201 Created  184 ms  1.2 KB
› ▾ Payments         │ Params  Auth  Headers(3)  Body  Pre  Tests       │ Body  Headers(9)  Tests 3/3
    GET  List charges│ ─────────────────────────────────────────────── │ ───────────────────────────
  › POST Create      │ {                                               │ {
    DEL  Refund      │   "amount": {{amount}},                         │   "id": "ch_8f2c",
  ▸ Customers        │   "currency": "usd"                             │   "status": "succeeded"
                     │ }                                               │ }
────────────────────────────────────────────────────────────────────────────────────────────
Ctrl-Enter Send  e Edit in $EDITOR  E Environments  / Filter  Ctrl-P Palette  ? Help
```

The tree marks the selection with `›` and accent; folders show `▾` (open) or `▸` (closed) (ASCII `v` / `>`). Unsaved edits show `●` after the request name; autosave clears it. Only the focused pane's key hints appear in the footer.

### Two panes (100–139 columns)

```text
postctl  payments-api                                       env  ● staging
─────────────────────────────────────────────────────────────────────────────────────────
Collections          │ POST  {{baseUrl}}/v1/charges
› ▾ Payments         │ Params  Auth  Headers(3)  Body  Pre  Tests        Response ▸
    GET  List charges│ ───────────────────────────────────────────────────────────────────
  › POST Create      │ {
    DEL  Refund      │   "amount": {{amount}},
  ▸ Customers        │ }
─────────────────────────────────────────────────────────────────────────────────────────
Ctrl-Enter Send  Tab Request/Response  E Environments  Ctrl-P Palette  ? Help
```

`Tab` swaps the right pane between the request and the response (which is focused automatically after a send).

### Single pane with overlays (< 100 columns)

```text
postctl  payments-api          ● staging
──────────────────────────────────────────
POST  {{baseUrl}}/v1/charges
Params  Auth  Headers(3)  Body  Pre  Tests
──────────────────────────────────────────
{
  "amount": {{amount}},
  "currency": "usd"
}
──────────────────────────────────────────
Ctrl-Enter Send  Tab Response  Ctrl-B Tree
```

`Ctrl-B` opens the tree as an overlay; `Tab` or `1` `2` `3` switch between Request, Response, and Console/Tests.

### Request editor

Tabs: `Params  Auth  Headers  Body  Pre-request  Tests  Settings`. The active tab is bold with an accent underline rule; tabs with content show a count or a `●` (for example `Headers(3)`, `Pre ●`). The request bar (`POST  {{baseUrl}}/v1/charges`) is always visible above the tabs and edits in place; `Ctrl-V` in the URL bar detects a pasted curl command. Body and script tabs edit inline; `e` opens the content in `$EDITOR` and reloads on return.

### Response viewer

Tabs: `Body  Headers  Cookies  Tests  Console  Timeline`. Body offers pretty / raw / text with syntax tokens, `/` to search, and a path filter. While a request is running the pane shows real steps, not a spinner alone:

```text
… Running pre-request script
… Waiting for response 1.2s
```

Failures use the error-state order: `× Could not connect to api.test:443.  Your request is unchanged.  Send again with Ctrl-Enter, or check the URL.`

### Environment switcher

```text
Environments                                        Esc Close
──────────────────────────────────────────────────────────────
  ● dev        4 variables
› ● staging    6 variables                          active
  ! prod       6 variables   production
  ● local      2 variables

Enter Use  e Edit  n New  d Delete  ? Help
```

The active environment is marked and named in the header. Production environments carry the `! ` prefix and the word `production`. Secret variables in the editor show `••••`.

### Empty first-run state

A workspace with no collections shows a guide, not an empty box:

```text
postctl  ~/.config/postctl                                        env  none
──────────────────────────────────────────────────────────────────────────
Welcome to postctl.

No collections yet. Bring in your Postman data:

›  Import a Postman export         postctl import postman <file.json>
   Import from the Postman app     postctl import postman-local
   Create a collection             n

Everything is stored as plain files in ~/.config/postctl, so it works with git.

? Help                                                             q Quit
```

The primary action is `›` selected. The guide names the storage location so users know where their data lives.

### Import report

```text
Import Postman data                                             env  none
──────────────────────────────────────────────────────────────────────────
Imported 2 collections · 124 requests · 3 environments

● 118 imported
! 5 imported with warnings
× 1 not imported

Warnings
  ! Payments › Refund       pm.visualizer is not supported and will do nothing
  ! Payments › Sync         Auth type oauth1 kept but cannot be sent

Not imported
  × Legacy › Ping           Request has no URL

Enter Open workspace  d Details  Esc Back
```

Every line pairs a symbol with words. Counts add up to the total. Warnings name the collection path and say what will happen at run time. `--dry-run` shows the same report headed `Dry run — nothing was written`.

## Interaction language

- Standard keys, no vim bindings: Enter opens or submits, Escape goes back or cancels, arrows move through lists, `/` filters, `?` shows help, `q` quits where context permits. `Ctrl-Enter` or `Ctrl-S` sends, `Ctrl-P` opens the command palette, `E` opens environments.
- Show only valid key hints for the current screen. A global key must not silently replace a focused text field's expected behavior.
- Support keyboard-only operation. Mouse support can be additive and must not be required.
- Preserve selection and scroll position across sends, resizes, and reloads.
- Do not bind destructive actions to a single easy-to-hit key without confirmation.
- Sending, scripting, disk access, and Keychain access never block the event loop; they run as commands that return typed messages, and a running request can be cancelled with Escape.

## Accessibility and resilience

- Provide visible text labels for all important statuses and actions; color is reinforcement only.
- Keep focus/selection visible using both a marker and style. Ensure users can tell where input will go.
- Offer linear (non-TUI) commands for users relying on screen readers or terminals with poor redraw support: `postctl send` and `postctl run` print plain text without escape sequences.
- Honor `NO_COLOR`, `--accessible`, and `--ascii`. Avoid animation as the only indication of progress.
- Use plain language, actionable errors, and stable screen positions so redraws do not make it difficult to follow updates.
- Reflow on resize. Do not assume mouse, true color, Unicode box drawing, or a particular terminal emulator.
- Palette contrast ratios are checked against representative surfaces only. User-selected terminal colors vary; preserve terminal defaults and verify with a spread of dark/light themes and ANSI-only output.

## Design review checklist

- Can a first-time user identify the active environment and the primary action within a few seconds?
- Does every waiting or error state explain what it is waiting for and what the user can do?
- Can the flow be completed without a mouse, color, or Unicode glyph support?
- Is selection visible without color? Are method names and status codes explicit text?
- Does the screen remain readable at 80 by 24 and reflow across the 100 and 140 column breakpoints?
- Are secrets absent from every visible state, log, and diagnostic message?
- Does the tree reserve width for method and name before optional metadata?
- Does a screen use borders only where they clarify grouping or focus?

## Research references

- [Bubble Tea v2](https://github.com/charmbracelet/bubbletea): background-color queries, cell rendering, input handling, color downsampling.
- [Lip Gloss v2](https://github.com/charmbracelet/lipgloss): styling, layout helpers, color profiles, light/dark helpers.
- [Bubbles v2](https://github.com/charmbracelet/bubbles): lists, tables, help, viewport, spinner, inputs, migration guidance.
- [Huh v2](https://github.com/charmbracelet/huh): composable forms, themes, accessible mode.
- [WCAG 2.2 contrast minimum](https://www.w3.org/TR/wcag/#contrast-minimum): reference contrast guidance used for palette review.
- [`NO_COLOR` standard](https://no-color.org/): convention for disabling ANSI color in command-line software.
