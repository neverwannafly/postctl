# Terminal and tmux setup

postctl is tuned for **Ghostty** inside **tmux** (plan U2, U4). Other terminals work with the fallbacks below; keys match [`docs/keys.md`](keys.md).

## Recommended tmux configuration

Add to `~/.tmux.conf` (reload with `tmux source-file ~/.tmux.conf`):

```tmux
# Extended keys (Ctrl-Enter, distinct Esc sequences) — tmux 3.4+
set -g extended-keys on
set -g extended-keys-format csi-u

# Tell tmux Ghostty speaks the kitty keyboard protocol
set -as terminal-features 'xterm-ghostty:extkeys'

# Fast Escape / prefix detection (ms)
set -g escape-time 0

# Mouse: click to focus, scroll (postctl also handles mouse when enabled)
set -g mouse on

# OSC 52 clipboard from postctl and the terminal
set -g set-clipboard on

# Focus events for pane activation
set -g focus-events on
```

On tmux versions without `extended-keys-format`, keep `extended-keys on` and omit the format line.

## vim-tmux-navigator with postctl

So **Ctrl-h**, **Ctrl-j**, **Ctrl-k** and **Ctrl-l** reach postctl at the edge of its layout and then move to the neighbouring tmux pane, extend the usual `is_vim` check to include the postctl process:

```bash
is_vim='vim|nvim|vi|postctl'
if [[ -n "$TMUX" ]] && [[ $# -eq 1 ]] && [[ $1 =~ $is_vim ]]; then
  tmux send-keys -X "$1"
else
  # ... your existing select-pane bindings ...
fi
```

Map **Ctrl-h/j/k/l** in tmux only when the pane is *not* vim/postctl, or use a plugin that respects `is_vim`. postctl calls `tmux select-pane -L|-D|-U|-R` when focus is already at its edge (not when the tmux window is zoomed).

## Ghostty

- Prefer running postctl inside tmux with the config above so **Ctrl-Enter** and **Ctrl-i** use enhanced keyboard reporting.
- If Ghostty intercepts **Ctrl-Enter**, add a passthrough keybind in `config` (see Ghostty docs), or send with **Ctrl-S** (always works).
- Native text selection with the mouse usually needs **Shift**-drag while postctl mouse mode is on.

## Other terminals

| Need | Preferred | Fallback |
| --- | --- | --- |
| Send | **Ctrl-Enter** (enhanced keyboard) | **Ctrl-S**; **Enter** in the URL bar |
| Jump forward | **Ctrl-i** (enhanced keyboard) | **Space ]** |
| Jump back | **Ctrl-o** | **Space [** |
| Option/Alt shortcuts | Terminal “Option as Meta” (iTerm2, Terminal.app) | Some keys unavailable |
| Copy via OSC 52 | tmux `set-clipboard on` | Use **Space y** / **Space Y** curl copy instead |

### iTerm2

Profiles → Keys → set **Left Option** and **Right Option** to **Esc+** (Meta). Enable “Report modifiers using CSI u” if available.

### kitty / WezTerm

Enable kitty keyboard protocol (WezTerm: `enable_kitty_keyboard = true`). **Ctrl-S** still sends if **Ctrl-Enter** is not reported.

### Terminal.app

Enable “Use Option as Meta key”. Expect **Ctrl-Enter** to be unavailable; use **Ctrl-S**.

## Startup notice

When `$TMUX` is set, postctl checks `extended-keys`, `escape-time`, `mouse` and `set-clipboard` once at start (300 ms budget). If something is off, a single warning appears on the status line, for example:

`tmux: set extended-keys on, escape-time 0 for Ctrl-Enter and Esc (see docs/terminal.md)`

Fix the settings above; the notice does not block the UI.
