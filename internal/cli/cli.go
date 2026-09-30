package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/ui"
)

// Exit codes from plan §9.
const (
	// ExitOK means the command finished and every test passed.
	ExitOK = 0
	// ExitTests means a test or a script failed.
	ExitTests = 1
	// ExitRequest means a request or the transport failed.
	ExitRequest = 2
	// ExitUsage means the arguments, workspace, or configuration are wrong.
	ExitUsage = 3
)

// Options configures one CLI invocation. A nil writer falls back to the
// standard stream. Store, when set, skips workspace discovery so tests can
// pass a temp workspace.
type Options struct {
	// Workspace is the --workspace directory. Empty uses discovery.
	Workspace string
	// ASCII selects the ASCII glyph set (--ascii or POSTCTL_ASCII).
	ASCII bool
	// NoColor disables ANSI color (NO_COLOR, --accessible, or a non-terminal).
	NoColor bool
	// Stdin reads confirmations such as env show --reveal, and the secret
	// value of env set --secret when it is not on the command line.
	Stdin io.Reader
	// Stdout receives command output.
	Stdout io.Writer
	// Stderr receives usage errors and warnings.
	Stderr io.Writer
	// Version is sent as the User-Agent when a request is made.
	Version string
	// Store, when set, is the workspace to use instead of discovery.
	Store *store.Store
	// IsTTY reports whether v is an interactive terminal. Nil checks the
	// file mode of an *os.File. v may be stdin or stdout.
	IsTTY func(v any) bool
	// ReadPassword reads one line from the terminal without echoing it, for
	// env set --secret without a value. Nil reads from Stdin (an *os.File)
	// with the terminal's echo turned off. It is called only when IsTTY
	// reports that Stdin is a terminal.
	ReadPassword func() ([]byte, error)
}

type command struct {
	name    string
	usage   string
	summary string
	run     func(ctx context.Context, a *app, args []string) int
}

// commands is the subcommand table. main.go dispatches these names here.
var commands = []command{
	{"ls", "ls [path] [--workspace DIR] [--flat] [--json]", "List collections, folders and requests.", runLs},
	{"send", "send <path> [--workspace DIR] [-e env] [--var k=v] [--json] [--include] [--verbose] [--no-scripts] [--no-history]", "Send one request and print the status, body and tests.", runSend},
	{"run", "run <path> [--workspace DIR] [-e env] [--var k=v] [-d file] [-n N] [--delay 200ms] [--bail] [--reporter cli,json,junit] [--reporter-json-export FILE] [--reporter-junit-export FILE] [--timeout-request 30s] [--no-history] [--json]", "Run a collection or folder. --json streams NDJSON events.", runRun},
	{"env", "env [--workspace DIR] ls | use <name> | show|get <name> [--reveal] | set <name> <key> [<value>] [--secret] | unset <name> <key>", "List, select and edit environments. -e on send and run defaults to the active environment.", runEnv},
	{"curl", "curl <path> [--workspace DIR] [-e env] [--with-secrets]", "Print the resolved request as a curl command. Secrets are masked unless --with-secrets is set.", runCurl},
}

// Execute runs the subcommand in args[0]. It returns a process exit code.
func Execute(ctx context.Context, opts Options, args []string) int {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if len(args) == 0 {
		fmt.Fprintln(opts.Stderr, "Missing command. Commands: "+strings.Join(names(), ", "))
		return ExitUsage
	}
	var cmd *command
	for i := range commands {
		if commands[i].name == args[0] {
			cmd = &commands[i]
			break
		}
	}
	if cmd == nil {
		fmt.Fprintf(opts.Stderr, "Unknown command %q. Commands: %s\n", args[0], strings.Join(names(), ", "))
		return ExitUsage
	}
	workspace, commandArgs, err := extractWorkspace(args[1:])
	if err != nil {
		fmt.Fprintln(opts.Stderr, err)
		return ExitUsage
	}
	if workspace != "" {
		opts.Workspace = workspace
	}
	a := newApp(opts, cmd)
	return cmd.run(ctx, a, commandArgs)
}

func extractWorkspace(args []string) (string, []string, error) {
	var workspace string
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--workspace" || arg == "-w":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", nil, fmt.Errorf("%s requires a directory", arg)
			}
			i++
			workspace = args[i]
		case strings.HasPrefix(arg, "--workspace="):
			workspace = strings.TrimPrefix(arg, "--workspace=")
			if strings.TrimSpace(workspace) == "" {
				return "", nil, errors.New("--workspace requires a directory")
			}
		case strings.HasPrefix(arg, "-w="):
			workspace = strings.TrimPrefix(arg, "-w=")
			if strings.TrimSpace(workspace) == "" {
				return "", nil, errors.New("-w requires a directory")
			}
		default:
			out = append(out, arg)
		}
	}
	return workspace, out, nil
}

func names() []string {
	out := make([]string, len(commands))
	for i, c := range commands {
		out[i] = c.name
	}
	return out
}

// app is the per-invocation state shared by a command.
type app struct {
	opts Options
	cmd  *command
	in   io.Reader
	out  io.Writer
	err  io.Writer

	store *store.Store
	ws    *model.Workspace
	state store.State
	view  view
	red   *history.Redactor
}

func newApp(opts Options, cmd *command) *app {
	tty := opts.tty(opts.Stdout)
	noColor := opts.NoColor || !tty
	return &app{
		opts: opts,
		cmd:  cmd,
		in:   opts.Stdin,
		out:  opts.Stdout,
		err:  opts.Stderr,
		view: view{
			theme:   ui.NewTheme(true, noColor, opts.ASCII),
			noColor: noColor,
			ascii:   opts.ASCII,
		},
		red: history.NewRedactor(nil),
	}
}

func (o Options) tty(v any) bool {
	if o.IsTTY != nil {
		return o.IsTTY(v)
	}
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a *app) flagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(a.cmd.name, flag.ContinueOnError)
	fs.SetOutput(a.err)
	fs.Usage = func() {
		fmt.Fprintf(a.err, "Usage: postctl %s\n\n%s\n", a.cmd.usage, a.cmd.summary)
	}
	return fs
}

func (a *app) parse(fs *flag.FlagSet, args []string) ([]string, int) {
	positional, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ExitOK
		}
		return nil, ExitUsage
	}
	return positional, -1
}

// parseArgs accepts flags before or after positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func (a *app) onePath(positional []string) (string, int) {
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		fmt.Fprintf(a.err, "Expected one path. Usage: postctl %s\n", a.cmd.usage)
		return "", ExitUsage
	}
	return positional[0], -1
}

func (a *app) optionalPath(positional []string) (string, int) {
	switch len(positional) {
	case 0:
		return "", -1
	case 1:
		return positional[0], -1
	default:
		fmt.Fprintf(a.err, "Expected at most one path. Usage: postctl %s\n", a.cmd.usage)
		return "", ExitUsage
	}
}

func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.err, format+"\n", args...)
}
