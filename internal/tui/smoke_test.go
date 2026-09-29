package tui

import "testing"

func TestSmoke(t *testing.T) {
	h := newHarness(t, demo(), 140, 30)
	h.selectByName("Create charge")
	h.keys("enter")
	t.Log("\n" + h.screen())
	h.keys("E")
	t.Log("\n" + h.screen())
	h.keys("down", "down", "down", "enter")
	t.Log("\n" + h.screen())
	h.keys("ctrl+p")
	h.typeText("cc")
	t.Log("\n" + h.screen())
	h.keys("esc", "?")
	t.Log("\n" + h.screen())
	h.keys("esc", "1", "d")
	t.Log("\n" + h.screen())
}
