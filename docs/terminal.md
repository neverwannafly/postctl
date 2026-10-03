# Terminal and tmux setup

postctl is tuned for **Ghostty** inside **tmux** (plan U2, U4). Other terminals work with the fallbacks below; keys match [`docs/keys.md`](keys.md).

## Recommended tmux configuration

Add to `~/.tmux.conf` (reload with `tmux source-file ~/.tmux.conf`):

```tmux
# Extended keys (Ctrl-Enter, distinct Esc sequences) — tmux 3.4+
set -g extended-keys on
# csi-u format needs tmux 3.5+; on 3.4 keep extended-keys on and omit the next line
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

## vim-tmux-navigator with postctl

postctl uses **Ctrl-h**, **Ctrl-j**, **Ctrl-k** and **Ctrl-l** for pane focus inside the app. At the layout edge it runs `tmux select-pane -L|-D|-U|-R` (unless the tmux window is zoomed). For tmux to pass those keys through to postctl instead of changing panes immediately, add bindings like vim-tmux-navigator (detects vim and postctl on the pane tty):

```tmux
is_postctl="ps -o state= -o comm= -t '#{pane_tty}' | grep -iqE '^[^TXZ ]+ +(\\S+\\/)?(g?(view|l?n?vim?x?|fzf)(diff)?|postctl)$'"
bind-key -n C-h if-shell "$is_postctl" 'send-keys C-h' 'select-pane -L'
bind-key -n C-j if-shell "$is_postctl" 'send-keys C-j' 'select-pane -D'
bind-key -n C-k if-shell "$is_postctl" 'send-keys C-k' 'select-pane -U'
bind-key -n C-l if-shell "$is_postctl" 'send-keys C-l' 'select-pane -R'

bind-key -T copy-mode-vi C-h select-pane -L
bind-key -T copy-mode-vi C-j select-pane -D
bind-key -T copy-mode-vi C-k select-pane -U
bind-key -T copy-mode-vi C-l select-pane -R
```

When postctl is focused and you press **Ctrl-h** at its left edge, postctl hands off to tmux; when not at the edge, tmux forwards **Ctrl-h** into postctl.

## Ghostty

Example `config` lines:

```ini
macos-option-as-alt = true
clipboard-write = allow
```

Ghostty sends **Ctrl-Enter** via the kitty keyboard protocol by default, so you usually do not need an extra passthrough keybind. If you added a user `keybind` that captures **ctrl+enter**, remove or change it, or send with **Ctrl-S** (always works). Prefer postctl inside tmux with the settings above so **Ctrl-Enter** and **Ctrl-i** use enhanced keyboard reporting. Native text selection with the mouse usually needs **Shift**-drag while postctl mouse mode is on.

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
