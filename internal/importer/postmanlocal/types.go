// Package postmanlocal extracts Postman data from a copied Chromium IndexedDB
// and emits standard Postman Collection v2.1 files.
package postmanlocal

import (
	"context"
	"encoding/json"
)

// Install describes one Postman desktop data installation.
type Install struct {
	DataDir     string
	PartitionID string
	IndexedDB   string
}

// Options controls extraction. Partition overrides Install.PartitionID when set.
type Options struct {
	Partition string
}

// Document is a decoded Postman JSON document.
type Document map[string]any

// CollectionCoverage reports extraction completeness without exposing content.
type CollectionCoverage struct {
	Name              string `json:"name"`
	Folders           int    `json:"folders"`
	Requests          int    `json:"requests"`
	PreRequestScripts int    `json:"preRequestScripts"`
	TestScripts       int    `json:"testScripts"`
	Examples          int    `json:"examples"`
	MissingBodies     int    `json:"missingBodies"`
	Unresolved        int    `json:"unresolvedReferences"`
}

// Coverage contains privacy-safe counts only.
type Coverage struct {
	Collections       int                  `json:"collections"`
	Folders           int                  `json:"folders"`
	Requests          int                  `json:"requests"`
	PreRequestScripts int                  `json:"preRequestScripts"`
	TestScripts       int                  `json:"testScripts"`
	Examples          int                  `json:"examples"`
	MissingBodies     int                  `json:"missingBodies"`
	Unresolved        int                  `json:"unresolvedReferences"`
	Environments      int                  `json:"environments"`
	Globals           int                  `json:"globals"`
	PerCollection     []CollectionCoverage `json:"perCollection"`
}

// Result is the in-memory standard Postman representation.
type Result struct {
	Collections  []Document
	Environments []Document
	Globals      Document
	Coverage     Coverage
}

// Extractor is useful for callers that want to substitute an extractor in tests.
type Extractor interface {
	Extract(context.Context, Install, Options) (*Result, error)
}

// MarshalCoverage returns only the privacy-safe coverage report.
func (r *Result) MarshalCoverage() ([]byte, error) {
	return json.MarshalIndent(r.Coverage, "", "  ")
}
