package engine

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// maxExactInt is the largest integer a JavaScript number holds exactly.
const maxExactInt = 1 << 53

// LoadCSV reads iteration data from CSV: the first row names the columns and
// every following row is one iteration. Values are strings, as in Postman.
// A UTF-8 byte-order mark is ignored and every row must have as many fields
// as the header.
func LoadCSV(r io.Reader) ([]map[string]any, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read CSV data: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("CSV data file is empty")
	}
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	seen := map[string]bool{}
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("CSV column %d has no name", i+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("CSV column %q appears twice", name)
		}
		seen[name] = true
		header[i] = name
	}
	var rows []map[string]any
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV data: %w", err)
		}
		row := make(map[string]any, len(header))
		for i, name := range header {
			row[name] = record[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// LoadJSON reads iteration data from JSON: an array of objects, one per
// iteration, or a single object for one iteration. Values keep their JSON
// types; integers too large for a JavaScript number become strings so they do
// not lose digits.
func LoadJSON(r io.Reader) ([]map[string]any, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("read JSON data file: %w", err)
	}
	if dec.More() {
		return nil, errors.New("JSON data file has more than one value")
	}
	switch v := raw.(type) {
	case map[string]any:
		return []map[string]any{convertObject(v)}, nil
	case []any:
		rows := make([]map[string]any, 0, len(v))
		for i, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("JSON data row %d is not an object", i+1)
			}
			rows = append(rows, convertObject(obj))
		}
		return rows, nil
	}
	return nil, errors.New("JSON data file must be an array of objects")
}

// LoadDataFile loads iteration data from a .csv or .json file.
func LoadDataFile(path string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		return LoadCSV(f)
	case ".json":
		return LoadJSON(f)
	}
	return nil, fmt.Errorf("unsupported data file %q: use .csv or .json", filepath.Base(path))
}

func convertObject(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = convertValue(v)
	}
	return out
}

func convertValue(v any) any {
	switch n := v.(type) {
	case json.Number:
		if f, err := n.Float64(); err == nil && !math.IsInf(f, 0) {
			isInteger := !strings.ContainsAny(n.String(), ".eE")
			if isInteger && (f > maxExactInt || f < -maxExactInt) {
				return n.String()
			}
			return f
		}
		return n.String()
	case map[string]any:
		return convertObject(n)
	case []any:
		out := make([]any, len(n))
		for i, item := range n {
			out[i] = convertValue(item)
		}
		return out
	}
	return v
}
