package vars

import "github.com/shubham/postctl/internal/model"

// Apply writes persisted-scope changes back into model values. Existing
// variable metadata, including secret type and reference, is preserved.
func Apply(
	workspace *model.Workspace,
	environment *model.Environment,
	collection *model.Collection,
	changes map[Scope][]Change,
) {
	if workspace != nil {
		if workspace.Globals == nil && len(changes[Global]) > 0 {
			workspace.Globals = &model.Globals{
				ID:   model.NewID(),
				Name: "Globals",
			}
		}
		if workspace.Globals != nil {
			workspace.Globals.Variables = applyChanges(workspace.Globals.Variables, changes[Global])
		}
	}
	if collection != nil {
		collection.Variables = applyChanges(collection.Variables, changes[Collection])
	}
	if environment != nil {
		environment.Variables = applyChanges(environment.Variables, changes[Environment])
	}
}

func applyChanges(variables []model.Variable, changes []Change) []model.Variable {
	result := append([]model.Variable(nil), variables...)
	for _, change := range changes {
		index := -1
		for i := range result {
			if result[i].Key == change.Key {
				index = i
				break
			}
		}
		if change.Unset {
			if index >= 0 {
				result = append(result[:index], result[index+1:]...)
			}
			continue
		}
		value := stringify(change.Value)
		if index >= 0 {
			result[index].Value = value
			result[index].Disabled = false
			continue
		}
		result = append(result, model.Variable{Key: change.Key, Value: value})
	}
	return result
}
