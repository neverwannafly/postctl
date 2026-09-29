package script

import (
	"runtime/metrics"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// DefaultMemoryLimit is how much the heap may grow while one script run is
// executing before the run is stopped. See [Input.MemoryLimit] for what the
// figure does and does not measure.
const DefaultMemoryLimit int64 = 256 << 20

const (
	// memoryPollInterval is how often the watchdog samples the heap.
	memoryPollInterval = 10 * time.Millisecond
	// garbageFactor scales the limit for the heap-objects figure, which
	// still counts garbage the collector has not swept yet.
	garbageFactor = 4
	// maxCallDepth caps JavaScript call nesting. goja's own default is
	// effectively unbounded, so unbounded recursion would grow the heap
	// until the memory watchdog stopped it; Node's limit is about the same.
	maxCallDepth = 10000
	// regexMatchTimeout bounds a single regular-expression match. goja
	// backs RegExp with regexp2 for patterns Go's regexp cannot express
	// (lookahead, backreferences); a catastrophic pattern would otherwise
	// run inside one native call that Runtime.Interrupt cannot reach.
	regexMatchTimeout = time.Second
	// abandonGrace is how long after its deadline a run waits for the VM to
	// unwind before it gives up on the VM altogether.
	abandonGrace = 1500 * time.Millisecond
	// finishBudget bounds the wrap-up that runs after the script chain, which
	// can call back into script-defined code (for example an overridden
	// pm.request.toJSON).
	finishBudget = 500 * time.Millisecond
)

func init() {
	// regexp2 reads this when a pattern is compiled, so setting it once at
	// start-up covers every RegExp a script creates. It is a package
	// variable, hence set here and never again.
	regexp2.DefaultMatchTimeout = regexMatchTimeout
}

var memoryMetrics = []string{
	"/gc/heap/live:bytes",
	"/memory/classes/heap/objects:bytes",
}

// memoryWatch reports when the process heap has grown by more than a limit
// since the watch was created.
//
// goja has no per-runtime allocation accounting, so the measure is the whole
// process's heap growth while the script runs. That is exact for the usual
// single script and conservative otherwise: a runaway script may also stop a
// second script that happens to run at the same time.
type memoryWatch struct {
	limit    uint64
	samples  []metrics.Sample
	baseLive uint64
	baseHeap uint64
}

// newMemoryWatch returns nil when the limit is disabled (negative).
func newMemoryWatch(limit int64) *memoryWatch {
	if limit < 0 {
		return nil
	}
	if limit == 0 {
		limit = DefaultMemoryLimit
	}
	m := &memoryWatch{limit: uint64(limit)}
	for _, name := range memoryMetrics {
		m.samples = append(m.samples, metrics.Sample{Name: name})
	}
	m.baseLive, m.baseHeap = m.read()
	return m
}

func (m *memoryWatch) read() (live, heap uint64) {
	metrics.Read(m.samples)
	value := func(s metrics.Sample) uint64 {
		if s.Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return s.Value.Uint64()
	}
	return value(m.samples[0]), value(m.samples[1])
}

// exceeded reports whether the heap has grown past the limit. Live bytes are
// only refreshed by a collection, so the heap-objects figure, which includes
// garbage, backs it up with a looser bound for allocation that outpaces the GC.
func (m *memoryWatch) exceeded() bool {
	live, heap := m.read()
	if live > m.baseLive && live-m.baseLive > m.limit {
		return true
	}
	return heap > m.baseHeap && heap-m.baseHeap > m.limit*garbageFactor
}
