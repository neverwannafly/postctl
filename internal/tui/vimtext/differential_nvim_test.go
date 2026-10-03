package vimtext

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rivo/uniseg"
	"github.com/shubham/postctl/internal/ui"
)

type differentialCase struct {
	Name      string   `json:"name"`
	Source    string   `json:"-"`
	Text      string   `json:"-"`
	Lines     []string `json:"lines"`
	Row       int      `json:"row"`
	ByteCol   int      `json:"byteCol"`
	Keys      string   `json:"-"`
	NvimKeys  string   `json:"keys"`
	Allow     string   `json:"-"`
	Operators []string `json:"-"`
	Commands  int      `json:"-"`
}

type differentialResult struct {
	Name    string   `json:"name"`
	Lines   []string `json:"lines"`
	Row     int      `json:"row"`
	ByteCol int      `json:"byteCol"`
	Mode    string   `json:"mode"`
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
	assertDifferentialCorpusCoverage(t, cases)
	cases = append(cases, newDifferentialCase(
		"deviation/literal-search", "deviation", "ab.", 0, 0, "/.<CR>", "literal-search",
	))
	randomCases := differentialRandomCases(500)
	assertRandomCoverage(t, randomCases)
	logRandomDistribution(t, randomCases)
	cases = append(cases, randomCases...)
	results := runNvimCases(t, nvim, cases)
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
		gotText, gotRow, gotCol, gotMode := runVimtextCase(t, tc)
		want := results[i]
		if want.Error != "" || want.Row < 0 || want.Row >= len(want.Lines) {
			mismatches = append(mismatches, fmt.Sprintf("%s [%s]: nvim oracle error", tc.Name, tc.Source))
			continue
		}
		wantText := strings.Join(want.Lines, "\n")
		wantCol := graphemeAtByte(want.Lines[want.Row], want.ByteCol)
		if want.Error == "" && gotText == wantText && gotRow == want.Row && gotCol == wantCol && gotMode == want.Mode {
			continue
		}
		line := fmt.Sprintf(
			"%s [%s] keys=%q: textBytes=%d/%d lines=%d/%d cursor=(%d,%d)/(%d,%d) mode=%s/%s nvimError=%t",
			tc.Name, tc.Source, tc.Keys, len(gotText), len(wantText), len(strings.Split(gotText, "\n")),
			len(want.Lines), gotRow, gotCol, want.Row, wantCol, gotMode, want.Mode, want.Error != "",
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
		if differentialExclusion(tc) != "" {
			continue
		}
		out = append(out, newDifferentialCase("corpus/"+tc.name, "corpus", tc.start, tc.row, tc.col, tc.keys, ""))
	}
	return out
}

func differentialExclusion(tc corpusCase) string {
	switch {
	case tc.opts.SingleLine:
		return "single-line"
	case tc.opts.ReadOnly:
		return "read-only"
	case tc.opts.StartInsert:
		return "start-insert"
	case tc.opts.Completer != nil:
		return "completion"
	case tc.opts.Highlighter != nil:
		return "highlighting"
	case tc.wantEvents != nil:
		return "routing-events"
	case strings.Contains(tc.keys, "<Paste:"):
		return "clipboard-paste"
	default:
		return ""
	}
}

func assertDifferentialCorpusCoverage(t *testing.T, included []differentialCase) {
	t.Helper()
	const (
		wantTotal    = 488
		wantEligible = 476
	)
	all := allCorpusCases()
	if len(all) != wantTotal {
		t.Fatalf("corpus case count = %d, want pinned %d", len(all), wantTotal)
	}
	if len(included) != wantEligible {
		t.Fatalf("eligible corpus count = %d, want pinned %d", len(included), wantEligible)
	}
	got := map[string]int{}
	for _, tc := range all {
		if reason := differentialExclusion(tc); reason != "" {
			got[reason]++
		}
	}
	want := map[string]int{"single-line": 9, "read-only": 2, "clipboard-paste": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("corpus exclusion categories = %v, want %v", got, want)
	}
}

func differentialRandomCases(count int) []differentialCase {
	buffers := []string{
		"{\n  \"a\": [1, {\"b\": 2}]\n}",
		"function f() {\n  return call(a[0], \"[\")\n}",
		`"one" "two"` + "\n" + "it's 'x'",
		"  alpha\n    beta\ngamma",
		"a\n\nb\n   \nc",
		"x",
		"",
		"(\n  [x]\n)",
		"one two.three\nfour",
		"first\nsecond\nthird",
		"café déjà",
		"世界 alpha\nbeta 世界",
		"👩‍💻 dev\né界",
	}
	requiredOperators := randomRequiredOperators()
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
		var generated generatedKeys
		if i < len(requiredOperators)*20 {
			generated = randomOperatorCommand(rng, requiredOperators[i%len(requiredOperators)])
		} else {
			generated = randomGrammarCommand(rng)
		}
		for notationKeyCount(generated.Keys) < 3 && generated.Commands < 3 {
			generated.Keys = "0" + generated.Keys
			generated.Commands++
		}
		if n := notationKeyCount(generated.Keys); n < 3 || n > 12 {
			panic(fmt.Sprintf("random notation has %d keys", n))
		}
		name := fmt.Sprintf("random/%03d/buf%d/r%d/c%d", i, bufferIndex, row, col)
		tc := newDifferentialCase(name, "random", text, row, col, generated.Keys, "")
		tc.Operators, tc.Commands = generated.Operators, generated.Commands
		out = append(out, tc)
	}
	return out
}

type generatedKeys struct {
	Keys      string
	Operators []string
	Commands  int
}

func randomRequiredOperators() []string {
	return []string{"d", "c", "y", "p", "P", "J", "r", "~", "x", "X", "s", "D", "C", ">>", "<<"}
}

func randomGrammarCommand(rng *rand.Rand) generatedKeys {
	switch rng.Intn(6) {
	case 0, 1:
		ops := randomRequiredOperators()
		return randomOperatorCommand(rng, ops[rng.Intn(len(ops))])
	case 2:
		return generatedKeys{Keys: randomMotion(rng), Commands: 1}
	case 3:
		return randomVisualCommand(rng)
	case 4:
		return randomInsertCommand(rng)
	default:
		return randomSearchOrHistoryCommand(rng)
	}
}

func randomOperatorCommand(rng *rand.Rand, op string) generatedKeys {
	count := randomCount(rng)
	switch op {
	case "d", "y":
		return generatedKeys{
			Keys: count + op + randomOperatorTarget(rng), Operators: []string{op}, Commands: 1,
		}
	case "c":
		return generatedKeys{
			Keys:      count + "c" + randomOperatorTarget(rng) + "Q<Esc>",
			Operators: []string{"c"}, Commands: 1,
		}
	case "p", "P":
		return generatedKeys{
			Keys: "yiw" + count + op, Operators: []string{"y", op}, Commands: 2,
		}
	case "J":
		return generatedKeys{Keys: count + "J", Operators: []string{"J"}, Commands: 1}
	case "r":
		replacement := []string{"Z", "é", "<CR>"}[rng.Intn(3)]
		return generatedKeys{Keys: count + "r" + replacement, Operators: []string{"r"}, Commands: 1}
	case "~", "x", "X", "D":
		return generatedKeys{Keys: count + op, Operators: []string{op}, Commands: 1}
	case "s", "C":
		return generatedKeys{
			Keys: count + op + "Q<Esc>", Operators: []string{op}, Commands: 1,
		}
	case ">>":
		return generatedKeys{Keys: count + ">>", Operators: []string{">>"}, Commands: 1}
	default:
		return generatedKeys{Keys: count + "<LT><LT>", Operators: []string{"<<"}, Commands: 1}
	}
}

func randomCount(rng *rand.Rand) string {
	if rng.Intn(2) == 0 {
		return ""
	}
	return strconv.Itoa(2 + rng.Intn(3))
}

func randomOperatorTarget(rng *rand.Rand) string {
	targets := []string{
		"w", "b", "e", "W", "B", "E", "ge", "0", "^", "$",
		"fa", "fz", "ta", "tz", "Fa", "Fz", "Ta", "Tz", ";", ",", "%",
		"gg", "G", "j", "k", "h", "l",
		"iw", "aw", "iW", "aW", `i"`, `a"`, "i'", "a'", "i(", "a(", "i[", "a[", "i{", "a{",
	}
	return targets[rng.Intn(len(targets))]
}

func randomMotion(rng *rand.Rand) string {
	motions := []string{
		"w", "b", "e", "W", "B", "E", "ge", "0", "^", "$",
		"fa", "fz", "ta", "tz", "Fa", "Fz", "Ta", "Tz", ";", ",", "%",
		"gg", "G", "j", "k", "h", "l",
	}
	return randomCount(rng) + motions[rng.Intn(len(motions))]
}

func randomVisualCommand(rng *rand.Rand) generatedKeys {
	mode := []string{"v", "V"}[rng.Intn(2)]
	targets := []string{
		"w", "b", "e", "W", "B", "E", "0", "^", "$", "fa", "fz", "ta", "tz",
		"%", "gg", "G", "j", "k", "h", "l",
		"iw", "aw", "iW", "aW", `i"`, `a"`, "i'", "a'", "i(", "a(", "i[", "a[", "i{", "a{",
	}
	op := []string{"d", "c", "y", ">", "<", "~", "J"}[rng.Intn(7)]
	opKeys := op
	if op == "<" {
		opKeys = "<LT>"
	}
	keys := mode + targets[rng.Intn(len(targets))] + opKeys
	if op == "c" {
		keys += "Q<Esc>"
	}
	return generatedKeys{Keys: keys, Operators: []string{op}, Commands: 1}
}

func randomInsertCommand(rng *rand.Rand) generatedKeys {
	open := []string{"i", "a", "I", "A", "o", "O"}[rng.Intn(6)]
	body := []string{
		"Q", "é", "界", "x<Left>y", "x<Right>y", "xy<BS>", "one two<C-w>",
	}[rng.Intn(7)]
	keys := open + body + "<Esc>"
	if rng.Intn(3) == 0 {
		keys += "."
		return generatedKeys{Keys: keys, Commands: 2}
	}
	return generatedKeys{Keys: keys, Commands: 1}
}

func randomSearchOrHistoryCommand(rng *rand.Rand) generatedKeys {
	commands := []generatedKeys{
		{Keys: "/a<CR>", Commands: 1},
		{Keys: "?x<CR>", Commands: 1},
		{Keys: "/a<CR>n", Commands: 2},
		{Keys: "?x<CR>N", Commands: 2},
		{Keys: "iQ<Esc>u", Commands: 2},
		{Keys: "iQ<Esc>u<C-r>", Commands: 3},
		{Keys: "xu<C-r>", Operators: []string{"x"}, Commands: 3},
		{Keys: "Ju<C-r>", Operators: []string{"J"}, Commands: 3},
	}
	return commands[rng.Intn(len(commands))]
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

func assertRandomCoverage(t *testing.T, cases []differentialCase) {
	t.Helper()
	if len(cases) != 500 {
		t.Fatalf("random case count = %d, want 500", len(cases))
	}
	operators := map[string]int{}
	for _, tc := range cases {
		n := notationKeyCount(tc.Keys)
		if n < 3 || n > 12 {
			t.Fatalf("%s has %d keys, want 3..12", tc.Name, n)
		}
		if tc.Commands < 1 || tc.Commands > 3 {
			t.Fatalf("%s has %d commands, want 1..3", tc.Name, tc.Commands)
		}
		for _, op := range tc.Operators {
			operators[op]++
		}
	}
	for _, op := range randomRequiredOperators() {
		if operators[op] < 20 {
			t.Fatalf("random operator %q appears %d times, want at least 20", op, operators[op])
		}
	}
}

func logRandomDistribution(t *testing.T, cases []differentialCase) {
	t.Helper()
	first, operators := map[string]int{}, map[string]int{}
	for _, tc := range cases {
		first[firstNotationKey(tc.Keys)]++
		for _, op := range tc.Operators {
			operators[op]++
		}
	}
	t.Logf("random first-key distribution: %s", formatDistribution(first))
	t.Logf("random operator distribution: %s", formatDistribution(operators))
}

func firstNotationKey(notation string) string {
	if strings.HasPrefix(notation, "<") {
		if end := strings.IndexByte(notation, '>'); end >= 0 {
			return notation[:end+1]
		}
	}
	cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(notation, -1)
	return cluster
}

func formatDistribution(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, " ")
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

func runVimtextCase(t *testing.T, tc differentialCase) (string, int, int, string) {
	t.Helper()
	m := New(ui.NewTheme(true, true, false), Options{})
	m.SetValue(tc.Text)
	m.SetCursor(tc.Row, graphemeAtByte(tc.Lines[tc.Row], tc.ByteCol))
	_ = m.Focus()
	for _, msg := range parseKeys(t, tc.Keys) {
		_, _ = m.Update(msg)
	}
	row, col := m.Cursor()
	return m.Value(), row, col, m.Mode().String()
}

func runNvimCases(t *testing.T, nvim string, cases []differentialCase) []differentialResult {
	t.Helper()
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "cases.json")
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
	results := make([]differentialResult, len(cases))
	jobs := make(chan int)
	var wg sync.WaitGroup
	const workers = 4
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = runNvimCase(nvim, scriptPath, inputPath, dir, i)
			}
		}()
	}
	for i := range cases {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func runNvimCase(nvim, scriptPath, inputPath, dir string, index int) differentialResult {
	outputPath := filepath.Join(dir, fmt.Sprintf("result-%04d.json", index))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nvim, "--clean", "--headless", "-n", "-l", scriptPath)
	cmd.Env = append(os.Environ(),
		"VIMTEXT_INPUT="+inputPath,
		"VIMTEXT_OUTPUT="+outputPath,
		"VIMTEXT_INDEX="+strconv.Itoa(index+1),
	)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return differentialResult{Error: "oracle timeout"}
	}
	if err != nil {
		return differentialResult{Error: fmt.Sprintf("oracle exit error; outputBytes=%d", len(output))}
	}
	resultData, err := os.ReadFile(outputPath)
	if err != nil {
		return differentialResult{Error: "oracle result missing"}
	}
	var result differentialResult
	if err := json.Unmarshal(resultData, &result); err != nil {
		return differentialResult{Error: "oracle result invalid"}
	}
	return result
}

func nvimKeys(notation string) string {
	replacer := strings.NewReplacer(
		"<LT>", "<", "<CR>", "<CR>", "<Esc>", "<Esc>", "<C-r>", "<C-r>",
		"<C-w>", "<C-w>", "<C-u>", "<C-u>", "<Tab>", "<Tab>", "<BS>", "<BS>",
		"<Del>", "<Del>", "<Left>", "<Left>", "<Right>", "<Right>", "<Up>", "<Up>", "<Down>", "<Down>",
	)
	return replacer.Replace(notation)
}

const nvimOracleLua = `
local input = os.getenv("VIMTEXT_INPUT")
local output = os.getenv("VIMTEXT_OUTPUT")
local index = tonumber(os.getenv("VIMTEXT_INDEX"))
local cases = vim.json.decode(table.concat(vim.fn.readfile(input), "\n"))
local case = cases[index]
local result = { name = case.name }
vim.o.autoindent = true
vim.o.expandtab = true
vim.o.shiftwidth = 2
vim.o.softtabstop = 2
local ok, err = pcall(function()
  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_set_current_buf(buf)
  for _, register in ipairs({ '"', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-' }) do
    vim.fn.setreg(register, {})
  end
  vim.fn.setreg("/", "")
  vim.bo[buf].undolevels = -1
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, case.lines)
  vim.bo[buf].undolevels = 1000
  vim.api.nvim_win_set_cursor(0, { case.row + 1, case.byteCol })
  local keys = vim.api.nvim_replace_termcodes(case.keys, true, false, true)
  vim.api.nvim_feedkeys(keys, "ntx", false)
  result.lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  local cursor = vim.api.nvim_win_get_cursor(0)
  result.row = cursor[1] - 1
  result.byteCol = cursor[2]
  local mode = vim.api.nvim_get_mode().mode
  if mode:sub(1, 1) == "i" then
    result.mode = "INSERT"
  elseif mode == "v" or mode == "\22" then
    result.mode = "VISUAL"
  elseif mode == "V" then
    result.mode = "VISUAL LINE"
  else
    result.mode = "NORMAL"
  end
end)
if not ok then
  result.error = tostring(err)
  result.lines = {}
  result.row = 0
  result.byteCol = 0
  result.mode = "NORMAL"
end
vim.fn.writefile({ vim.json.encode(result) }, output)
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
	for _, tc := range []struct {
		name string
		text string
		off  int
		want int
	}{
		{name: "inside accent", text: "éx", off: 1, want: 0},
		{name: "accent boundary", text: "éx", off: len("é"), want: 1},
		{name: "inside ZWJ", text: "👩‍💻x", off: 4, want: 0},
		{name: "ZWJ boundary", text: "👩‍💻x", off: len("👩‍💻"), want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := graphemeAtByte(tc.text, tc.off); got != tc.want {
				t.Fatalf("graphemeAtByte offset %d = %d, want %d", tc.off, got, tc.want)
			}
		})
	}
}

func TestDifferentialRandomCaseShape(t *testing.T) {
	cases := differentialRandomCases(500)
	assertRandomCoverage(t, cases)
}
