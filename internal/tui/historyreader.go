package tui

import (
	"github.com/shubham/postctl/internal/history"
)

// HistoryReader lists and reads the workspace send log. Implementations wrap
// [history.Log].
type HistoryReader interface {
	List(filter history.Filter) ([]history.Entry, error)
	Get(id string) (*history.Entry, error)
	Clear() error
}
