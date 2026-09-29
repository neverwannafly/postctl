// Command postctl is a keyboard-first terminal replacement for Postman.
//
// With no subcommand it opens the interactive UI. ls, send, run, env and curl
// are the linear CLI. import loads Postman documents.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/shubham/postctl/internal/cli"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/response"
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
	{"ls", "ls [path] [--flat] [--json]", "List collections, folders and requests."},
	{"send", "send <path> [-e env] [--var k=v] [--json] [--include] [--verbose] [--no-scripts] [--no-history]", "Send one request and print the status, body and tests."},
	{"run", "run <path> [-e env] [-d file] [-n N] [--delay 200ms] [--bail] [--reporter cli,json,junit] [--json]", "Run a collection or folder, like newman."},
	{"env", "env ls | use <name> | show <name> [--reveal] | set <name> <key> <value> [--secret] | unset <name> <key>", "List, select and edit environments."},
	{"curl", "curl <path> [-e env] [--with-secrets]", "Print the resolved request as a curl command."},
}

// launchUI starts the interactive UI; tests replace it.
var launchUI = tui.Run

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes postctl with args (excluding the program name) and returns the
// process exit status.
func run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fs := flag.NewFlagSet("postctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Bool("help", false, "show help")
	fs.Bool("h", false, "show help")
	workspace := fs.String("workspace", "", "workspace directory (default: found from the current directory)")
	accessible := fs.Bool("accessible", false, "linear, screen-reader friendly presentation")
	ascii := fs.Bool("ascii", false, "use ASCII instead of Unicode glyphs")
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
		return openUI(*workspace, *accessible, *ascii, stderr)
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
	switch rest[0] {
	case "ls", "send", "run", "env", "curl":
		if hasHelpFlag(rest[1:]) {
			for _, c := range commands {
				if c.name == rest[0] {
					fmt.Fprintf(stdout, "Usage: postctl %s\n\n%s\n", c.usage, c.summary)
					return exitOK
				}
			}
		}
		return cli.Execute(ctx, cli.Options{
			Workspace: *workspace,
			ASCII:     *ascii || *accessible || truthy(os.Getenv("POSTCTL_ASCII")),
			NoColor:   os.Getenv("NO_COLOR") != "" || *accessible,
			Stdout:    stdout,
			Stderr:    stderr,
			Version:   version,
		}, rest)
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

// openUI resolves the workspace and runs the interactive UI. NO_COLOR,
// POSTCTL_ACCESSIBLE and POSTCTL_ASCII are honored by tui.Run.
func openUI(workspace string, accessible, ascii bool, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "Cannot read the current directory: %v. Pass --workspace DIR.\n", err)
		return exitUsage
	}
	// --workspace wins, then POSTCTL_WORKSPACE, then discovery (plan §0, D1).
	loc, err := store.Discover(cwd, store.WithWorkspace(workspace))
	if err != nil {
		fmt.Fprintf(stderr, "Cannot find a workspace: %v. Pass --workspace DIR.\n", err)
		return exitUsage
	}
	root := loc.Root
	root, err = filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(stderr, "Invalid workspace path: %v\n", err)
		return exitUsage
	}
	err = launchUI(tui.Options{
		Backend:         tui.NewStoreBackend(root, store.WithSecretStore(store.NewKeychain(""))),
		Accessible:      accessible,
		ASCII:           ascii,
		NewResponsePane: response.New,
	})
	if err != nil {
		fmt.Fprintf(stderr, "Could not run the interactive UI: %v. It needs a terminal; for scripts use the subcommands (postctl --help).\n", err)
		return exitUsage
	}
	return exitOK
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "y", "yes":
		return true
	default:
		return false
	}
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
	fmt.Fprintln(w, "  postctl [--workspace DIR] [--accessible] [--ascii]")
	fmt.Fprintln(w, "      Open the interactive UI.")
	for _, c := range commands {
		fmt.Fprintf(w, "  postctl %s\n      %s\n", c.usage, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --version   Print the version and exit")
	fmt.Fprintln(w, "  --workspace Use this workspace directory")
	fmt.Fprintln(w, "  --help      Show this help")
	fmt.Fprintln(w, "  --workspace DIR  Workspace to open (or POSTCTL_WORKSPACE); default: found from the current directory")
	fmt.Fprintln(w, "  --accessible     Linear presentation, no alternate screen or animation (or POSTCTL_ACCESSIBLE=1)")
	fmt.Fprintln(w, "  --ascii          ASCII glyphs instead of Unicode (or POSTCTL_ASCII=1)")
	fmt.Fprintln(w, "  NO_COLOR         Set to disable color")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok, 1 test failures, 2 request errors, 3 usage or configuration errors.")
}
