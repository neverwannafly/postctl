package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/model"
)

func runRun(ctx context.Context, a *app, args []string) int {
	fs := a.flagSet()
	envName := fs.String("e", "", "environment name or id (default: the active environment)")
	fs.StringVar(envName, "env", "", "environment name or id")
	dataFile := fs.String("d", "", "iteration data file (.csv or .json)")
	iterations := fs.Int("n", 0, "iteration count (default: one per data row, or 1)")
	delay := fs.Duration("delay", 0, "pause between requests")
	bail := fs.Bool("bail", false, "stop after the first failed request")
	reporter := fs.String("reporter", "", "reporters to use: cli, json, junit (default: cli)")
	jsonExport := fs.String("reporter-json-export", "", "write NDJSON events to FILE")
	junitExport := fs.String("reporter-junit-export", "", "write JUnit XML to FILE")
	timeout := fs.Duration("timeout-request", 0, "per-request timeout")
	noHistory := fs.Bool("no-history", false, "do not record requests in history")
	asJSON := fs.Bool("json", false, "stream NDJSON events on stdout")
	positional, code := a.parse(fs, args)
	if code >= 0 {
		return code
	}
	path, code := a.onePath(positional)
	if code >= 0 {
		return code
	}
	if *iterations < 0 {
		fmt.Fprintln(a.err, "-n must be zero or positive.")
		return ExitUsage
	}
	names, err := parseReporters(*reporter, *asJSON)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return ExitUsage
	}
	if code := a.open(); code >= 0 {
		return code
	}
	n, err := resolvePath(a.ws, path)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return ExitUsage
	}
	if n.kind() == "request" {
		fmt.Fprintf(a.err, "%q is a request. postctl run takes a collection or folder. Use postctl send for one request.\n", path)
		return ExitUsage
	}
	env, code := a.lookupEnv(*envName)
	if code >= 0 {
		return code
	}
	var rows []map[string]any
	if *dataFile != "" {
		rows, err = engine.LoadDataFile(*dataFile)
		if err != nil {
			fmt.Fprintf(a.err, "Cannot read data file: %v\n", err)
			return ExitUsage
		}
	}
	col := n.col.Clone()
	if *timeout > 0 {
		applyTimeout(col, *timeout)
	}
	var folderID model.ID
	if n.kind() == "folder" {
		folderID = n.folder().ID
	}
	methods := map[model.ID]string{}
	_ = col.Walk(func(it model.Item, _ model.Container, _ int) error {
		if r, ok := it.(*model.Request); ok {
			methods[r.ID] = r.Method
		}
		return nil
	})
	reps, cleanup, err := openReporters(a, names, *jsonExport, *junitExport, methods)
	if err != nil {
		fmt.Fprintln(a.err, err.Error())
		return ExitUsage
	}
	defer cleanup()
	eng, code := a.newEngine(*noHistory)
	if code >= 0 {
		return code
	}
	events, err := eng.RunAsync(ctx, engine.RunInput{
		Collection:  col,
		FolderID:    folderID,
		Environment: env,
		Globals:     a.ws.Globals,
		Iterations:  *iterations,
		Data:        rows,
		Delay:       *delay,
		Bail:        *bail,
	})
	if err != nil {
		fmt.Fprintf(a.err, "Cannot run: %v\n", a.mask(err.Error()))
		return ExitUsage
	}
	var summary *engine.RunSummary
	for ev := range events {
		if fin, ok := ev.(engine.RunFinished); ok {
			summary = fin.Summary
		}
		for _, rep := range reps {
			if err := rep.Event(ev); err != nil {
				fmt.Fprintf(a.err, "Reporter failed: %v\n", err)
				return ExitRequest
			}
		}
	}
	return runExit(summary)
}

func applyTimeout(col *model.Collection, d time.Duration) {
	_ = col.Walk(func(it model.Item, _ model.Container, _ int) error {
		if r, ok := it.(*model.Request); ok {
			r.Settings.Timeout = &d
		}
		return nil
	})
}
