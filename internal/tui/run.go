package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"
)

// Run starts the interactive UI and blocks until the user quits.
//
// Environment: NO_COLOR (any value) disables color, POSTCTL_ACCESSIBLE selects
// the linear presentation and POSTCTL_ASCII the ASCII glyph set. Explicit
// options win over the environment.
func Run(opts Options) error {
	opts = ApplyEnv(opts)
	_, err := tea.NewProgram(newApp(opts)).Run()
	return err
}

// ApplyEnv folds NO_COLOR, POSTCTL_ACCESSIBLE and POSTCTL_ASCII into opts.
func ApplyEnv(opts Options) Options {
	if os.Getenv("NO_COLOR") != "" {
		opts.NoColor = true
	}
	if truthy(os.Getenv("POSTCTL_ACCESSIBLE")) {
		opts.Accessible = true
	}
	if truthy(os.Getenv("POSTCTL_ASCII")) {
		opts.ASCII = true
	}
	if truthy(os.Getenv("POSTCTL_NO_MOUSE")) {
		opts.NoMouse = true
	}
	return opts
}

// truthy reports whether an environment value means "on": anything except
// empty, 0, false, no and off.
func truthy(v string) bool {
	switch v {
	case "", "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false
	}
	return true
}
