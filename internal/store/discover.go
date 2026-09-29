package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Environment variables that influence workspace discovery.
const (
	// EnvHome overrides the location of the global fallback workspace only;
	// it does not affect project discovery.
	EnvHome = "POSTCTL_HOME"
	// EnvWorkspace forces a workspace directory and skips discovery. An
	// explicit [WithWorkspace] (the --workspace flag) takes precedence.
	EnvWorkspace = "POSTCTL_WORKSPACE"
	// EnvXDGConfigHome is the XDG base directory for user config.
	EnvXDGConfigHome = "XDG_CONFIG_HOME"
)

// Source says how a workspace location was found.
type Source string

// Location sources.
const (
	// SourceFlag means the directory was given explicitly with
	// [WithWorkspace] (the --workspace flag).
	SourceFlag Source = "flag"
	// SourceEnv means the directory came from $POSTCTL_WORKSPACE.
	SourceEnv Source = "env"
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
	// always true for [SourceProject] and may be false for the other sources
	// (an explicit or global directory may not have been initialized yet).
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

// DiscoverOption configures [Discover].
type DiscoverOption func(*discoverConfig)

type discoverConfig struct{ explicit string }

// WithWorkspace forces dir as the workspace and skips discovery (the global
// --workspace DIR flag). An empty dir is ignored, so callers can pass the flag
// value unconditionally. The directory need not be initialized: Location.Initialized
// tells the caller whether to [Init] it.
func WithWorkspace(dir string) DiscoverOption {
	return func(c *discoverConfig) { c.explicit = dir }
}

// Discover finds the workspace to use from start (usually the working
// directory). Precedence, first match wins:
//
//  1. [WithWorkspace] (the --workspace flag): used as given, no discovery;
//  2. $POSTCTL_WORKSPACE: same, no discovery;
//  3. walking up from start for a postctl.yaml (a project workspace);
//  4. [GlobalDir], which may not be initialized yet.
//
// Relative explicit paths are resolved against the process working directory.
// An explicit path that exists but is not a directory is an error.
func Discover(start string, opts ...DiscoverOption) (Location, error) {
	var cfg discoverConfig
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.explicit != "" {
		return explicitLocation(cfg.explicit, SourceFlag)
	}
	if env := os.Getenv(EnvWorkspace); env != "" {
		return explicitLocation(env, SourceEnv)
	}
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

func explicitLocation(dir string, src Source) (Location, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Location{}, err
	}
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		return Location{}, fmt.Errorf("workspace %s is not a directory", abs)
	}
	return Location{Root: abs, Source: src, Initialized: isFile(filepath.Join(abs, workspaceYAML))}, nil
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
