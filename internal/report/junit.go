package report

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shubham/postctl/internal/engine"
)

type junitReporter struct {
	opts   Options
	w      io.Writer
	name   string
	suites []junitSuite
}

// NewJUnit returns a buffered JUnit XML run reporter.
func NewJUnit(w io.Writer, opts Options) Reporter {
	return &junitReporter{opts: opts, w: w}
}

// Close finishes the reporter.
func (r *junitReporter) Close() error { return nil }

// Event accumulates one run event and writes the report when the run finishes.
func (r *junitReporter) Event(ev engine.RunEvent) error {
	switch e := ev.(type) {
	case engine.RunStarted:
		r.name = e.CollectionName
		if e.FolderName != "" {
			r.name += "/" + e.FolderName
		}
	case engine.RequestFinished:
		r.suites = append(r.suites, r.suite(e))
	case engine.RunFinished:
		return r.write(e.Summary)
	}
	return nil
}

func (r *junitReporter) suite(e engine.RequestFinished) junitSuite {
	o := e.Outcome
	name := ""
	if o != nil {
		name = strings.Join(o.Path, "/")
	}
	if e.Iteration > 0 || (o != nil && o.Iteration > 0) {
		iter := e.Iteration
		if o != nil {
			iter = o.Iteration
		}
		name = fmt.Sprintf("%s [#%d]", name, iter+1)
	}
	s := junitSuite{Name: name}
	if o == nil {
		return s
	}
	seconds := o.Duration.Seconds()
	if o.Response != nil {
		seconds = o.Response.Timings.Total.Seconds()
	}
	timeAttr := fmt.Sprintf("%.3f", seconds)
	if o.Skipped {
		msg := o.SkipReason
		if msg == "" {
			msg = "skipped"
		}
		s.Cases = append(s.Cases, junitCase{
			Name: "request", ClassName: name, Time: timeAttr,
			Skipped: &junitSkipped{Message: msg},
		})
		s.Tests++
		s.Skipped++
	}
	for _, t := range o.Tests {
		tc := junitCase{Name: t.Name, ClassName: name, Time: timeAttr}
		switch {
		case t.Skipped:
			tc.Skipped = &junitSkipped{Message: "skipped"}
			s.Skipped++
		case !t.Passed:
			msg := r.opts.mask(t.Message)
			tc.Failure = &junitFailure{Message: msg, Type: "assertion", Text: msg}
			s.Failures++
		}
		s.Cases = append(s.Cases, tc)
		s.Tests++
	}
	if o.ScriptError != nil {
		msg := r.opts.mask(o.ScriptError.Text)
		s.Cases = append(s.Cases, junitCase{
			Name: "script", ClassName: name, Time: timeAttr,
			Failure: &junitFailure{Message: msg, Type: "script", Text: msg},
		})
		s.Tests++
		s.Failures++
	}
	if o.Err != nil && !errors.Is(o.Err, context.Canceled) {
		msg := r.opts.mask(o.Err.Error())
		s.Cases = append(s.Cases, junitCase{
			Name: "request", ClassName: name, Time: timeAttr,
			Failure: &junitFailure{Message: msg, Type: "transport", Text: msg},
		})
		s.Tests++
		s.Failures++
	}
	s.Time = timeAttr
	return s
}

func (r *junitReporter) write(sum *engine.RunSummary) error {
	doc := JUnitSuites{Name: r.name}
	for _, s := range r.suites {
		doc.Tests += s.Tests
		doc.Failures += s.Failures
		doc.Skipped += s.Skipped
		doc.Suites = append(doc.Suites, s)
	}
	if sum != nil {
		doc.Time = fmt.Sprintf("%.3f", sum.TotalTime.Seconds())
	}
	if _, err := io.WriteString(r.w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(r.w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(r.w, "\n")
	return err
}

// JUnitSuites is the XML document emitted by the JUnit reporter.
type JUnitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr,omitempty"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}
