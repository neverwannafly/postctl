// Package history keeps a bounded, append-only log of sent requests under a
// workspace's .postctl/history directory.
//
// Each entry is one JSON line holding a resolved snapshot of the request, the
// status and timing, test counts and a capped copy of the response body. Every
// snapshot is redacted before it is written (see [Redactor]): secret values,
// credentials headers and well-known token parameters never reach the disk.
//
// The log is bounded by entry count and by file size. When either bound is
// exceeded the oldest entries are dropped by rewriting the file, so the log
// never grows without limit. Several processes (the TUI and the CLI) may
// share one directory: appends and compaction are serialised with an advisory
// file lock.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shubham/postctl/internal/model"
)

// Default bounds and limits.
const (
	// DefaultMaxEntries is how many entries the log keeps.
	DefaultMaxEntries = 1000
	// DefaultMaxBytes is the size bound of the log file.
	DefaultMaxBytes int64 = 50 << 20
	// DefaultMaxBodyBytes caps the stored request and response bodies.
	DefaultMaxBodyBytes = 256 << 10
)

const (
	fileName = "history.jsonl"
	lockName = "history.lock"
	dirPerm  = 0o700
	filePerm = 0o600
)

// ErrNotFound is returned by [Log.Get] for an unknown entry ID.
var ErrNotFound = errors.New("history: entry not found")

// Entry is one sent request. Entries returned by [Log.List] are summaries:
// the request body and the Response are left out; use [Log.Get] for those.
type Entry struct {
	// ID identifies the entry; it sorts in the order entries were appended.
	ID string `json:"id"`
	// Time is when the send started.
	Time time.Time `json:"time"`
	// RequestID is the saved request that was sent.
	RequestID model.ID `json:"requestId"`
	// RequestName is the request's name when it was sent.
	RequestName string `json:"requestName"`
	// Path is the collection, folder and request names leading to it.
	Path []string `json:"path,omitempty"`
	// CollectionID is the owning collection.
	CollectionID model.ID `json:"collectionId,omitempty"`
	// Environment is the active environment's name, if any.
	Environment string `json:"environment,omitempty"`
	// Request is the redacted request as it went on the wire, or as it
	// would have if the transport failed.
	Request RequestSnapshot `json:"request"`
	// Template is the redacted unresolved request for restore into the
	// editor. Nil on entries written before this field existed.
	Template *RequestTemplate `json:"template,omitempty"`
	// Status is the HTTP status code, or 0 when there was no response.
	Status int `json:"status"`
	// StatusText is the reason phrase.
	StatusText string `json:"statusText,omitempty"`
	// Error is the redacted transport or resolution error, if any.
	Error string `json:"error,omitempty"`
	// DurationMS is the total response time in milliseconds.
	DurationMS int64 `json:"durationMs"`
	// SizeBytes is the response size on the wire (headers and body).
	SizeBytes int64 `json:"sizeBytes"`
	// Tests counts test outcomes.
	Tests TestCounts `json:"tests"`
	// ScriptError is the redacted text of a script failure, if any.
	ScriptError string `json:"scriptError,omitempty"`
	// Response is the redacted response, up to the body cap. Nil in
	// summaries and when there was no response.
	Response *ResponseSnapshot `json:"response,omitempty"`
}

// RequestSnapshot is the redacted request.
type RequestSnapshot struct {
	// Method is the HTTP method.
	Method string `json:"method"`
	// URL is the resolved URL with sensitive query values masked.
	URL string `json:"url"`
	// Headers are the request headers with sensitive values masked.
	Headers []model.KV `json:"headers,omitempty"`
	// Body is the redacted body text, up to the body cap.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports that Body was cut at the cap.
	BodyTruncated bool `json:"bodyTruncated,omitempty"`
}

// ResponseSnapshot is the redacted response.
type ResponseSnapshot struct {
	// Headers are the response headers with sensitive values masked.
	Headers []model.KV `json:"headers,omitempty"`
	// Body is the response body, up to the body cap. It is base64 when
	// BodyEncoding says so.
	Body string `json:"body,omitempty"`
	// BodyEncoding is empty for UTF-8 text and "base64" for binary.
	BodyEncoding string `json:"bodyEncoding,omitempty"`
	// Truncated reports that Body was cut at the cap.
	Truncated bool `json:"truncated,omitempty"`
}

// TestCounts summarises pm.test outcomes.
type TestCounts struct {
	// Passed is the number of passing tests.
	Passed int `json:"passed"`
	// Failed is the number of failing tests.
	Failed int `json:"failed"`
	// Skipped is the number of skipped tests.
	Skipped int `json:"skipped"`
}

// Filter selects entries for [Log.List]. The zero value matches everything.
type Filter struct {
	// RequestID keeps only entries for one request.
	RequestID model.ID
	// CollectionID keeps only entries for one collection.
	CollectionID model.ID
	// Query keeps entries whose request name, method or URL contains it,
	// ignoring case.
	Query string
	// Since keeps entries at or after this time.
	Since time.Time
	// Limit caps the result; zero means no cap.
	Limit int
	// Offset skips that many matches (newest first) before collecting.
	Offset int
}

// Options tune a [Log]. Zero fields take the defaults.
type Options struct {
	// MaxEntries is how many entries to keep.
	MaxEntries int
	// MaxBytes is the size bound of the file.
	MaxBytes int64
	// Now supplies the clock for entries without a Time.
	Now func() time.Time
}

// Log is an open history directory. It is safe for concurrent use.
type Log struct {
	dir  string
	opts Options

	mu    sync.Mutex
	index []indexed
	byID  map[string]int
	size  int64
	info  os.FileInfo
}

type indexed struct {
	offset  int64
	length  int
	summary Entry
}

// Open opens (creating on first append) the history in dir, normally
// <workspace>/.postctl/history.
func Open(dir string, opts Options) (*Log, error) {
	if dir == "" {
		return nil, errors.New("history: empty directory")
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = DefaultMaxEntries
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	l := &Log{dir: dir, opts: opts, byID: map[string]int{}}
	if err := l.withLock(l.syncLocked); err != nil {
		return nil, err
	}
	return l, nil
}

// Dir returns the history directory.
func (l *Log) Dir() string { return l.dir }

func (l *Log) path() string { return filepath.Join(l.dir, fileName) }

// Append writes e and returns it with ID and Time filled in. It compacts the
// file when a bound is exceeded.
func (l *Log) Append(e Entry) (Entry, error) {
	if e.ID == "" {
		e.ID = string(model.NewID())
	}
	if e.Time.IsZero() {
		e.Time = l.opts.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return e, fmt.Errorf("history: encode entry: %w", err)
	}
	line = append(line, '\n')
	err = l.withLock(func() error {
		if err := l.syncLocked(); err != nil {
			return err
		}
		if err := os.MkdirAll(l.dir, dirPerm); err != nil {
			return err
		}
		f, err := os.OpenFile(l.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
		if err != nil {
			return err
		}
		offset := l.size
		if _, err := f.Write(line); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		l.add(offset, len(line), e)
		l.size += int64(len(line))
		if st, err := os.Stat(l.path()); err == nil {
			l.info = st
		}
		if l.overBounds() {
			return l.compactLocked()
		}
		return nil
	})
	return e, err
}

// List returns summaries, newest first.
func (l *Log) List(f Filter) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.syncLocked(); err != nil {
		return nil, err
	}
	query := strings.ToLower(f.Query)
	var out []Entry
	skipped := 0
	first := 0
	if len(l.index) > l.opts.MaxEntries {
		first = len(l.index) - l.opts.MaxEntries
	}
	for i := len(l.index) - 1; i >= first; i-- {
		s := l.index[i].summary
		if !matches(s, f, query) {
			continue
		}
		if skipped < f.Offset {
			skipped++
			continue
		}
		out = append(out, s)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func matches(s Entry, f Filter, query string) bool {
	if f.RequestID != "" && s.RequestID != f.RequestID {
		return false
	}
	if f.CollectionID != "" && s.CollectionID != f.CollectionID {
		return false
	}
	if !f.Since.IsZero() && s.Time.Before(f.Since) {
		return false
	}
	if query == "" {
		return true
	}
	hay := strings.ToLower(s.RequestName + " " + s.Request.Method + " " + s.Request.URL)
	return strings.Contains(hay, query)
}

// Get returns the full entry, including the request body and response.
func (l *Log) Get(id string) (*Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.syncLocked(); err != nil {
		return nil, err
	}
	i, ok := l.byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	f, err := os.Open(l.path())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, l.index[i].length)
	if _, err := f.ReadAt(buf, l.index[i].offset); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(bytes.TrimSpace(buf), &e); err != nil {
		return nil, fmt.Errorf("history: entry %s is corrupt: %w", id, err)
	}
	return &e, nil
}

// Len reports how many entries the log holds.
func (l *Log) Len() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.syncLocked(); err != nil {
		return 0, err
	}
	return len(l.index), nil
}

// Clear deletes every entry.
func (l *Log) Clear() error {
	return l.withLock(func() error {
		if err := os.Remove(l.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		l.reset()
		return nil
	})
}

// withLock serialises the in-process state and, across processes, the file.
func (l *Log) withLock(fn func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, dirPerm); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(l.dir, lockName), os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	return fn()
}

func (l *Log) reset() {
	l.index = nil
	l.byID = map[string]int{}
	l.size = 0
	l.info = nil
}

func (l *Log) add(offset int64, length int, e Entry) {
	s := e
	s.Response = nil
	s.Request.Body = ""
	s.Request.BodyTruncated = false
	if s.Template != nil {
		t := *s.Template
		t.BodyRaw = ""
		t.BodyURLEncoded = nil
		t.BodyFormData = nil
		t.BodyGraphQLVariables = ""
		t.BodyRawTruncated = false
		t.BodyRawEncoding = ""
		s.Template = &t
	}
	l.byID[e.ID] = len(l.index)
	l.index = append(l.index, indexed{offset: offset, length: length, summary: s})
}

// syncLocked brings the in-memory index up to date with the file, which
// another process may have appended to or compacted.
func (l *Log) syncLocked() error {
	st, err := os.Stat(l.path())
	if errors.Is(err, os.ErrNotExist) {
		l.reset()
		return nil
	}
	if err != nil {
		return err
	}
	if l.info != nil && os.SameFile(l.info, st) && st.Size() >= l.size {
		if st.Size() == l.size {
			l.info = st
			return nil
		}
		return l.scanFrom(l.size, st)
	}
	l.reset()
	return l.scanFrom(0, st)
}

// scanFrom indexes the complete lines from offset to the end of the file. A
// trailing partial line, for example from a crashed writer, is ignored and
// overwritten by the next compaction.
func (l *Log) scanFrom(offset int64, st os.FileInfo) error {
	f, err := os.Open(l.path())
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	pos := offset
	for {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		var e Entry
		if json.Unmarshal(line, &e) == nil && e.ID != "" {
			l.add(pos, len(line), e)
		}
		pos += int64(len(line))
	}
	l.size = pos
	l.info = st
	return nil
}

func (l *Log) overBounds() bool {
	slack := l.opts.MaxEntries / 10
	if slack < 1 {
		slack = 1
	}
	return len(l.index) > l.opts.MaxEntries+slack || l.size > l.opts.MaxBytes
}

// compactLocked keeps the newest entries within MaxEntries and 90% of
// MaxBytes (the gap avoids rewriting the file on every append) by copying
// them to a new file and renaming it into place.
func (l *Log) compactLocked() error {
	budget := l.opts.MaxBytes / 10 * 9
	if budget < 1 {
		budget = l.opts.MaxBytes
	}
	keepFrom := len(l.index)
	var bytesKept int64
	for i := len(l.index) - 1; i >= 0; i-- {
		n := int64(l.index[i].length)
		if len(l.index)-i > l.opts.MaxEntries || (bytesKept+n > budget && keepFrom < len(l.index)) {
			break
		}
		bytesKept += n
		keepFrom = i
	}
	kept := l.index[keepFrom:]

	src, err := os.Open(l.path())
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(l.dir, ".history-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return err
	}
	next := make([]indexed, 0, len(kept))
	var offset int64
	for _, item := range kept {
		buf := make([]byte, item.length)
		if _, err := src.ReadAt(buf, item.offset); err != nil && !errors.Is(err, io.EOF) {
			_ = tmp.Close()
			return err
		}
		if _, err := tmp.Write(buf); err != nil {
			_ = tmp.Close()
			return err
		}
		next = append(next, indexed{offset: offset, length: item.length, summary: item.summary})
		offset += int64(item.length)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, l.path()); err != nil {
		return err
	}
	l.index = next
	l.byID = make(map[string]int, len(next))
	for i, item := range next {
		l.byID[item.summary.ID] = i
	}
	l.size = offset
	if st, err := os.Stat(l.path()); err == nil {
		l.info = st
	}
	return nil
}
