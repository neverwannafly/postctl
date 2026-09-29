package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/shubham/postctl/internal/importer/postmanlocal"
)

func main() {
	out := flag.String("out", "", "staging output directory")
	list := flag.Bool("list", false, "list collection names and coverage without writing")
	dryRun := flag.Bool("dry-run", false, "extract and report coverage without writing")
	partition := flag.String("partition", "", "Postman partition UUID override")
	flag.Parse()

	install, err := postmanlocal.Locate()
	if err != nil {
		fatal(err)
	}
	result, err := postmanlocal.Extract(context.Background(), install, postmanlocal.Options{
		Partition: *partition,
	})
	if err != nil {
		fatal(err)
	}
	printCoverage(result.Coverage)
	if *list || *dryRun {
		return
	}
	if *out == "" {
		fatal(fmt.Errorf("--out is required unless --list or --dry-run is used"))
	}
	if err := result.WriteStaging(*out); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote staging files to %s\n", *out)
}

func printCoverage(coverage postmanlocal.Coverage) {
	fmt.Printf(
		"collections=%d folders=%d requests=%d requests_with_prerequest=%d requests_with_tests=%d examples=%d missing_bodies=%d unresolved=%d environments=%d globals=%d\n",
		coverage.Collections,
		coverage.Folders,
		coverage.Requests,
		coverage.RequestsWithPreRequestScripts,
		coverage.RequestsWithTestScripts,
		coverage.Examples,
		coverage.MissingBodies,
		coverage.Unresolved,
		coverage.Environments,
		coverage.Globals,
	)
	for _, collection := range coverage.PerCollection {
		fmt.Printf(
			"%s: folders=%d requests=%d requests_with_prerequest=%d requests_with_tests=%d examples=%d missing_bodies=%d unresolved=%d\n",
			collection.Name,
			collection.Folders,
			collection.Requests,
			collection.RequestsWithPreRequestScripts,
			collection.RequestsWithTestScripts,
			collection.Examples,
			collection.MissingBodies,
			collection.Unresolved,
		)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "postman-extract:", err)
	os.Exit(1)
}
