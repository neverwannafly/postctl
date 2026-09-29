package model

import "strings"

// Script languages.
const (
	// LangJS is JavaScript, the only language executed.
	LangJS = "js"
)

// Scripts are the two script slots on a collection, folder or request.
// Postman allows several events of the same kind; importers concatenate them
// in order into one Source so behaviour is preserved.
type Scripts struct {
	// PreRequest runs before the request is sent (Postman: prerequest).
	PreRequest *Script `yaml:"preRequest,omitempty"`
	// Test runs after the response arrives (Postman: test).
	Test *Script `yaml:"test,omitempty"`
}

// Script is one script. In memory Source is authoritative. The store writes
// Source to a sibling .js file and records its name in File; the file is what
// you edit in $EDITOR.
type Script struct {
	// Language is "js" (see [LangJS]); empty means JavaScript.
	Language string `yaml:"language,omitempty"`
	// Source is the program text with lines joined by "\n". Never
	// serialized in the YAML file; see File.
	Source string `yaml:"-"`
	// File is the .js file holding Source, relative to the YAML file that
	// references it. The store sets it on load and derives it on save
	// (any value set by callers is ignored on save).
	File string `yaml:"file,omitempty"`
	// ID is the source's script ID (Postman: script.id), if any.
	ID ID `yaml:"id,omitempty"`
	// Name is an optional script label (Postman: script.name).
	Name string `yaml:"name,omitempty"`
	// Src is an external URL for scripts the source referenced rather than
	// embedded (Postman: script.src). Preserved, not fetched.
	Src string `yaml:"src,omitempty"`
	// Disabled keeps the script but skips it at run time (Postman:
	// event.disabled).
	Disabled bool `yaml:"disabled,omitempty"`
}

// ScriptFromExec builds a JavaScript Script from Postman-style "exec" lines.
// Language is left empty (JavaScript) to keep files free of noise.
func ScriptFromExec(lines []string) *Script {
	return &Script{Source: strings.Join(lines, "\n")}
}

// Exec returns Source as Postman-style "exec" lines. It is the inverse of
// [ScriptFromExec]; an empty Source yields a single empty line.
func (s *Script) Exec() []string { return strings.Split(s.Source, "\n") }

// Empty reports whether the script has no code and no external source.
func (s *Script) Empty() bool {
	return s == nil || (strings.TrimSpace(s.Source) == "" && s.Src == "")
}
