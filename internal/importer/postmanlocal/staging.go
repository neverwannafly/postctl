package postmanlocal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeFilename = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// WriteStaging writes standard Postman JSON files below dir.
func (r *Result) WriteStaging(dir string) error {
	if r == nil {
		return errors.New("nil extraction result")
	}
	for _, subdir := range []string{"collections", "environments"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0o700); err != nil {
			return fmt.Errorf("create staging directory: %w", err)
		}
	}
	used := map[string]int{}
	for _, collection := range r.Collections {
		if err := ValidateCollection(collection); err != nil {
			return err
		}
		name := text(object(collection["info"])["name"])
		path := uniquePath(filepath.Join(dir, "collections"), safeFilename(name), ".postman_collection.json", used)
		if err := writeJSON(path, collection); err != nil {
			return err
		}
	}
	for _, environment := range r.Environments {
		name := text(environment["name"])
		path := uniquePath(filepath.Join(dir, "environments"), safeFilename(name), ".postman_environment.json", used)
		if err := writeJSON(path, environment); err != nil {
			return err
		}
	}
	if r.Globals != nil {
		if err := writeJSON(filepath.Join(dir, "globals.postman_globals.json"), r.Globals); err != nil {
			return err
		}
	}
	return nil
}

// ValidateCollection performs the required structural v2.1 validation.
func ValidateCollection(document Document) error {
	info := object(document["info"])
	if text(info["name"]) == "" {
		return errors.New("collection has no name")
	}
	if text(info["schema"]) != collectionSchema {
		return errors.New("collection does not declare the Postman v2.1 schema")
	}
	if _, ok := document["item"].([]any); !ok {
		return errors.New("collection item is not an array")
	}
	return validateItems(array(document["item"]))
}

func validateItems(items []any) error {
	for _, raw := range items {
		item := object(raw)
		if text(item["name"]) == "" {
			return errors.New("collection item has no name")
		}
		if children, ok := item["item"]; ok {
			if _, ok := children.([]any); !ok {
				return errors.New("folder item is not an array")
			}
			if err := validateItems(array(children)); err != nil {
				return err
			}
			continue
		}
		request := object(item["request"])
		if len(request) == 0 || text(request["method"]) == "" {
			return errors.New("request item is malformed")
		}
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode staging JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write staging JSON: %w", err)
	}
	return nil
}

func safeFilename(name string) string {
	name = strings.Trim(unsafeFilename.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		return "unnamed"
	}
	return name
}

func uniquePath(dir, name, suffix string, used map[string]int) string {
	base := name + suffix
	key := filepath.Join(dir, base)
	used[key]++
	if used[key] == 1 {
		return key
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", name, used[key], suffix))
}
