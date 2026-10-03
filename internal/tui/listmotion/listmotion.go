// Package listmotion implements shared vim list navigation (j/k, gg/G,
// Ctrl-d/Ctrl-u) for TUI screens.
package listmotion

import tea "charm.land/bubbletea/v2"

// View holds cursor position and gg state for one list.
type View struct {
	Cursor int
	Top    int
	GWait  bool // true after a lone g, waiting for the second g
}

// Config describes a list being navigated.
type Config struct {
	Count int // number of items; 0 is allowed
	Page  int // visible rows; at least 1 is used
}

// Result reports what happened to the list view.
type Result struct {
	Moved   bool
	Handled bool
}

// Key applies one key to v and returns whether the key was consumed and whether
// the cursor moved. gg is two separate g presses; G jumps to the last item.
func Key(k tea.KeyPressMsg, cfg Config, v *View) Result {
	if cfg.Count <= 0 {
		switch k.String() {
		case "j", "down", "k", "up", "ctrl+d", "pgdown", "ctrl+u", "pgup", "home", "end", "G", "g":
			v.GWait = false
			return Result{Handled: true}
		default:
			if k.String() != "g" {
				return Result{}
			}
		}
	}
	clamped := false
	if cfg.Count > 0 {
		beforeC, beforeT := v.Cursor, v.Top
		v.Cursor = min(v.Cursor, cfg.Count-1)
		if v.Cursor < 0 {
			v.Cursor = 0
		}
		page := max(1, cfg.Page)
		if v.Top > v.Cursor {
			v.Top = v.Cursor
		}
		if v.Top > max(0, cfg.Count-page) {
			v.Top = max(0, cfg.Count-page)
		}
		clamped = v.Cursor != beforeC || v.Top != beforeT
	}
	page := max(1, cfg.Page)
	moved := func(was bool) bool { return was || clamped }
	half := max(1, page/2)
	s := k.String()
	switch s {
	case "j", "down":
		v.GWait = false
		if cfg.Count == 0 {
			return Result{Handled: true, Moved: moved(false)}
		}
		next := min(cfg.Count-1, v.Cursor+1)
		if next != v.Cursor {
			v.Cursor = next
			v.ensureVisible(page)
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: true, Moved: moved(false)}
	case "k", "up":
		v.GWait = false
		if cfg.Count == 0 {
			return Result{Handled: true, Moved: moved(false)}
		}
		next := max(0, v.Cursor-1)
		if next != v.Cursor {
			v.Cursor = next
			v.ensureVisible(page)
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: true, Moved: moved(false)}
	case "ctrl+d", "pgdown":
		v.GWait = false
		if cfg.Count == 0 {
			return Result{Handled: true, Moved: moved(false)}
		}
		next := min(cfg.Count-1, v.Cursor+half)
		if next != v.Cursor {
			v.Cursor = next
			v.ensureVisible(page)
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: true, Moved: moved(false)}
	case "ctrl+u", "pgup":
		v.GWait = false
		if cfg.Count == 0 {
			return Result{Handled: true, Moved: moved(false)}
		}
		next := max(0, v.Cursor-half)
		if next != v.Cursor {
			v.Cursor = next
			v.ensureVisible(page)
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: true, Moved: moved(false)}
	case "g":
		if v.GWait {
			v.GWait = false
			if v.Cursor != 0 {
				v.Cursor = 0
				v.Top = 0
				return Result{Handled: true, Moved: moved(true)}
			}
			return Result{Handled: true, Moved: moved(false)}
		}
		v.GWait = true
		return Result{Handled: true, Moved: moved(false)}
	case "G":
		v.GWait = false
		if cfg.Count == 0 {
			return Result{Handled: true, Moved: moved(false)}
		}
		last := cfg.Count - 1
		if v.Cursor != last {
			v.Cursor = last
			v.ensureVisible(page)
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: true, Moved: moved(false)}
	case "home":
		v.GWait = false
		if cfg.Count > 0 && v.Cursor != 0 {
			v.Cursor = 0
			v.Top = 0
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: cfg.Count > 0, Moved: moved(false)}
	case "end":
		v.GWait = false
		if cfg.Count == 0 {
			return Result{Handled: true, Moved: moved(false)}
		}
		last := cfg.Count - 1
		if v.Cursor != last {
			v.Cursor = last
			v.ensureVisible(page)
			return Result{Handled: true, Moved: moved(true)}
		}
		return Result{Handled: true, Moved: moved(false)}
	default:
		v.GWait = false
		return Result{}
	}
}

func (v *View) ensureVisible(page int) {
	if v.Cursor < v.Top {
		v.Top = v.Cursor
	}
	if v.Cursor >= v.Top+page {
		v.Top = v.Cursor - page + 1
	}
}
