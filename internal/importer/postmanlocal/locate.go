package postmanlocal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const postmanRelativePath = "Library/Application Support/Postman"

// Locate finds Postman's active user partition.
func Locate() (Install, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Install{}, fmt.Errorf("find home directory: %w", err)
	}
	return locateAt(filepath.Join(home, postmanRelativePath))
}

func locateAt(dataDir string) (Install, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "storage", "userPartitionData.json"))
	if err != nil {
		return Install{}, fmt.Errorf("read Postman partition metadata: %w", err)
	}
	var meta struct {
		NamespaceMeta struct {
			Users struct {
				Active string `json:"activePartition"`
			} `json:"users"`
		} `json:"v8PartitionsNamespaceMeta"`
		Partitions map[string]json.RawMessage `json:"v8Partitions"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Install{}, fmt.Errorf("decode Postman partition metadata: %w", err)
	}
	if meta.NamespaceMeta.Users.Active == "" {
		return Install{}, errors.New("postman has no active user partition")
	}
	if _, ok := meta.Partitions[meta.NamespaceMeta.Users.Active]; !ok {
		return Install{}, errors.New("postman active partition is not present")
	}
	indexedDB := filepath.Join(dataDir, "Partitions", meta.NamespaceMeta.Users.Active, "IndexedDB")
	if stat, err := os.Stat(indexedDB); err != nil || !stat.IsDir() {
		return Install{}, errors.New("postman active partition has no IndexedDB directory")
	}
	return Install{DataDir: dataDir, PartitionID: meta.NamespaceMeta.Users.Active, IndexedDB: indexedDB}, nil
}

func withPartition(install Install, partition string) (Install, error) {
	if partition == "" || partition == install.PartitionID {
		return install, nil
	}
	path := filepath.Join(install.DataDir, "Partitions", partition, "IndexedDB")
	if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
		return Install{}, fmt.Errorf("postman partition %q has no IndexedDB directory", partition)
	}
	install.PartitionID, install.IndexedDB = partition, path
	return install, nil
}

// copyIndexedDB makes the mandatory private working copy.
func copyIndexedDB(source string) (string, func(), error) {
	root, err := os.MkdirTemp("", "postctl-postman-*")
	if err != nil {
		return "", nil, fmt.Errorf("create extraction directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	target := filepath.Join(root, "IndexedDB")
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inCloseErr != nil {
			return inCloseErr
		}
		return closeErr
	})
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copy Postman IndexedDB: %w", err)
	}
	return target, cleanup, nil
}
