package postmanlocal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const collectionSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// Extract copies Postman's IndexedDB to a private temporary directory, decodes
// the copy, and returns standard Postman JSON documents.
func Extract(ctx context.Context, install Install, options Options) (*Result, error) {
	selected, err := withPartition(install, options.Partition)
	if err != nil {
		return nil, err
	}
	copyRoot, cleanup, err := copyIndexedDB(selected.IndexedDB)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	paths, err := findLevelDBs(copyRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate copied IndexedDB: %w", err)
	}
	var entities, environments, sessions []map[string]any
	decodeFailures := 0
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reader, err := openIndexedDB(path)
		if err != nil {
			return nil, err
		}
		records, failed, readErr := reader.records(
			func(name string) bool { return strings.HasSuffix(name, "-v3-collections") },
			"v3-collections",
		)
		entities = append(entities, records...)
		decodeFailures += failed
		records, failed, envErr := reader.records(
			func(name string) bool { return strings.HasSuffix(name, "-environments") },
			"environments",
		)
		environments = append(environments, records...)
		decodeFailures += failed
		records, failed, sessionErr := reader.records(
			func(name string) bool { return name == "postman-app" },
			"variable_sessions",
		)
		sessions = append(sessions, records...)
		decodeFailures += failed
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if envErr != nil {
			return nil, envErr
		}
		if sessionErr != nil {
			return nil, sessionErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if len(entities) == 0 && len(environments) == 0 && len(sessions) == 0 {
		return nil, errors.New("no supported Postman records found in active partition")
	}
	result := buildResult(entities, environments, sessions)
	result.Coverage.Unresolved += decodeFailures
	return result, nil
}

func buildResult(records, environmentRecords, sessions []map[string]any) *Result {
	result := &Result{}
	entities := map[string]map[string]any{}
	var roots []map[string]any
	for _, record := range records {
		value := object(record["value"])
		id := text(value["id"])
		if id == "" {
			id = text(record["id"])
		}
		if id == "" {
			continue
		}
		entities[id] = value
	}
	for _, entity := range entities {
		if text(entity["$kind"]) == "collection" && text(entity["parentId"]) == "" {
			roots = append(roots, entity)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return text(roots[i]["name"]) < text(roots[j]["name"]) })
	for _, root := range roots {
		coverage := CollectionCoverage{Name: text(root["name"])}
		document := collectionDocument(root, entities, &coverage)
		result.Collections = append(result.Collections, document)
		result.Coverage.PerCollection = append(result.Coverage.PerCollection, coverage)
		result.Coverage.Folders += coverage.Folders
		result.Coverage.Requests += coverage.Requests
		result.Coverage.RequestsWithPreRequestScripts += coverage.RequestsWithPreRequestScripts
		result.Coverage.RequestsWithTestScripts += coverage.RequestsWithTestScripts
		result.Coverage.Examples += coverage.Examples
		result.Coverage.MissingBodies += coverage.MissingBodies
		result.Coverage.Unresolved += coverage.Unresolved
	}
	result.Coverage.Collections = len(result.Collections)

	seenEnvironments := map[string]bool{}
	for _, record := range environmentRecords {
		value := object(record["value"])
		id := text(value["id"])
		if id == "" || seenEnvironments[id] {
			continue
		}
		seenEnvironments[id] = true
		result.Environments = append(result.Environments, environmentDocument(value))
	}
	sort.Slice(result.Environments, func(i, j int) bool {
		return text(result.Environments[i]["name"]) < text(result.Environments[j]["name"])
	})
	result.Coverage.Environments = len(result.Environments)

	for _, session := range sessions {
		if text(session["model"]) != "globals" {
			continue
		}
		result.Globals = Document{
			"id":                      text(session["modelId"]),
			"name":                    "Globals",
			"values":                  cloneArray(session["values"]),
			"_postman_variable_scope": "globals",
			"_postman_exported_using": "postctl postman-local extractor",
		}
		result.Coverage.Globals = 1
		break
	}
	return result
}

func collectionDocument(root map[string]any, entities map[string]map[string]any, coverage *CollectionCoverage) Document {
	info := map[string]any{
		"_postman_id": text(root["id"]),
		"name":        text(root["name"]),
		"schema":      collectionSchema,
	}
	copyOptional(info, root, "description")
	document := Document{
		"info": info,
		"item": buildItems(root, entities, coverage, map[string]bool{}),
	}
	copyCommon(document, root)
	return document
}

func buildItems(parent map[string]any, entities map[string]map[string]any, coverage *CollectionCoverage, visiting map[string]bool) []any {
	result := make([]any, 0)
	for _, referenceValue := range array(parent["items"]) {
		reference := object(referenceValue)
		id := text(reference["id"])
		entity := entities[id]
		if entity == nil || visiting[id] {
			coverage.Unresolved++
			continue
		}
		visiting[id] = true
		switch text(entity["$kind"]) {
		case "collection":
			item := Document{
				"id":   id,
				"name": text(entity["name"]),
				"item": buildItems(entity, entities, coverage, visiting),
			}
			copyOptional(item, entity, "description")
			copyCommon(item, entity)
			result = append(result, item)
			coverage.Folders++
		case "http-request":
			if text(entity["name"]) == "" || text(entity["method"]) == "" {
				coverage.Unresolved++
				break
			}
			result = append(result, requestItem(entity, entities, coverage))
		default:
			coverage.Unresolved++
		}
		delete(visiting, id)
	}
	return result
}

func requestItem(entity map[string]any, entities map[string]map[string]any, coverage *CollectionCoverage) Document {
	request := Document{
		"method": text(entity["method"]),
		"header": headers(entity["headers"]),
		"url":    text(entity["url"]),
	}
	copyOptional(request, entity, "description")
	if auth := authBlock(entity["auth"]); auth != nil {
		request["auth"] = auth
	}
	if body := bodyBlock(entity["body"]); body != nil {
		request["body"] = body
	} else if methodUsuallyHasBody(text(entity["method"])) {
		coverage.MissingBodies++
	}
	item := Document{
		"id":      text(entity["id"]),
		"name":    text(entity["name"]),
		"request": request,
	}
	copyEvents(item, entity)
	hasPreRequest, hasTest := requestScriptKinds(entity)
	if hasPreRequest {
		coverage.RequestsWithPreRequestScripts++
	}
	if hasTest {
		coverage.RequestsWithTestScripts++
	}
	for _, exampleRef := range array(entity["examples"]) {
		ref := object(exampleRef)
		example := entities[text(ref["id"])]
		if example == nil {
			coverage.Unresolved++
			continue
		}
		// The current cache commonly keeps examples partial. Preserve a decoded
		// response only when one is available instead of inventing fields.
		if response := object(example["response"]); len(response) > 0 {
			item["response"] = append(array(item["response"]), response)
			coverage.Examples++
		}
	}
	coverage.Requests++
	return item
}

func copyCommon(target Document, source map[string]any) {
	if auth := authBlock(source["auth"]); auth != nil {
		target["auth"] = auth
	}
	if variables := array(source["variables"]); len(variables) > 0 {
		target["variable"] = cloneArray(variables)
	}
	copyEvents(target, source)
}

func copyEvents(target Document, source map[string]any) {
	var events []any
	for _, raw := range array(source["scripts"]) {
		script := object(raw)
		listen := ""
		switch text(script["type"]) {
		case "beforeRequest":
			listen = "prerequest"
		case "afterResponse", "test":
			listen = "test"
		default:
			continue
		}
		code := text(script["code"])
		events = append(events, map[string]any{
			"listen": listen,
			"script": map[string]any{
				"type": "text/javascript",
				"exec": strings.Split(code, "\n"),
			},
		})
	}
	if len(events) > 0 {
		target["event"] = events
	}
}

func requestScriptKinds(source map[string]any) (preRequest, test bool) {
	for _, raw := range array(source["scripts"]) {
		switch text(object(raw)["type"]) {
		case "beforeRequest":
			preRequest = true
		case "afterResponse", "test":
			test = true
		}
	}
	return preRequest, test
}

func headers(value any) []any {
	var result []any
	for _, raw := range array(value) {
		header := object(raw)
		out := map[string]any{"key": text(header["key"]), "value": text(header["value"])}
		copyOptional(out, header, "disabled", "description", "type")
		result = append(result, out)
	}
	return result
}

func authBlock(value any) map[string]any {
	auth := object(value)
	authType := text(auth["type"])
	if authType == "" {
		return nil
	}
	result := map[string]any{"type": authType}
	if authType == "noauth" {
		return result
	}
	result[authType] = cloneArray(auth["credentials"])
	return result
}

func bodyBlock(value any) map[string]any {
	body := object(value)
	switch text(body["type"]) {
	case "json", "text", "xml", "javascript":
		return map[string]any{
			"mode": "raw",
			"raw":  text(body["content"]),
			"options": map[string]any{"raw": map[string]any{
				"language": text(body["type"]),
			}},
		}
	case "formdata":
		return map[string]any{"mode": "formdata", "formdata": cloneArray(body["content"])}
	case "urlencoded":
		return map[string]any{"mode": "urlencoded", "urlencoded": cloneArray(body["content"])}
	default:
		return nil
	}
}

func environmentDocument(value map[string]any) Document {
	return Document{
		"id":                      text(value["id"]),
		"name":                    text(value["name"]),
		"values":                  cloneArray(value["values"]),
		"_postman_variable_scope": "environment",
		"_postman_exported_using": "postctl postman-local extractor",
	}
}

func object(value any) map[string]any {
	switch result := value.(type) {
	case map[string]any:
		return result
	case Document:
		return map[string]any(result)
	default:
		return nil
	}
}

func array(value any) []any {
	result, _ := value.([]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func cloneArray(value any) []any {
	source := array(value)
	result := make([]any, len(source))
	copy(result, source)
	return result
}

func copyOptional(target map[string]any, source map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := source[key]; ok && value != nil {
			target[key] = value
		}
	}
}

func methodUsuallyHasBody(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// PrivateStagingDir returns the conventional private acceptance-test path.
func PrivateStagingDir(repoRoot string) string {
	return filepath.Join(repoRoot, "internal", "importer", "postmanlocal", "testdata", "private", "staging")
}
