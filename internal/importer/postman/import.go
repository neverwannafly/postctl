package postman

import (
	"reflect"
	"strconv"

	"github.com/shubham/postctl/internal/importer/postmanlocal"
	"github.com/shubham/postctl/internal/model"
)

// MergeDocuments mutates workspace by overwriting source-matched nodes by
// stable Postman ID. Existing nodes absent from the source are retained.
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
		importedIDs := map[model.ID]bool{}
		collectItemIDs(imported.Items, importedIDs)
		mergeItems(existing.Items, &imported.Items, importedIDs, imported.Name, report)
		if !reflect.DeepEqual(existing, imported) {
			report.Changes.Changed = append(report.Changes.Changed, imported.Name)
		}
		workspace.Collections[index] = imported
		return
	}
	workspace.Collections = append(workspace.Collections, imported)
	report.Changes.Added = append(report.Changes.Added, imported.Name)
}

func mergeItems(
	existing []model.Item,
	imported *[]model.Item,
	importedIDs map[model.ID]bool,
	parentPath string,
	report *Report,
) {
	oldByID := make(map[model.ID]model.Item, len(existing))
	for _, item := range existing {
		oldByID[item.ItemID()] = item
	}
	for _, item := range *imported {
		path := joinPath(parentPath, item.ItemName())
		old := oldByID[item.ItemID()]
		if old == nil {
			report.Changes.Added = append(report.Changes.Added, path)
			continue
		}
		newFolder, newIsFolder := item.(*model.Folder)
		oldFolder, oldIsFolder := old.(*model.Folder)
		if newIsFolder && oldIsFolder {
			mergeItems(oldFolder.Items, &newFolder.Items, importedIDs, path, report)
		}
		if !reflect.DeepEqual(old, item) {
			report.Changes.Changed = append(report.Changes.Changed, path)
		}
	}
	for _, old := range existing {
		if importedIDs[old.ItemID()] {
			continue
		}
		*imported = append(*imported, old)
		report.Changes.Kept = append(report.Changes.Kept, joinPath(parentPath, old.ItemName()))
	}
}

func collectItemIDs(items []model.Item, ids map[model.ID]bool) {
	for _, item := range items {
		ids[item.ItemID()] = true
		if folder, ok := item.(*model.Folder); ok {
			collectItemIDs(folder.Items, ids)
		}
	}
}

func mergeEnvironment(workspace *model.Workspace, imported *model.Environment, report *Report) {
	path := "Environment › " + imported.Name
	for index, existing := range workspace.Environments {
		if existing.ID != imported.ID {
			continue
		}
		if !reflect.DeepEqual(existing, imported) {
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
	case !reflect.DeepEqual(workspace.Globals, imported):
		report.Changes.Changed = append(report.Changes.Changed, "Globals")
	}
	workspace.Globals = imported
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
