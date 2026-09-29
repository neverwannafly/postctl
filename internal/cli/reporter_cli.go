package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

type cliReporter struct {
	a        *app
	w        io.Writer
	methods  map[model.ID]string
	preN     int
	preFail  int
	testN    int
	testFail int
	bytes    int64
	name     string
}

func newCLIReporter(a *app, w io.Writer, methods map[model.ID]string) *cliReporter {
	return &cliReporter{a: a, w: w, methods: methods}
}

func (r *cliReporter) close() error { return nil }

func (r *cliReporter) event(ev engine.RunEvent) error {
	switch e := ev.(type) {
	case engine.RunStarted:
		r.name = e.CollectionName
		if e.FolderName != "" {
			r.name = e.CollectionName + "/" + e.FolderName
		}
		fmt.Fprintln(r.w, r.name)
		fmt.Fprintln(r.w)
	case engine.IterationStarted:
		if e.Iteration > 0 {
			fmt.Fprintln(r.w)
		}
		fmt.Fprintf(r.w, "Iteration %d of %d\n\n", e.Iteration+1, e.Iterations)
	case engine.RequestProgress:
		switch e.Step.Kind {
		case engine.StepPreRequest:
			r.preN++
		case engine.StepTest:
			r.testN++
		}
	case engine.RequestFinished:
		r.writeRequest(e)
	case engine.RunFinished:
		r.writeSummary(e.Summary)
	}
	return nil
}

func (r *cliReporter) writeRequest(e engine.RequestFinished) {
	o := e.Outcome
	if o == nil {
		fmt.Fprintln(r.w)
		return
	}
	method := r.methods[o.RequestID]
	path := strings.Join(o.Path, "/")
	if o.ScriptError != nil {
		if o.ScriptError.Event == "prerequest" {
			r.preFail++
		} else {
			r.testFail++
		}
	}
	if o.Response != nil {
		r.bytes += o.Response.Size.Headers + o.Response.Size.Body
	}
	if method == "" {
		fmt.Fprintln(r.w, path)
	} else {
		fmt.Fprintf(r.w, "%s %s\n", r.a.view.method(method), path)
	}
	fmt.Fprintf(r.w, "  %s\n", r.a.outcomeStatus(o))
	for _, t := range o.Tests {
		switch {
		case t.Skipped:
			fmt.Fprintf(r.w, "  %s %s  skipped\n", r.a.view.skip(), t.Name)
		case t.Passed:
			fmt.Fprintf(r.w, "  %s %s\n", r.a.view.paint(ui.StatusSuccess, r.a.view.check()), t.Name)
		default:
			fmt.Fprintf(r.w, "  %s %s\n", r.a.view.paint(ui.StatusDanger, r.a.view.fail()), t.Name)
			if t.Message != "" {
				fmt.Fprintf(r.w, "    %s\n", r.a.mask(t.Message))
			}
		}
	}
	if o.ScriptError != nil {
		fmt.Fprintf(r.w, "  %s %s\n", r.a.view.fail(), r.a.mask(o.ScriptError.Text))
	}
	fmt.Fprintln(r.w)
}

func (r *cliReporter) writeSummary(sum *engine.RunSummary) {
	if sum == nil {
		return
	}
	if text := stopText(sum.StopReason); text != "" {
		fmt.Fprintln(r.w, text)
		fmt.Fprintln(r.w)
	}
	rows := [][3]string{
		{"iterations", fmt.Sprintf("%d", sum.Iterations), fmt.Sprintf("%d", failedIterationCount(sum))},
		{"requests", fmt.Sprintf("%d", sum.Requests), fmt.Sprintf("%d", failedRequestCount(sum))},
		{"skipped", fmt.Sprintf("%d", sum.Skipped), "0"},
		{"test-scripts", fmt.Sprintf("%d", r.testN), fmt.Sprintf("%d", r.testFail)},
		{"prerequest-scripts", fmt.Sprintf("%d", r.preN), fmt.Sprintf("%d", r.preFail)},
		{"assertions", fmt.Sprintf("%d", sum.Tests.Total()), fmt.Sprintf("%d", sum.Tests.Failed)},
	}
	var foot []string
	foot = append(foot, "total run duration: "+ui.FormatDuration(sum.TotalTime))
	if r.bytes > 0 {
		foot = append(foot, "total data received: "+ui.FormatSize(r.bytes))
	}
	foot = append(foot, "average response time: "+ui.FormatDuration(sum.AvgResponseTime))
	fmt.Fprintln(r.w, summaryTable(r.a.view.ascii, rows, foot))
	if len(sum.Failures) == 0 {
		return
	}
	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, "Failures")
	for i, f := range sum.Failures {
		fmt.Fprintf(r.w, "  %d. %s\n", i+1, strings.Join(f.Path, " / "))
		switch f.Kind {
		case engine.FailureTest:
			fmt.Fprintf(r.w, "     %s %s\n", r.a.view.fail(), f.Name)
			if f.Message != "" {
				fmt.Fprintf(r.w, "       %s\n", r.a.mask(f.Message))
			}
		default:
			fmt.Fprintf(r.w, "     %s %s\n", r.a.view.fail(), r.a.mask(f.Message))
		}
	}
}

func summaryTable(ascii bool, rows [][3]string, footers []string) string {
	const labelW, numW = 22, 10
	span := labelW + 1 + numW + 1 + numW
	var b strings.Builder
	line := func(left, mid, right, fill string) {
		b.WriteString(left)
		b.WriteString(strings.Repeat(fill, labelW))
		b.WriteString(mid)
		b.WriteString(strings.Repeat(fill, numW))
		b.WriteString(mid)
		b.WriteString(strings.Repeat(fill, numW))
		b.WriteString(right)
		b.WriteByte('\n')
	}
	row := func(label, exec, fail string) {
		b.WriteString(boxV(ascii))
		b.WriteString(padLeft(label, labelW))
		b.WriteString(boxV(ascii))
		b.WriteString(padLeft(exec, numW))
		b.WriteString(boxV(ascii))
		b.WriteString(padLeft(fail, numW))
		b.WriteString(boxV(ascii))
		b.WriteByte('\n')
	}
	if ascii {
		line("+", "+", "+", "-")
		row("", "executed", "failed")
		line("+", "+", "+", "-")
	} else {
		line("┌", "┬", "┐", "─")
		row("", "executed", "failed")
		line("├", "┼", "┤", "─")
	}
	for _, r := range rows {
		row(r[0], r[1], r[2])
	}
	if ascii {
		line("+", "+", "+", "-")
	} else {
		line("├", "┴", "┤", "─")
	}
	for _, f := range footers {
		b.WriteString(boxV(ascii))
		b.WriteString(padRight(f, span))
		b.WriteString(boxV(ascii))
		b.WriteByte('\n')
	}
	if ascii {
		b.WriteString("+")
		b.WriteString(strings.Repeat("-", span))
		b.WriteString("+\n")
	} else {
		b.WriteString("└")
		b.WriteString(strings.Repeat("─", span))
		b.WriteString("┘\n")
	}
	return b.String()
}

func boxV(ascii bool) string {
	if ascii {
		return "|"
	}
	return "│"
}

func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
