package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Errors returned by the store.
var (
	// ErrNotWorkspace means the directory has no postctl.yaml.
	ErrNotWorkspace = errors.New("store: not a postctl workspace")
	// ErrExists means a workspace already exists at the target directory.
	ErrExists = errors.New("store: workspace already exists")
	// ErrNoSecretStore means a secret value must be saved but the store has
	// no SecretStore configured. Plaintext fallback is never used.
	ErrNoSecretStore = errors.New("store: secret value needs a SecretStore")
	// ErrNewerSchema means a file was written by a newer postctl.
	ErrNewerSchema = errors.New("store: file schema is newer than this postctl supports")
	// ErrUnsafePath means a YAML file references a script path outside its
	// own directory.
	ErrUnsafePath = errors.New("store: unsafe path")
)

// FileError adds the file path to a read, parse or write failure.
type FileError struct {
	// Path is the absolute file path.
	Path string
	// Err is the underlying error.
	Err error
}

// Error implements error.
func (e *FileError) Error() string { return e.Path + ": " + e.Err.Error() }

// Unwrap returns the underlying error.
func (e *FileError) Unwrap() error { return e.Err }

const (
	dirPerm  fs.FileMode = 0o755
	filePerm fs.FileMode = 0o644
)

// marshalYAML encodes v with two-space indentation. Struct fields keep their
// declaration order and map keys are sorted, so output is deterministic.
func marshalYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unmarshalYAML decodes data into v, rejecting unknown fields so typos in
// hand-edited files surface as errors.
func unmarshalYAML(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil // empty file: leave v as the zero value
		}
		return err
	}
	return nil
}

// writeFileAtomic writes data to path via a temp file in the same directory
// and renames it into place. It reports whether the file changed.
func writeFileAtomic(path string, data []byte) (changed bool, err error) {
	if old, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(old, data) {
		return false, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".postctl-tmp-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err = tmp.Close(); err != nil {
		return false, err
	}
	if err = os.Chmod(tmpName, filePerm); err != nil {
		return false, err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}

// safeRel validates that name is a plain relative path with no traversal.
func safeRel(name string) error {
	if name == "" || filepath.IsAbs(name) || strings.HasPrefix(filepath.Clean(name), "..") ||
		strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	return nil
}
