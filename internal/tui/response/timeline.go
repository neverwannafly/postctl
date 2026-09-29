package response

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/ui"
)

// phaseBar renders one labeled timing bar.
func phaseBar(t ui.Theme, label string, d, total time.Duration, barW int) string {
	if barW < 4 {
		return t.Label(10, label, ui.FormatDuration(d))
	}
	frac := 0.0
	if total > 0 {
		frac = float64(d) / float64(total)
	}
	full := int(frac * float64(barW))
	if d > 0 && full == 0 {
		full = 1
	}
	bar := strings.Repeat(t.Glyphs.BarFull, full) + strings.Repeat(t.Glyphs.BarEmpty, barW-full)
	return t.Label(10, label, bar+" "+t.Render(ui.TextSecondary, ui.FormatDuration(d)))
}

// RenderTimeline draws timing bars, redirects, TLS and the sent request.
func RenderTimeline(t ui.Theme, resp *httpx.Response, w int, reveal bool, redactor *history.Redactor) string {
	if resp == nil {
		return t.EmptyState("No timing data", "Send a request to see DNS, TLS and download phases.", "Send with Ctrl-S.", w)
	}
	if redactor == nil {
		redactor = history.NewRedactor(nil)
	}
	mask := func(text string) string {
		return strings.ReplaceAll(text, history.Mask, t.Glyphs.Mask)
	}
	tm := resp.Timings
	total := tm.Total
	if total <= 0 {
		total = tm.DNS + tm.Connect + tm.TLS + tm.TTFB + tm.Download
	}
	barW := max(8, min(24, w/4))
	var lines []string
	lines = append(lines, t.Bold().Render("Timing"))
	lines = append(lines, phaseBar(t, "DNS", tm.DNS, total, barW))
	lines = append(lines, phaseBar(t, "Connect", tm.Connect, total, barW))
	lines = append(lines, phaseBar(t, "TLS", tm.TLS, total, barW))
	lines = append(lines, phaseBar(t, "TTFB", tm.TTFB, total, barW))
	lines = append(lines, phaseBar(t, "Download", tm.Download, total, barW))
	lines = append(lines, t.Render(ui.TextSecondary, "Total "+ui.FormatDuration(total)))

	if resp.RemoteAddr != "" {
		lines = append(lines, "", t.Label(8, "Remote", resp.RemoteAddr))
	}
	if resp.TLS != nil {
		lines = append(lines, t.Label(8, "TLS", fmt.Sprintf("%s  %s", tlsVersionName(resp.TLS), tls.CipherSuiteName(resp.TLS.CipherSuite))))
	}
	if len(resp.Redirects) > 0 {
		lines = append(lines, "", t.Bold().Render("Redirects"))
		for _, r := range resp.Redirects {
			location := r.Location
			if !reveal {
				location = mask(redactor.URL(location))
			}
			lines = append(lines, t.Render(ui.TextSecondary, fmt.Sprintf("%d %s", r.StatusCode, t.Truncate(location, w-6))))
		}
	}

	sent := resp.Sent
	sentURL := sent.URL
	headers := sent.Headers
	if !reveal {
		sentURL = mask(redactor.URL(sentURL))
		headers = redactor.Headers(headers)
	}
	lines = append(lines, "", t.Bold().Render("Request sent"))
	lines = append(lines, t.MethodBadge(sent.Method, 7)+" "+t.Render(ui.TextPrimary, t.Truncate(sentURL, max(1, w-8))))
	for _, h := range headers {
		val := mask(h.Value)
		lines = append(lines, t.Render(ui.TextSecondary, t.Truncate(h.Key+": "+val, w)))
	}
	return strings.Join(lines, "\n")
}

func tlsVersionName(state *tls.ConnectionState) string {
	switch state.Version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	default:
		return fmt.Sprintf("TLS 0x%x", state.Version)
	}
}
