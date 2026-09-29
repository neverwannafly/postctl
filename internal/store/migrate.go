package store

import (
	"errors"
	"fmt"

	"github.com/shubham/postctl/internal/model"
	"go.yaml.in/yaml/v3"
)

// FileKind names a kind of YAML file for migrations.
type FileKind string

// File kinds.
const (
	// KindWorkspace is postctl.yaml.
	KindWorkspace FileKind = "workspace"
	// KindCollection is collection.yaml.
	KindCollection FileKind = "collection"
	// KindFolder is folder.yaml. It takes the schema version of its
	// collection.
	KindFolder FileKind = "folder"
	// KindRequest is a request file. It takes the schema version of its
	// collection.
	KindRequest FileKind = "request"
	// KindEnvironment is environments/<slug>.yaml.
	KindEnvironment FileKind = "environment"
	// KindGlobals is globals.yaml.
	KindGlobals FileKind = "globals"
)

// carriesVersion reports whether files of this kind have their own
// schemaVersion field.
func (k FileKind) carriesVersion() bool {
	return k != KindFolder && k != KindRequest
}

// Migration upgrades one file kind from schema version From to From+1. Apply
// receives the document's root mapping node and edits it in place; the store
// updates schemaVersion afterwards. Migrations run in version order when a
// file older than [model.SchemaVersion] is read; files are only rewritten in
// the new format when next saved.
//
// There are no migrations yet (the schema is at version 1). The hook exists so
// a future version bump is a pure addition; register migrations with
// [WithMigrations].
type Migration struct {
	// Kind selects which files the migration applies to.
	Kind FileKind
	// From is the schema version the migration upgrades from.
	From int
	// Apply edits the root mapping node.
	Apply func(doc *yaml.Node) error
}

// peekVersion reads the schemaVersion field; missing means 1 (hand-written
// files need not state it).
func peekVersion(data []byte) (int, error) {
	var p struct {
		SchemaVersion int `yaml:"schemaVersion"`
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return 0, err
	}
	if p.SchemaVersion == 0 {
		return 1, nil
	}
	return p.SchemaVersion, nil
}

// migrate applies registered migrations of kind to data written at version
// from and returns the upgraded bytes.
func (s *Store) migrate(kind FileKind, from int, data []byte) ([]byte, error) {
	if from > model.SchemaVersion {
		return nil, fmt.Errorf("%w (file is v%d, this build supports v%d)", ErrNewerSchema, from, model.SchemaVersion)
	}
	if from == model.SchemaVersion {
		return data, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("expected a YAML mapping at the top level")
	}
	root := doc.Content[0]
	for v := from; v < model.SchemaVersion; v++ {
		for _, m := range s.migrations {
			if m.Kind == kind && m.From == v {
				if err := m.Apply(root); err != nil {
					return nil, fmt.Errorf("migrate %s v%d: %w", kind, v, err)
				}
			}
		}
	}
	if kind.carriesVersion() {
		setSchemaVersion(root, model.SchemaVersion)
	}
	return marshalYAML(&doc)
}

func setSchemaVersion(root *yaml.Node, v int) {
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(v)}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "schemaVersion" {
			root.Content[i+1] = val
			return
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "schemaVersion"}
	root.Content = append([]*yaml.Node{key, val}, root.Content...)
}
