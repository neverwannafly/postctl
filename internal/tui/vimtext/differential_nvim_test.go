package vimtext

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/ui"
)

type differentialCase struct {
	Name     string   `json:"name"`
	Source   string   `json:"-"`
	Text     string   `json:"-"`
	Lines    []string `json:"lines"`
	Row      int      `json:"row"`
	ByteCol  int      `json:"byteCol"`
	Keys     string   `json:"-"`
	NvimKeys string   `json:"keys"`
	Allow    string   `json:"-"`
}

type differentialResult struct {
	Name    string   `json:"name"`
	Lines   []string `json:"lines"`
	Row     int      `json:"row"`
	ByteCol int      `json:"byteCol"`
	Error   string   `json:"error,omitempty"`
}

// TestDifferentialNvim compares vimtext with a clean nvim oracle. It is opt-in
// because nvim is a development tool and is not part of postctl's toolchain.
func TestDifferentialNvim(t *testing.T) {
	if os.Getenv("VIMTEXT_NVIM") != "1" {
		t.Skip("set VIMTEXT_NVIM=1 to run the nvim differential oracle")
	}
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim is not on PATH")
	}
	cases := differentialCorpusCases()
	cases = append(cases, newDifferentialCase(
		"deviation/literal-search", "deviation", "ab.", 0, 0, "/.<CR>", "literal-search",
	))
	cases = append(cases, differentialRandomCases(500)...)
	results := runNvimBatch(t, nvim, cases)
	if len(results) != len(cases) {
		t.Fatalf("nvim returned %d results for %d cases", len(results), len(cases))
	}
	var mismatches, allowed []string
	corpusCompared, randomCompared := 0, 0
	for i, tc := range cases {
		switch tc.Source {
		case "random":
			randomCompared++
		case "corpus":
			corpusCompared++
		}
		gotText, gotRow, gotCol := runVimtextCase(t, tc)
		want := results[i]
		if want.Error != "" || want.Row < 0 || want.Row >= len(want.Lines) {
			mismatches = append(mismatches, fmt.Sprintf("%s [%s]: nvim oracle error", tc.Name, tc.Source))
			continue
		}
		wantText := strings.Join(want.Lines, "\n")
		wantCol := graphemeAtByte(want.Lines[want.Row], want.ByteCol)
		if want.Error == "" && gotText == wantText && gotRow == want.Row && gotCol == wantCol {
			continue
		}
		line := fmt.Sprintf(
			"%s [%s] keys=%q: textBytes=%d/%d lines=%d/%d cursor=(%d,%d)/(%d,%d) nvimError=%t",
			tc.Name, tc.Source, tc.Keys, len(gotText), len(wantText), len(strings.Split(gotText, "\n")),
			len(want.Lines), gotRow, gotCol, want.Row, wantCol, want.Error != "",
		)
		if tc.Allow != "" {
			if differentialDeviationReasons[tc.Allow] == "" {
				t.Fatalf("%s uses unknown deviation %q", tc.Name, tc.Allow)
			}
			allowed = append(allowed, line+" allow="+tc.Allow)
		} else {
			mismatches = append(mismatches, line)
		}
	}
	report := append([]string{
		fmt.Sprintf("corpus=%d random=%d mismatches=%d allowed=%d", corpusCompared, randomCompared, len(mismatches), len(allowed)),
	}, mismatches...)
	report = append(report, allowed...)
	reportFile, err := os.CreateTemp("", "vimtext-nvim-mismatches-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	reportPath := reportFile.Name()
	if err := reportFile.Chmod(0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reportFile.WriteString(strings.Join(report, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := reportFile.Close(); err != nil {
		t.Fatal(err)
	}
	t.Log(report[0])
	if len(mismatches) > 0 {
		t.Fatalf("nvim differential mismatches: %d; report: %s", len(mismatches), reportPath)
	}
}

func differentialCorpusCases() []differentialCase {
	var out []differentialCase
	for _, tc := range allCorpusCases() {
		if tc.opts.SingleLine || tc.opts.ReadOnly || tc.opts.StartInsert ||
			tc.opts.Completer != nil || tc.opts.Highlighter != nil ||
			tc.wantEvents != nil || strings.Contains(tc.keys, "<Paste:") {
			continue
		}
		out = append(out, newDifferentialCase("corpus/"+tc.name, "corpus", tc.start, tc.row, tc.col, tc.keys, ""))
	}
	return out
}

func differentialRandomCases(count int) []differentialCase {
	buffers := []string{
		"{\n  \"a\": [1, 2]\n}",
		"function f() {\n  return (a + b)\n}",
		`"one" "two"` + "\n" + "it's 'x'",
		"  alpha\n    beta\ngamma",
		"a\n\nb\n   \nc",
		"word",
		"",
		"(\n  [x]\n)",
		"one two.three\nfour",
		"first\nsecond\nthird",
	}
	cores := []string{
		"10h", "10l", "10j", "10k", "10w", "10e", "10W", "10E",
		"2gg", "10G", "d%u", "d0u", "d^u", "d$u",
		"dwu", "dWu", "deu", "dbu", "dlu", "ddu", "dju", "diw", "daw", "di(", "da[",
		"iX<Esc>", "aY<Esc>", "oZ<Esc>", "OQ<Esc>",
		"iX<Esc>u", "iX<Esc>u<C-r>", "iX<Esc>.", "Vdu", "viwy", "vawd",
		"10x", "1>>", "1<LT><LT>", "1fa", "1tz", "1F(", "1T]",
		"/a<CR>", "?x<CR>", "/a<CR>n", "/a<CR>2n", "d/a<CR>",
	}
	rng := rand.New(rand.NewSource(0x56494d))
	out := make([]differentialCase, 0, count)
	for i := range count {
		bufferIndex := rng.Intn(len(buffers))
		text := buffers[bufferIndex]
		lines := strings.Split(text, "\n")
		row := rng.Intn(len(lines))
		col := 0
		if n := len(clusters(lines[row])); n > 0 {
			col = rng.Intn(n)
		}
		notation := cores[rng.Intn(len(cores))]
		if n := notationKeyCount(notation); n < 3 || n > 12 {
			panic(fmt.Sprintf("random notation has %d keys", n))
		}
		name := fmt.Sprintf("random/%03d/buf%d/r%d/c%d", i, bufferIndex, row, col)
		out = append(out, newDifferentialCase(name, "random", text, row, col, notation, ""))
	}
	return out
}

func notationKeyCount(notation string) int {
	count := 0
	for notation != "" {
		count++
		if notation[0] == '<' {
			end := strings.IndexByte(notation, '>')
			if end >= 0 {
				notation = notation[end+1:]
				continue
			}
		}
		_, rest, _, _ := uniseg.FirstGraphemeClusterInString(notation, -1)
		notation = rest
	}
	return count
}

func newDifferentialCase(name, source, text string, row, col int, keys, allow string) differentialCase {
	lines := strings.Split(text, "\n")
	row = min(max(0, row), len(lines)-1)
	col = min(max(0, col), len(clusters(lines[row])))
	return differentialCase{
		Name: name, Source: source, Text: text, Lines: lines, Row: row,
		ByteCol: byteAt(lines[row], col), Keys: keys, NvimKeys: nvimKeys(keys), Allow: allow,
	}
}

func runVimtextCase(t *testing.T, tc differentialCase) (string, int, int) {
	t.Helper()
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue(tc.Text)
	m.SetCursor(tc.Row, graphemeAtByte(tc.Lines[tc.Row], tc.ByteCol))
	_ = m.Focus()
	for _, msg := range parseKeys(t, tc.Keys) {
		_, _ = m.Update(msg)
	}
	row, col := m.Cursor()
	return m.Value(), row, col
}

func runNvimBatch(t *testing.T, nvim string, cases []differentialCase) []differentialResult {
	t.Helper()
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "cases.json")
	outputPath := filepath.Join(dir, "results.json")
	scriptPath := filepath.Join(dir, "oracle.lua")
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(nvimOracleLua), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, nvim, "--clean", "--headless", "-n", "-l", scriptPath)
	cmd.Env = append(os.Environ(), "VIMTEXT_INPUT="+inputPath, "VIMTEXT_OUTPUT="+outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("nvim oracle timed out after 2m")
		}
		t.Fatalf("nvim oracle failed: %v (output bytes=%d)", err, len(output))
	}
	resultData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var results []differentialResult
	if err := json.Unmarshal(resultData, &results); err != nil {
		t.Fatal(err)
	}
	return results
}

func nvimKeys(notation string) string {
	notation = insertArrowUndoBreaks(notation)
	replacer := strings.NewReplacer(
		"<LT>", "<", "<CR>", "<CR>", "<Esc>", "<Esc>", "<C-r>", "<C-r>",
		"<C-w>", "<C-w>", "<C-u>", "<C-u>", "<Tab>", "<Tab>", "<BS>", "<BS>",
		"<Del>", "<Del>", "<Left>", "<Left>", "<Right>", "<Right>", "<Up>", "<Up>", "<Down>", "<Down>",
	)
	return replacer.Replace(notation)
}

func insertArrowUndoBreaks(notation string) string {
	var out strings.Builder
	insert, visual, changePending, objectPending := false, false, false, false
	for notation != "" {
		token := notation[:1]
		notation = notation[1:]
		if token == "<" {
			end := strings.IndexByte(notation, '>')
			if end >= 0 {
				token += notation[:end+1]
				notation = notation[end+1:]
			}
		}
		if insert && (token == "<Left>" || token == "<Right>" || token == "<Up>" || token == "<Down>") {
			out.WriteString("<C-g>u")
		}
		out.WriteString(token)
		if token == "<Esc>" {
			insert, visual, changePending, objectPending = false, false, false, false
			continue
		}
		if insert || len(token) != 1 {
			continue
		}
		if objectPending {
			insert, changePending, objectPending = true, false, false
			continue
		}
		if changePending {
			if token == "i" || token == "a" {
				objectPending = true
			} else {
				insert, changePending = true, false
			}
			continue
		}
		switch token {
		case "v", "V":
			visual = true
		case "c":
			if visual {
				insert, visual = true, false
			} else {
				changePending = true
			}
		case "i", "I", "a", "A", "o", "O", "s", "S", "C":
			insert = true
		}
	}
	return out.String()
}

const nvimOracleLua = `
local input = os.getenv("VIMTEXT_INPUT")
local output = os.getenv("VIMTEXT_OUTPUT")
local cases = vim.json.decode(table.concat(vim.fn.readfile(input), "\n"))
local results = {}
vim.o.autoindent = true
vim.o.expandtab = true
vim.o.shiftwidth = 2
vim.o.softtabstop = 2
for _, case in ipairs(cases) do
  local result = { name = case.name }
  local ok, err = pcall(function()
    local buf = vim.api.nvim_create_buf(false, true)
    vim.api.nvim_set_current_buf(buf)
    vim.fn.setreg("/", "")
    vim.bo[buf].undolevels = -1
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, case.lines)
    vim.bo[buf].undolevels = 1000
    vim.api.nvim_win_set_cursor(0, { case.row + 1, case.byteCol })
    local keys = vim.api.nvim_replace_termcodes(case.keys, true, false, true)
    vim.api.nvim_feedkeys(keys, "nx", false)
    result.lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
    local cursor = vim.api.nvim_win_get_cursor(0)
    result.row = cursor[1] - 1
    result.byteCol = cursor[2]
    vim.api.nvim_buf_delete(buf, { force = true })
  end)
  if not ok then
    result.error = tostring(err)
    result.lines = {}
    result.row = 0
    result.byteCol = 0
  end
  table.insert(results, result)
end
vim.fn.writefile({ vim.json.encode(results) }, output)
vim.cmd("qa!")
`

func TestDifferentialDeviationAllowlist(t *testing.T) {
	if len(differentialDeviationReasons) != 1 {
		t.Fatalf("deviation allowlist has %d entries, want 1", len(differentialDeviationReasons))
	}
	if differentialDeviationReasons["literal-search"] == "" {
		t.Fatal("literal-search deviation needs a reason")
	}
}

func TestNvimKeyTranslation(t *testing.T) {
	got := nvimKeys("<LT><Esc><C-r><Left>")
	if got != "<<Esc><C-r><Left>" {
		t.Fatalf("termcode translation = %q", got)
	}
	if uniseg.GraphemeClusterCount("👩‍💻") != 1 {
		t.Fatal("grapheme test precondition failed")
	}
}

func TestDifferentialRandomCaseShape(t *testing.T) {
	cases := differentialRandomCases(500)
	if len(cases) != 500 {
		t.Fatalf("random case count = %d, want 500", len(cases))
	}
	for _, tc := range cases {
		n := notationKeyCount(tc.Keys)
		if n < 3 || n > 12 {
			t.Fatalf("%s has %d keys, want 3..12", tc.Name, n)
		}
	}
}
