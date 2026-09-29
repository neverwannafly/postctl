package store

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/shubham/postctl/internal/model"
)

const stateFile = "state.yaml"

// State is small per-machine workspace state kept in .postctl/state.yaml. It is
// not versioned with the workspace.
type State struct {
	// ActiveEnvironment is the ID of the selected environment, or empty for
	// none.
	ActiveEnvironment model.ID `yaml:"activeEnvironment,omitempty"`
}

// LoadState reads local state. A missing file yields the zero State.
func (s *Store) LoadState() (State, error) {
	var st State
	path := filepath.Join(s.StateDir(), stateFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, &FileError{Path: path, Err: err}
	}
	if err := unmarshalYAML(data, &st); err != nil {
		return State{}, &FileError{Path: path, Err: err}
	}
	return st, nil
}

// SaveState writes local state, creating .postctl/ if needed.
func (s *Store) SaveState(st State) error {
	path := filepath.Join(s.StateDir(), stateFile)
	data, err := marshalYAML(&st)
	if err != nil {
		return err
	}
	if _, err := writeFileAtomic(path, data); err != nil {
		return &FileError{Path: path, Err: err}
	}
	return nil
}
