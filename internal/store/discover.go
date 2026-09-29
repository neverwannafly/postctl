package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Environment variables that influence workspace discovery.
const (
	// EnvHome overrides the location of the global workspace.
	EnvHome = "POSTCTL_HOME"
	// EnvXDGConfigHome is the XDG base directory for user config.
	EnvXDGConfigHome = "XDG_CONFIG_HOME"
)

// Source says how a workspace location was found.
type Source string

// Location sources.
const (
	// SourceProject means a postctl.yaml was found walking up from the
	// start directory.
	SourceProject Source = "project"
	// SourceGlobal means the user-level fallback directory was used.
	SourceGlobal Source = "global"
)

// Location is a discovered workspace directory.
type Location struct {
	// Root is the absolute workspace directory.
	Root string
	// Source is how it was found.
	Source Source
	// Initialized reports whether Root already contains postctl.yaml. It is
	// always true for [SourceProject] and may be false for [SourceGlobal].
	Initialized bool
}

// GlobalDir returns the user-level workspace directory: $POSTCTL_HOME if set,
// otherwise $XDG_CONFIG_HOME/postctl, otherwise ~/.config/postctl.
func GlobalDir() (string, error) {
	if h := os.Getenv(EnvHome); h != "" {
		return filepath.Abs(h)
	}
	if x := os.Getenv(EnvXDGConfigHome); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "postctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "postctl"), nil
}

// Discover finds the workspace to use from start (usually the working
// directory). It walks up looking for postctl.yaml; if none is found it falls
// back to [GlobalDir], which may not be initialized yet.
func Discover(start string) (Location, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return Location{}, err
	}
	for dir := abs; ; {
		if isFile(filepath.Join(dir, workspaceYAML)) {
			return Location{Root: dir, Source: SourceProject, Initialized: true}, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	g, err := GlobalDir()
	if err != nil {
		return Location{}, err
	}
	return Location{Root: g, Source: SourceGlobal, Initialized: isFile(filepath.Join(g, workspaceYAML))}, nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return !errors.Is(err, os.ErrNotExist) && err == nil
}
