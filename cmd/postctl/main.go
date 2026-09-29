// Command postctl is a keyboard-first terminal replacement for Postman.
//
// This is the WS0 scaffold: flag handling and subcommand dispatch exist, and
// every command reports "not implemented yet" (exit status 3) until its
// workstream lands.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

// Exit codes (plan §9).
const (
	exitOK    = 0
	exitUsage = 3 // usage, configuration, and "not implemented" errors
)

type command struct {
	name    string
	usage   string
	summary string
}

// commands is the subcommand table; behavior lands in later workstreams.
var commands = []command{
	{"import", "import postman <files...> [--workspace DIR] [--env FILE...] [--globals FILE] [--dry-run]", "Import Postman collections, environments and globals."},
	{"ls", "ls [collection[/folder]] [--json]", "List collections, folders and requests."},
	{"send", "send <collection/path/request> [-e env] [--var k=v] [--json]", "Send one request and print the response and test results."},
	{"run", "run <collection[/folder]> [-e env] [-d data.csv] [-n iterations] [--reporter cli|json|junit] [--bail]", "Run a collection or folder, like newman."},
	{"env", "env ls|use|set|get", "List, select and edit environments."},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes postctl with args (excluding the program name) and returns the
// process exit status.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("postctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "print the version and exit")
	workspace := fs.String("workspace", "", "use this workspace directory")
	fs.Bool("help", false, "show help")
	fs.Bool("h", false, "show help")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout)
			return exitOK
		}
		fmt.Fprintf(stderr, "%v. Run: postctl --help\n", err)
		return exitUsage
	}
	help := false
	fs.Visit(func(f *flag.Flag) {
		if (f.Name == "help" || f.Name == "h") && f.Value.String() == "true" {
			help = true
		}
	})
	switch {
	case *showVersion:
		fmt.Fprintf(stdout, "postctl %s\n", version)
		return exitOK
	case help:
		usage(stdout)
		return exitOK
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "The interactive UI is not implemented yet. Run: postctl --help")
		return exitUsage
	}
	if rest[0] == "help" {
		usage(stdout)
		return exitOK
	}
	if rest[0] == "import" {
		importArgs := rest[1:]
		if *workspace != "" && len(importArgs) > 0 {
			importArgs = append([]string{importArgs[0], "--workspace", *workspace}, importArgs[1:]...)
		}
		return runImport(importArgs, stdout, stderr)
	}
	for _, c := range commands {
		if c.name == rest[0] {
			if hasHelpFlag(rest[1:]) {
				fmt.Fprintf(stdout, "Usage: postctl %s\n\n%s\n", c.usage, c.summary)
				return exitOK
			}
			fmt.Fprintf(stderr, "postctl %s is not implemented yet.\n", c.name)
			return exitUsage
		}
	}
	fmt.Fprintf(stderr, "Unknown command %q. Commands: %s. Run: postctl --help\n", rest[0], strings.Join(names(), ", "))
	return exitUsage
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "-help" {
			return true
		}
	}
	return false
}

func names() []string {
	out := make([]string, len(commands))
	for i, c := range commands {
		out[i] = c.name
	}
	return out
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "postctl - a terminal API client for Postman collections")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  postctl                 Open the interactive UI (not implemented yet)")
	for _, c := range commands {
		fmt.Fprintf(w, "  postctl %s\n      %s\n", c.usage, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --version   Print the version and exit")
	fmt.Fprintln(w, "  --workspace Use this workspace directory")
	fmt.Fprintln(w, "  --help      Show this help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok, 1 test failures, 2 request errors, 3 usage or configuration errors.")
}
