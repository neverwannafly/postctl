package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/model"
)

// reporter consumes a collection run. close finishes any buffered export.
type reporter interface {
	Event(ev engine.RunEvent) error
	Close() error
}

func parseReporters(flagValue string, jsonFlag bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(name string) error {
		switch name {
		case "cli", "json", "junit":
		default:
			return fmt.Errorf("unknown reporter %q. Use cli, json or junit", name)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return nil
	}
	if strings.TrimSpace(flagValue) != "" {
		for _, part := range strings.Split(flagValue, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if err := add(part); err != nil {
				return nil, err
			}
		}
	}
	if jsonFlag {
		if err := add("json"); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		out = []string{"cli"}
	}
	return out, nil
}

// openReporters wires each reporter to stdout or to its export file. When
// more than one reporter would write to stdout, the extra ones need an
// export path so a pipe stays unambiguous.
func openReporters(a *app, names []string, jsonExport, junitExport string, methods map[model.ID]string) ([]reporter, func(), error) {
	var reps []reporter
	var files []io.Closer
	stdoutUsed := ""
	cleanup := func() {
		for _, r := range reps {
			_ = r.Close()
		}
		for _, f := range files {
			_ = f.Close()
		}
	}
	claim := func(name, export string) (io.Writer, error) {
		if export != "" {
			f, err := createExport(export)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
			return f, nil
		}
		if stdoutUsed != "" {
			other := name
			if other == "cli" {
				other = stdoutUsed
			}
			return nil, fmt.Errorf("%s and %s both write to stdout. Pass --reporter-%s-export FILE", stdoutUsed, name, other)
		}
		stdoutUsed = name
		return a.out, nil
	}
	for _, name := range names {
		switch name {
		case "cli":
			w, err := claim("cli", "")
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			reps = append(reps, newCLIReporter(a, w, methods))
		case "json":
			w, err := claim("json", jsonExport)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			reps = append(reps, newJSONReporter(a, w))
		case "junit":
			w, err := claim("junit", junitExport)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			reps = append(reps, newJUnitReporter(a, w))
		}
	}
	return reps, cleanup, nil
}

func runExit(sum *engine.RunSummary) int {
	if sum == nil {
		return ExitRequest
	}
	code := sum.ExitCode()
	if sum.StopReason == engine.StopCancelled && code == ExitOK {
		return ExitRequest
	}
	return code
}

func stopText(reason engine.StopReason) string {
	switch reason {
	case engine.StopBail:
		return "Stopped after the first failure (--bail)."
	case engine.StopScriptError:
		return "Stopped after a script error."
	case engine.StopCancelled:
		return "Stopped because the run was canceled."
	case engine.StopNextNull:
		return "Stopped because a script called setNextRequest(null)."
	case engine.StopNextMissing:
		return "Stopped because setNextRequest named a request that is not in this run."
	default:
		return ""
	}
}

func failedRequestCount(sum *engine.RunSummary) int {
	seen := map[string]bool{}
	for _, f := range sum.Failures {
		seen[fmt.Sprintf("%d/%s", f.Iteration, f.RequestID)] = true
	}
	return len(seen)
}

func failedIterationCount(sum *engine.RunSummary) int {
	seen := map[int]bool{}
	for _, f := range sum.Failures {
		seen[f.Iteration] = true
	}
	return len(seen)
}
