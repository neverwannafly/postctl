package vimtext

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/ui"
)

func BenchmarkLargeBuffer(b *testing.B) {
	value := strings.Repeat("const item = 42;\n", 4999) + "const item = 42;"
	th := ui.NewTheme(true, true, false)
	b.Run("G-gg", func(b *testing.B) {
		m := New(th, Options{})
		m.SetValue(value)
		_ = m.Focus()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = m.Update(runeKey('G'))
			_, _ = m.Update(runeKey('g'))
			_, _ = m.Update(runeKey('g'))
		}
	})
	b.Run("type-100", func(b *testing.B) {
		for range b.N {
			m := New(th, Options{StartInsert: true})
			m.SetValue(value)
			m.SetCursor(2500, 0)
			_ = m.Focus()
			b.StartTimer()
			for range 100 {
				_, _ = m.Update(runeKey('x'))
			}
			b.StopTimer()
		}
	})
	b.Run("highlight-recompute", func(b *testing.B) {
		m := New(ui.NewTheme(true, false, false), Options{
			StartInsert: true,
			Highlighter: ChromaHighlighter(ui.NewTheme(true, false, false), "javascript"),
		})
		m.SetValue(value)
		m.SetCursor(2500, 0)
		_ = m.Focus()
		key := tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"})
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = m.Update(key)
		}
	})
}
