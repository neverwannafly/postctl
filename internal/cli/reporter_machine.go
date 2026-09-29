package cli

import (
	"io"

	"github.com/shubham/postctl/internal/report"
)

type junitSuites = report.JUnitSuites

func newJSONReporter(a *app, w io.Writer) reporter {
	return report.NewJSON(w, report.Options{Mask: a.mask})
}

func newJUnitReporter(a *app, w io.Writer) reporter {
	return report.NewJUnit(w, report.Options{Mask: a.mask})
}
