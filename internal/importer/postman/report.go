// Package postman imports Postman Collection v2.0/v2.1, environment, and
// globals JSON into postctl's domain model.
package postman

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Counts summarizes converted Postman data.
type Counts struct {
	Collections  int `json:"collections"`
	Folders      int `json:"folders"`
	Requests     int `json:"requests"`
	PreScripts   int `json:"preRequestScripts"`
	TestScripts  int `json:"testScripts"`
	Examples     int `json:"examples"`
	Environments int `json:"environments"`
	Globals      int `json:"globals"`
	SecretsMoved int `json:"secretsMovedToKeychain"`
}

// Warning describes a non-fatal compatibility or coverage issue.
type Warning struct {
	Path    string `json:"path,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Failure describes one input that could not be imported. Error text never
// includes document values.
type Failure struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

// Changes summarizes the effect of merging imported documents into a
// workspace.
type Changes struct {
	Added   []string `json:"added,omitempty"`
	Changed []string `json:"changed,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Kept    []string `json:"keptNotInSource,omitempty"`
}

// Report is the complete privacy-safe import result.
type Report struct {
	DryRun   bool      `json:"dryRun"`
	Counts   Counts    `json:"counts"`
	Changes  Changes   `json:"changes"`
	Warnings []Warning `json:"warnings,omitempty"`
	Failures []Failure `json:"failures,omitempty"`
}

// AddWarning appends a warning without exposing source values.
func (r *Report) AddWarning(path, code, message string) {
	r.Warnings = append(r.Warnings, Warning{Path: path, Code: code, Message: message})
}

// AddFailure appends a failed input.
func (r *Report) AddFailure(source, message string) {
	r.Failures = append(r.Failures, Failure{Source: source, Message: message})
}

// Merge combines another report into r.
func (r *Report) Merge(other Report) {
	r.Counts.Collections += other.Counts.Collections
	r.Counts.Folders += other.Counts.Folders
	r.Counts.Requests += other.Counts.Requests
	r.Counts.PreScripts += other.Counts.PreScripts
	r.Counts.TestScripts += other.Counts.TestScripts
	r.Counts.Examples += other.Counts.Examples
	r.Counts.Environments += other.Counts.Environments
	r.Counts.Globals += other.Counts.Globals
	r.Counts.SecretsMoved += other.Counts.SecretsMoved
	r.Changes.Added = append(r.Changes.Added, other.Changes.Added...)
	r.Changes.Changed = append(r.Changes.Changed, other.Changes.Changed...)
	r.Changes.Removed = append(r.Changes.Removed, other.Changes.Removed...)
	r.Changes.Kept = append(r.Changes.Kept, other.Changes.Kept...)
	r.Warnings = append(r.Warnings, other.Warnings...)
	r.Failures = append(r.Failures, other.Failures...)
}

// WriteJSON writes the machine-readable report.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText writes the linear, accessible import report.
func (r Report) WriteText(w io.Writer) {
	if r.DryRun {
		fmt.Fprintln(w, "Dry run — nothing was written")
	} else {
		fmt.Fprintln(w, "Import Postman data")
	}
	fmt.Fprintln(w, "──────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(w, "Imported %d collections · %d folders · %d requests · %d environments\n",
		r.Counts.Collections, r.Counts.Folders, r.Counts.Requests, r.Counts.Environments)
	fmt.Fprintf(w, "Scripts %d pre-request · %d test · %d examples · %d secrets moved to Keychain\n",
		r.Counts.PreScripts, r.Counts.TestScripts, r.Counts.Examples, r.Counts.SecretsMoved)
	fmt.Fprintf(w, "● %d added · %d changed · %d kept (not in source)\n",
		len(r.Changes.Added), len(r.Changes.Changed), len(r.Changes.Kept))
	if len(r.Changes.Removed) > 0 {
		fmt.Fprintf(w, "× %d removed\n", len(r.Changes.Removed))
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Warnings")
		for _, warning := range r.Warnings {
			if warning.Path == "" {
				fmt.Fprintf(w, "  ! %s\n", warning.Message)
			} else {
				fmt.Fprintf(w, "  ! %s  %s\n", warning.Path, warning.Message)
			}
		}
	}
	if len(r.Failures) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Not imported")
		for _, failure := range r.Failures {
			fmt.Fprintf(w, "  × %s  %s\n", failure.Source, failure.Message)
		}
	}
}

func (r *Report) sort() {
	sort.Strings(r.Changes.Added)
	sort.Strings(r.Changes.Changed)
	sort.Strings(r.Changes.Removed)
	sort.Strings(r.Changes.Kept)
	sort.SliceStable(r.Warnings, func(i, j int) bool {
		if r.Warnings[i].Path == r.Warnings[j].Path {
			return r.Warnings[i].Code < r.Warnings[j].Code
		}
		return r.Warnings[i].Path < r.Warnings[j].Path
	})
}
