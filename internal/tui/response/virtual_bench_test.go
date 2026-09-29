package response

import (
	"bytes"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/ui"
)

func BenchmarkLargeBodyFirstRender(b *testing.B) {
	var buf bytes.Buffer
	buf.WriteString("[\n")
	for i := 0; i < 500_000; i++ {
		buf.WriteString(`{"id":1,"name":"x"},`)
		buf.WriteByte('\n')
	}
	buf.WriteString("]\n")
	body := buf.Bytes()
	th := ui.NewTheme(true, false, false)
	p := &Pane{th: th, w: 120, h: 40, bodyMode: modeRaw, scroll: 0}
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		p.rawIdx = lineIndex{}
		_ = p.renderRawVirtual(body, 120, 38)
	}
	if b.N > 0 {
		avg := time.Since(start) / time.Duration(b.N)
		if avg > 50*time.Millisecond {
			b.Logf("avg render %v (soft target <50ms)", avg)
		}
	}
}
