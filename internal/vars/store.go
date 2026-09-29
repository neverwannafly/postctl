package vars

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/shubham/postctl/internal/model"
)

const maxResolveDepth = 16

type layer struct {
	order  []string
	values map[string]any
}

// MemStore is an in-memory implementation of Store for one run.
type MemStore struct {
	mu      sync.Mutex
	layers  map[Scope]*layer
	changes map[Scope][]Change
	random  RandomSource
}

// New builds a store from the persisted scopes. Disabled variables are
// ignored. Later duplicate keys replace the value without changing order.
func New(globals, collection, environment []model.Variable, data map[string]any) *MemStore {
	return NewWithRandom(globals, collection, environment, data, cryptoSource{})
}

// NewWithRandom is New with an injectable source for dynamic variables.
func NewWithRandom(
	globals, collection, environment []model.Variable,
	data map[string]any,
	source RandomSource,
) *MemStore {
	if source == nil {
		source = cryptoSource{}
	}
	s := &MemStore{
		layers:  make(map[Scope]*layer, int(Local)+1),
		changes: make(map[Scope][]Change),
		random:  source,
	}
	for scope := Global; scope <= Local; scope++ {
		s.layers[scope] = &layer{values: make(map[string]any)}
	}
	s.loadVariables(Global, globals)
	s.loadVariables(Collection, collection)
	s.loadVariables(Environment, environment)
	for key, value := range data {
		s.put(Data, key, value)
	}
	return s
}

func (s *MemStore) loadVariables(scope Scope, variables []model.Variable) {
	for _, variable := range variables {
		if variable.Enabled() {
			s.put(scope, variable.Key, variable.Value)
		}
	}
}

func (s *MemStore) put(scope Scope, key string, value any) {
	l := s.layers[scope]
	if _, exists := l.values[key]; !exists {
		l.order = append(l.order, key)
	}
	l.values[key] = value
}

// Get implements Store.
func (s *MemStore) Get(key string) (any, Scope, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for scope := Local; scope >= Global; scope-- {
		if value, ok := s.layers[scope].values[key]; ok {
			return value, scope, true
		}
	}
	return nil, Global, false
}

// GetIn implements Store.
func (s *MemStore) GetIn(scope Scope, key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.layers[scope]
	if !ok {
		return nil, false
	}
	value, exists := l.values[key]
	return value, exists
}

// Set implements Store.
func (s *MemStore) Set(scope Scope, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.layers[scope]; !ok {
		return
	}
	s.put(scope, key, value)
	s.changes[scope] = append(s.changes[scope], Change{Key: key, Value: value})
}

// Unset implements Store.
func (s *MemStore) Unset(scope Scope, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.layers[scope]
	if !ok {
		return
	}
	if _, exists := l.values[key]; !exists {
		return
	}
	delete(l.values, key)
	for i, existing := range l.order {
		if existing == key {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	s.changes[scope] = append(s.changes[scope], Change{Key: key, Unset: true})
}

// Clear implements Store.
func (s *MemStore) Clear(scope Scope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.layers[scope]
	if !ok {
		return
	}
	for _, key := range l.order {
		s.changes[scope] = append(s.changes[scope], Change{Key: key, Unset: true})
	}
	l.order = nil
	clear(l.values)
}

// Keys implements Store.
func (s *MemStore) Keys(scope Scope) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.layers[scope]
	if !ok {
		return nil
	}
	return append([]string(nil), l.order...)
}

// Changes implements Store.
func (s *MemStore) Changes() map[Scope][]Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[Scope][]Change, len(s.changes))
	for scope, changes := range s.changes {
		result[scope] = append([]Change(nil), changes...)
	}
	return result
}

// Replace implements Store.
func (s *MemStore) Replace(template string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replace(template, 0, make(map[string]bool))
}

func (s *MemStore) replace(template string, depth int, resolving map[string]bool) string {
	if depth >= maxResolveDepth {
		return template
	}
	var result strings.Builder
	for cursor := 0; cursor < len(template); {
		start := strings.Index(template[cursor:], "{{")
		if start < 0 {
			result.WriteString(template[cursor:])
			break
		}
		start += cursor
		result.WriteString(template[cursor:start])
		endOffset := strings.Index(template[start+2:], "}}")
		if endOffset < 0 {
			result.WriteString(template[start:])
			break
		}
		end := start + 2 + endOffset
		literal := template[start : end+2]
		name := strings.TrimSpace(template[start+2 : end])
		replacement, ok := s.resolve(name, literal, depth, resolving)
		if ok {
			result.WriteString(replacement)
		} else {
			result.WriteString(literal)
		}
		cursor = end + 2
	}
	return result.String()
}

func (s *MemStore) resolve(
	name, literal string,
	depth int,
	resolving map[string]bool,
) (string, bool) {
	if strings.HasPrefix(name, "$") {
		value, ok := s.dynamic(name)
		return value, ok
	}
	if resolving[name] {
		return literal, false
	}
	var value any
	found := false
	for scope := Local; scope >= Global; scope-- {
		if value, found = s.layers[scope].values[name]; found {
			break
		}
	}
	if !found {
		return "", false
	}
	text := stringify(value)
	if !strings.Contains(text, "{{") {
		return text, true
	}
	resolving[name] = true
	text = s.replace(text, depth+1, resolving)
	delete(resolving, name)
	return text, true
}

func stringify(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	if number, ok := value.(json.Number); ok {
		return number.String()
	}
	encoded, err := json.Marshal(value)
	if err == nil {
		return string(encoded)
	}
	return fmt.Sprint(value)
}

type cryptoSource struct{}

// Intn returns cryptographically random integer in [0, n).
func (cryptoSource) Intn(n int) int {
	if n <= 0 {
		panic("invalid argument to Intn")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return int(value.Int64())
}

// Read fills p with cryptographically secure random bytes.
func (cryptoSource) Read(p []byte) (int, error) { return rand.Read(p) }
