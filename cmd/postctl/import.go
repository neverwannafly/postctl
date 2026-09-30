package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shubham/postctl/internal/importer/postman"
	"github.com/shubham/postctl/internal/importer/postmanlocal"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

const exitPartial = 2

type importOptions struct {
	workspace          string
	partition          string
	dryRun             bool
	json               bool
	keepLiteralSecrets bool
	noMarkSecrets      bool
	paths              []string
	envPaths           []string
	globalsPath        string
}

// convertOptions maps the command-line flags to importer options.
func (o importOptions) convertOptions() postman.Options {
	return postman.Options{
		KeepLiteralSecrets: o.keepLiteralSecrets,
		NoMarkSecrets:      o.noMarkSecrets,
	}
}

func runImport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || hasHelpFlag(args) {
		importUsage(stdout)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	args, commandWorkspace, err := extractImportWorkspace(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v. Run: postctl import --help\n", err)
		return exitUsage
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Missing import source. Use postman or postman-local.")
		return exitUsage
	}
	source := args[0]
	options, err := parseImportOptions(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "%v. Run: postctl import --help\n", err)
		return exitUsage
	}
	if commandWorkspace != "" {
		options.workspace = commandWorkspace
	}
	switch source {
	case "postman":
		return importPostmanFiles(options, stdout, stderr)
	case "postman-local":
		if len(options.paths) > 0 || len(options.envPaths) > 0 || options.globalsPath != "" {
			fmt.Fprintln(stderr, "postman-local does not accept JSON input paths")
			return exitUsage
		}
		return importPostmanLocal(options, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown import source %q. Use postman or postman-local.\n", source)
		return exitUsage
	}
}

func extractImportWorkspace(args []string) ([]string, string, error) {
	out := make([]string, 0, len(args))
	var workspace string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--workspace" || arg == "-w":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return nil, "", fmt.Errorf("%s requires a value", arg)
			}
			i++
			workspace = args[i]
		case strings.HasPrefix(arg, "--workspace="):
			workspace = strings.TrimPrefix(arg, "--workspace=")
			if strings.TrimSpace(workspace) == "" {
				return nil, "", fmt.Errorf("--workspace requires a value")
			}
		case strings.HasPrefix(arg, "-w="):
			workspace = strings.TrimPrefix(arg, "-w=")
			if strings.TrimSpace(workspace) == "" {
				return nil, "", fmt.Errorf("-w requires a value")
			}
		default:
			out = append(out, arg)
		}
	}
	return out, workspace, nil
}

func parseImportOptions(args []string) (importOptions, error) {
	var result importOptions
	var err error
	takeValue := func(index *int, current, name string) (string, error) {
		if value, ok := strings.CutPrefix(current, name+"="); ok {
			if value == "" {
				return "", fmt.Errorf("%s requires a value", name)
			}
			return value, nil
		}
		*index++
		if *index >= len(args) {
			return "", fmt.Errorf("%s requires a value", name)
		}
		return args[*index], nil
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--dry-run":
			result.dryRun = true
		case arg == "--json":
			result.json = true
		case arg == "--keep-literal-secrets":
			result.keepLiteralSecrets = true
		case arg == "--no-mark-secrets":
			result.noMarkSecrets = true
		case arg == "--workspace" || strings.HasPrefix(arg, "--workspace="):
			result.workspace, err = takeValue(&index, arg, "--workspace")
		case arg == "-w" || strings.HasPrefix(arg, "-w="):
			result.workspace, err = takeValue(&index, arg, "-w")
		case arg == "--partition" || strings.HasPrefix(arg, "--partition="):
			result.partition, err = takeValue(&index, arg, "--partition")
		case arg == "--env" || strings.HasPrefix(arg, "--env="):
			var value string
			value, err = takeValue(&index, arg, "--env")
			result.envPaths = append(result.envPaths, value)
		case arg == "--globals" || strings.HasPrefix(arg, "--globals="):
			result.globalsPath, err = takeValue(&index, arg, "--globals")
		case strings.HasPrefix(arg, "-"):
			err = fmt.Errorf("unknown import flag %q", arg)
		default:
			result.paths = append(result.paths, arg)
		}
		if err != nil {
			return importOptions{}, err
		}
	}
	return result, nil
}

// importSecrets returns the secret store imports write to: the Keychain.
// Tests replace it so they never touch the real one.
var importSecrets = func() store.SecretStore { return store.NewKeychain("") }

func importPostmanFiles(options importOptions, stdout, stderr io.Writer) int {
	paths := append([]string{}, options.paths...)
	paths = append(paths, options.envPaths...)
	if options.globalsPath != "" {
		paths = append(paths, options.globalsPath)
	}
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "postctl import postman requires at least one JSON file or directory")
		return exitUsage
	}
	files, discoveryFailures := collectJSONFiles(paths)
	report := postman.Report{DryRun: options.dryRun}
	for _, failure := range discoveryFailures {
		report.AddFailure(filepath.Base(failure.source), failure.message)
	}
	var documents []*postman.Document
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			report.AddFailure(filepath.Base(path), "could not read input")
			continue
		}
		document, converted, err := postman.Convert(data, options.convertOptions())
		if err != nil {
			report.AddFailure(filepath.Base(path), err.Error())
			continue
		}
		documents = append(documents, document)
		report.Merge(converted)
	}
	return finishImport(options, documents, report, stdout, stderr)
}

func importPostmanLocal(options importOptions, stdout, stderr io.Writer) int {
	install, err := postmanlocal.Locate()
	if err != nil {
		fmt.Fprintln(stderr, "Could not locate the local Postman data")
		return exitUsage
	}
	extracted, err := postmanlocal.Extract(context.Background(), install, postmanlocal.Options{
		Partition: options.partition,
	})
	if err != nil {
		fmt.Fprintln(stderr, "Could not extract the local Postman cache")
		return exitPartial
	}
	report := postman.Report{DryRun: options.dryRun}
	var documents []*postman.Document
	add := func(raw postmanlocal.Document, label string) {
		document, converted, convertErr := postman.ConvertObject(map[string]any(raw), options.convertOptions())
		if convertErr != nil {
			report.AddFailure(label, convertErr.Error())
			return
		}
		documents = append(documents, document)
		report.Merge(converted)
	}
	for index, collection := range extracted.Collections {
		add(collection, fmt.Sprintf("local collection %d", index+1))
	}
	for index, environment := range extracted.Environments {
		add(environment, fmt.Sprintf("local environment %d", index+1))
	}
	if extracted.Globals != nil {
		add(extracted.Globals, "local globals")
	}
	postman.AddLocalCoverage(&report, extracted.Coverage)
	return finishImport(options, documents, report, stdout, stderr)
}

func finishImport(options importOptions, documents []*postman.Document, report postman.Report, stdout, stderr io.Writer) int {
	if len(documents) == 0 {
		writeImportReport(report, options.json, stdout)
		if len(report.Failures) > 0 {
			return exitPartial
		}
		fmt.Fprintln(stderr, "No supported Postman documents were found")
		return exitUsage
	}
	workspace, workspaceStore, root, err := openImportWorkspace(options.workspace)
	if err != nil {
		fmt.Fprintln(stderr, "Could not open the target workspace")
		return exitUsage
	}
	replaced := collectionsBeingReplaced(workspace, documents)
	postman.MergeDocuments(workspace, documents, &report)
	if !options.dryRun {
		if workspaceStore == nil {
			workspaceStore, err = store.Init(root, filepath.Base(root),
				store.WithSecretStore(importSecrets()))
			if err != nil {
				fmt.Fprintln(stderr, "Could not create the target workspace")
				return exitUsage
			}
		}
		if err := workspaceStore.Save(context.Background(), workspace); err != nil {
			fmt.Fprintln(stderr, "Import could not be saved")
			return exitPartial
		}
		// Items the export dropped are gone from disk; drop their Keychain
		// secrets too, so nothing credential-shaped outlives its node.
		for _, old := range replaced {
			workspaceStore.DeleteRemovedSecrets(context.Background(), old)
		}
	}
	writeImportReport(report, options.json, stdout)
	if len(report.Failures) > 0 {
		return exitPartial
	}
	return exitOK
}

// collectionsBeingReplaced returns copies of the workspace's collections that a
// document is about to overwrite, taken before the merge.
func collectionsBeingReplaced(workspace *model.Workspace, documents []*postman.Document) []*model.Collection {
	incoming := map[model.ID]bool{}
	for _, document := range documents {
		if document.Kind == postman.KindCollection {
			incoming[document.Collection.ID] = true
		}
	}
	var out []*model.Collection
	for _, collection := range workspace.Collections {
		if incoming[collection.ID] {
			out = append(out, collection.Clone())
		}
	}
	return out
}

func openImportWorkspace(explicit string) (*model.Workspace, *store.Store, string, error) {
	root := explicit
	if root == "" {
		root = os.Getenv("POSTCTL_WORKSPACE")
	}
	initialized := false
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, "", err
		}
		location, err := store.Discover(cwd)
		if err != nil {
			return nil, nil, "", err
		}
		root, initialized = location.Root, location.Initialized
	} else {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, nil, "", err
		}
		root = absolute
		_, statErr := os.Stat(filepath.Join(root, "postctl.yaml"))
		initialized = statErr == nil
	}
	if !initialized {
		return &model.Workspace{ID: model.NewID(), Name: filepath.Base(root)}, nil, root, nil
	}
	workspaceStore, err := store.Open(root, store.WithSecretStore(importSecrets()))
	if err != nil {
		return nil, nil, "", err
	}
	workspace, err := workspaceStore.Load()
	if err != nil {
		return nil, nil, "", err
	}
	return workspace, workspaceStore, root, nil
}

func writeImportReport(report postman.Report, asJSON bool, output io.Writer) {
	if asJSON {
		_ = report.WriteJSON(output)
		return
	}
	report.WriteText(output)
}

type inputFailure struct {
	source  string
	message string
}

func collectJSONFiles(paths []string) ([]string, []inputFailure) {
	var files []string
	var failures []inputFailure
	seen := map[string]bool{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			failures = append(failures, inputFailure{source: path, message: "path does not exist"})
			continue
		}
		if !info.IsDir() {
			if !seen[path] {
				files = append(files, path)
				seen[path] = true
			}
			continue
		}
		walkErr := filepath.WalkDir(path, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
				return nil
			}
			if !seen[filePath] {
				files = append(files, filePath)
				seen[filePath] = true
			}
			return nil
		})
		if walkErr != nil {
			failures = append(failures, inputFailure{source: path, message: "could not read directory"})
		}
	}
	sort.Strings(files)
	return files, failures
}

func importUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  postctl import postman <files or dirs...> [--workspace DIR] [--env FILE] [--globals FILE]")
	fmt.Fprintln(output, "                         [--dry-run] [--json] [--keep-literal-secrets] [--no-mark-secrets]")
	fmt.Fprintln(output, "  postctl import postman-local [--partition ID] [--workspace DIR] [--dry-run] [--json]")
	fmt.Fprintln(output, "                               [--keep-literal-secrets] [--no-mark-secrets]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Variables whose key looks like a credential (token, secret, password, apikey, cookie,")
	fmt.Fprintln(output, "session, jwt, bearer, credential, private key, ...) are marked secret. A value they have")
	fmt.Fprintln(output, "goes to the Keychain, not the YAML. --no-mark-secrets turns that off.")
}
