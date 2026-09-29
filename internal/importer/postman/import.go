package postman

import (
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/shubham/postctl/internal/importer/postmanlocal"
	"github.com/shubham/postctl/internal/model"
)

// MergeDocuments applies imported documents to workspace, overwriting by
// stable Postman ID (plan §0 Q3). A document replaces the node with its ID
// wholesale: fields are taken from the source, not merged with local edits.
// For a collection this includes its items: requests and folders that are no
// longer in the export are removed, ones that are new are added, and ones
// that moved keep their ID. Collections, environments and globals the
// documents do not mention are left alone and listed as kept.
//
// report.Changes describes exactly what a save will do, so a dry run (which
// stops after this call) and a real import agree. A node counts as changed
// only when its saved form would differ: secret values, which a dry run does
// not read from the Keychain, and script file names, which the store derives,
// do not count.
//
// Local state outside the imported documents is never touched here: the
// caller saves only workspace files, and .postctl/ (history, cookies, UI
// state) is not part of the model. An OAuth 2.0 token the user fetched for a
// node stays attached to the node when the export carries no token of its own.
func MergeDocuments(workspace *model.Workspace, documents []*Document, report *Report) {
	sourceCollections := map[model.ID]bool{}
	sourceEnvironments := map[model.ID]bool{}
	hasGlobals := false
	for _, document := range documents {
		switch document.Kind {
		case KindCollection:
			sourceCollections[document.Collection.ID] = true
			mergeCollection(workspace, document.Collection, report)
		case KindEnvironment:
			sourceEnvironments[document.Environment.ID] = true
			mergeEnvironment(workspace, document.Environment, report)
		case KindGlobals:
			hasGlobals = true
			mergeGlobals(workspace, document.Globals, report)
		}
	}
	for _, collection := range workspace.Collections {
		if !sourceCollections[collection.ID] {
			report.Changes.Kept = append(report.Changes.Kept, collection.Name)
		}
	}
	for _, environment := range workspace.Environments {
		if !sourceEnvironments[environment.ID] {
			report.Changes.Kept = append(report.Changes.Kept, "Environment › "+environment.Name)
		}
	}
	if workspace.Globals != nil && !hasGlobals {
		report.Changes.Kept = append(report.Changes.Kept, "Globals")
	}
	report.sort()
}

func mergeCollection(workspace *model.Workspace, imported *model.Collection, report *Report) {
	for index, existing := range workspace.Collections {
		if existing.ID != imported.ID {
			continue
		}
		oldNodes := indexNodes(existing)
		keepOAuthTokens(oldNodes, imported)
		diffCollection(existing, imported, oldNodes, report)
		workspace.Collections[index] = imported
		return
	}
	workspace.Collections = append(workspace.Collections, imported)
	report.Changes.Added = append(report.Changes.Added, imported.Name)
}

// node is one folder or request of a collection with where it sits.
type node struct {
	item   model.Item
	path   string
	parent model.ID
}

// indexNodes lists every folder and request of a collection by ID.
func indexNodes(collection *model.Collection) map[model.ID]node {
	nodes := map[model.ID]node{}
	var walk func(parent model.Container, parentID model.ID, parentPath string)
	walk = func(parent model.Container, parentID model.ID, parentPath string) {
		for _, item := range parent.Children() {
			path := joinPath(parentPath, item.ItemName())
			nodes[item.ItemID()] = node{item: item, path: path, parent: parentID}
			if folder, ok := item.(*model.Folder); ok {
				walk(folder, folder.ID, path)
			}
		}
	}
	walk(collection, collection.ID, collection.Name)
	return nodes
}

// diffCollection records what replacing existing by imported adds, changes
// and removes. Every request and folder is reported on its own, so removing a
// folder lists the folder and each request in it.
func diffCollection(existing, imported *model.Collection, oldNodes map[model.ID]node, report *Report) {
	if fingerprint(existing) != fingerprint(imported) {
		report.Changes.Changed = append(report.Changes.Changed, imported.Name)
	}
	newNodes := indexNodes(imported)
	for id, now := range newNodes {
		before, ok := oldNodes[id]
		switch {
		case !ok:
			report.Changes.Added = append(report.Changes.Added, now.path)
		case before.parent != now.parent || fingerprint(before.item) != fingerprint(now.item):
			report.Changes.Changed = append(report.Changes.Changed, now.path)
		}
	}
	for id, before := range oldNodes {
		if _, ok := newNodes[id]; !ok {
			report.Changes.Removed = append(report.Changes.Removed, before.path)
		}
	}
}

// keepOAuthTokens carries a token the user fetched locally over to the
// imported node with the same ID when the export has an OAuth 2.0 block
// without a token, so re-importing does not orphan or delete it (R2: tokens
// are local, user-triggered state).
func keepOAuthTokens(oldNodes map[model.ID]node, imported *model.Collection) {
	carry := func(kept, fresh *model.Auth) {
		if kept == nil || kept.OAuth2 == nil || kept.OAuth2.Token == nil ||
			fresh == nil || fresh.OAuth2 == nil || fresh.OAuth2.Token != nil {
			return
		}
		token := *kept.OAuth2.Token
		fresh.OAuth2.Token = &token
	}
	_ = imported.Walk(func(item model.Item, _ model.Container, _ int) error {
		before, ok := oldNodes[item.ItemID()]
		if !ok {
			return nil
		}
		switch now := item.(type) {
		case *model.Folder:
			if old, ok := before.item.(*model.Folder); ok {
				carry(old.Auth, now.Auth)
			}
		case *model.Request:
			if old, ok := before.item.(*model.Request); ok {
				carry(old.Auth, now.Auth)
			}
		}
		return nil
	})
}

func mergeEnvironment(workspace *model.Workspace, imported *model.Environment, report *Report) {
	path := "Environment › " + imported.Name
	for index, existing := range workspace.Environments {
		if existing.ID != imported.ID {
			continue
		}
		if fingerprint(existing) != fingerprint(imported) {
			report.Changes.Changed = append(report.Changes.Changed, path)
		}
		workspace.Environments[index] = imported
		return
	}
	workspace.Environments = append(workspace.Environments, imported)
	report.Changes.Added = append(report.Changes.Added, path)
}

func mergeGlobals(workspace *model.Workspace, imported *model.Globals, report *Report) {
	switch {
	case workspace.Globals == nil:
		report.Changes.Added = append(report.Changes.Added, "Globals")
	case fingerprint(workspace.Globals) != fingerprint(imported):
		report.Changes.Changed = append(report.Changes.Changed, "Globals")
	}
	workspace.Globals = imported
}

// fingerprint is the saved form of one node, without its children, as text.
// Two nodes with the same fingerprint would write the same files, so the
// report calls them unchanged. It ignores what the store derives or keeps in
// the Keychain: script file names, secret values and references, OAuth 2.0
// token values and references. Serialising through the on-disk YAML tags also
// makes nil and empty lists equal.
func fingerprint(value any) string {
	var scripts []*model.Script
	var text string
	switch typed := value.(type) {
	case *model.Request:
		c := typed.Clone()
		scrubScripts(&c.Scripts)
		scripts = []*model.Script{c.Scripts.PreRequest, c.Scripts.Test}
		scrubAuth(c.Auth)
		text = marshalNode(c)
	case *model.Folder:
		c := typed.Clone()
		c.Items = nil
		scrubScripts(&c.Scripts)
		scripts = []*model.Script{c.Scripts.PreRequest, c.Scripts.Test}
		scrubAuth(c.Auth)
		scrubVariables(c.Variables)
		text = marshalNode(c)
	case *model.Collection:
		c := typed.Clone()
		c.Items = nil
		scrubScripts(&c.Scripts)
		scripts = []*model.Script{c.Scripts.PreRequest, c.Scripts.Test}
		scrubAuth(c.Auth)
		scrubVariables(c.Variables)
		text = marshalNode(c)
	case *model.Environment:
		c := typed.Clone()
		scrubVariables(c.Variables)
		text = marshalNode(c)
	case *model.Globals:
		c := typed.Clone()
		scrubVariables(c.Variables)
		text = marshalNode(c)
	}
	var out strings.Builder
	out.WriteString(text)
	for _, script := range scripts {
		if script != nil {
			// Source is not serialised in the YAML (it lives in a .js file).
			out.WriteString("\x00" + script.Source)
		}
	}
	return out.String()
}

func marshalNode(value any) string {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}

// scrubScripts clears the store-derived file names of a node's scripts.
func scrubScripts(scripts *model.Scripts) {
	for _, script := range []*model.Script{scripts.PreRequest, scripts.Test} {
		if script != nil {
			script.File = ""
		}
	}
}

func scrubVariables(variables []model.Variable) {
	for i := range variables {
		if variables[i].IsSecret() {
			variables[i].Value, variables[i].SecretRef = "", ""
		}
	}
}

func scrubAuth(auth *model.Auth) {
	if auth == nil || auth.OAuth2 == nil || auth.OAuth2.Token == nil {
		return
	}
	token := auth.OAuth2.Token
	token.AccessToken, token.RefreshToken, token.IDToken, token.Ref = "", "", "", ""
}

// AddLocalCoverage adds privacy-safe cache gaps to a report.
func AddLocalCoverage(report *Report, coverage postmanlocal.Coverage) {
	for _, collection := range coverage.PerCollection {
		if collection.MissingBodies > 0 {
			report.AddWarning(collection.Name, "local-missing-bodies",
				strconv.Itoa(collection.MissingBodies)+" request bodies were absent from the local cache")
		}
		if collection.Unresolved > 0 {
			report.AddWarning(collection.Name, "local-unresolved",
				strconv.Itoa(collection.Unresolved)+" cached references could not be resolved")
		}
	}
	unassigned := coverage.Unresolved
	for _, collection := range coverage.PerCollection {
		unassigned -= collection.Unresolved
	}
	if unassigned > 0 {
		report.AddWarning("", "local-unresolved",
			strconv.Itoa(unassigned)+" local cache records could not be decoded")
	}
	report.sort()
}
