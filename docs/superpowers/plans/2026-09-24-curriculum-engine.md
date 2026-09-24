# Curriculum Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `tattva`, a Go CLI that asks Claude Code to design a build-your-own-x curriculum for a target system, writes it as files inside the learner's project, and tracks their progress from the terminal.

**Architecture:** One Go module, package `main`, standard library only. `spec.go` defines the Learning Spec, its validity rules and the JSON Schemas generated from it. `claude.go` runs `claude -p` in a subprocess. `generate.go` runs the design, revise and expand calls, with one repair each. `render.go` turns the spec into markdown. `registry.go` tracks projects and file hashes in `~/.tattva/projects.json`. `progress.go` keeps the progress log. `main.go` dispatches the commands.

**Tech Stack:** Go 1.25 (standard library only), Claude Code CLI 2.1.281 (`claude -p`), `text/template`, `embed`.

**Spec:** `docs/superpowers/specs/2026-09-24-curriculum-engine-design.md` (read it with this plan). `§N` in the spec refers to `docs/specs.md`.

## Global Constraints

- Go 1.25, module path `github.com/0x1DKFA/tattva`, package `main`, **no third-party modules**.
- Work on branch `curriculum-engine`. Make one commit per task, with conventional prefixes (`feat:`, `docs:`, `test:`). **Commit messages have no `Co-Authored-By` or any other Claude attribution** (the user's instruction).
- Run `gofmt -w .` before each commit. `go vet ./...` and `go test ./...` must pass before each commit.
- Tests never call the real Claude. `TestMain` limits `PATH` to `/usr/bin:/bin`. Any test that touches the registry sets `HOME` to a temp dir.
- Project files: `curriculum/spec.json`, `curriculum/progress.jsonl`, `curriculum/README.md`, `curriculum/steps/NN-<step id>.md`, `curriculum/.failed/`. Registry: `~/.tattva/projects.json`.
- Every Claude call is `claude -p --output-format json --json-schema <schema> --system-prompt <prompt> --tools WebSearch,WebFetch --allowedTools WebSearch,WebFetch --safe-mode --no-session-persistence [--model <m>]`. Repair calls use `--tools ""` and no `--allowedTools`. The prompt goes on stdin, and the process runs in an empty temp dir.
- Exact values: at most 99 steps; at most 2 new concepts per step; exactly 3 hints; server port 1024–65535; cli port 0; 20-minute timeout per call; expand runs 3 phases at a time; one repair call per failed validation.
- Verified by testing the installed CLI on 2026-09-24:
  - Results arrive as JSON on stdout, with `is_error`, `result`, `structured_output` (already a parsed object) and `total_cost_usd`.
  - An error exits with status 1 but still prints that JSON with `is_error: true`.
  - `--safe-mode` keeps the user's SessionStart hooks out of the call.
  - WebFetch works in `-p` mode with `--allowedTools`.
- One small change from the spec's code layout: the two markdown templates live in `templates/*.tmpl` (built in with `embed`), not inside `render.go`. They contain backticks, which can't appear inside Go raw strings.

## Review Focus

1. **`claude` isn't installed or isn't on PATH.** The user expects a clear "install Claude Code" message, not `exec: "claude": executable file not found`. The test is `TestRunClaudeMissingBinary` in Task 4.
2. **Ctrl-C during `expand`.** Phases that already finished stay saved. The interrupted phase isn't saved, and re-running retries it. The test is `TestExpandInterruptKeepsFinishedPhases` in Task 9.
3. **A hand edit leaves `spec.json` as broken JSON** (not just breaking a rule). Every command stops with an error naming the file, and nothing panics. The test is `TestOpenProjectRejectsMalformedJSON` in Task 6.
4. **`~/.tattva/projects.json` is corrupt.** Commands keep working, a warning is printed, and the registry rebuilds itself. The test is `TestCorruptRegistryIsIgnored` in Task 6.
5. **A project command is run outside any project.** The error says there's no tattva project here and suggests `tattva new`. The test is `TestOpenProjectOutsideProject` in Task 6, plus the run-level check in `TestRunNextDoneStatus` in Task 7.

---

### Task 1: Branch, docs commit and CLI skeleton

**Files:**
- Modify: `.gitignore` (append)
- Create: `go.mod`
- Create: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `run(ctx context.Context, dir string, args []string, out io.Writer) error`. Later tasks add `case` blocks before its `default:`.
  - `newFlags(name string) *flag.FlagSet`
  - `parseArgs(fs *flag.FlagSet, args []string) ([]string, error)`
  - `TestMain`, which limits `PATH` for every test in the package

- [ ] **Step 1: Create the branch and commit the docs**

```bash
cd /Users/rohitkk074/Documents/projects/tattva
git checkout -b curriculum-engine
git add docs/specs.md docs/superpowers/specs/2026-09-24-curriculum-engine-design.md docs/superpowers/plans/2026-09-24-curriculum-engine.md
git commit -m "docs: add product vision, curriculum engine design and plan"
```

- [ ] **Step 2: Create `go.mod` and ignore the built binary**

`go.mod`:

```
module github.com/0x1DKFA/tattva

go 1.25
```

Append to `.gitignore`:

```
# tattva binary
/tattva
```

- [ ] **Step 3: Write the failing tests**

`main_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// TestMain hides the real claude CLI so no test can ever call it.
func TestMain(m *testing.M) {
	os.Setenv("PATH", "/usr/bin:/bin")
	os.Exit(m.Run())
}

func TestParseArgsFindsFlagsAfterPositionals(t *testing.T) {
	fs := newFlags("new")
	lang := fs.String("lang", "", "")
	pos, err := parseArgs(fs, []string{"Redis", "--lang", "go", "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if *lang != "go" || strings.Join(pos, "|") != "Redis|extra" {
		t.Fatalf("lang=%q pos=%q", *lang, pos)
	}
	if _, err := parseArgs(newFlags("next"), []string{"--bogus"}); err == nil {
		t.Fatal("unknown flags must be an error")
	}
}

func TestRunHelpAndUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), t.TempDir(), []string{"help"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `tattva new "<target>" --lang <lang>`) {
		t.Fatalf("usage missing: %s", out.String())
	}
	err := run(context.Background(), t.TempDir(), []string{"bogus"}, &out)
	if err == nil || !strings.Contains(err.Error(), `unknown command "bogus"`) {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: newFlags`, `undefined: parseArgs` and `undefined: run`.

- [ ] **Step 5: Write `main.go`**

```go
// Command tattva turns a system you've used into a curriculum you build yourself.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
)

const usage = `tattva turns a system you've used into a curriculum you build yourself.

Usage:
  tattva new "<target>" --lang <lang> [--model <m>]   design a curriculum here
  tattva revise "<feedback>" [--force] [--model <m>]  rewrite the outline
  tattva expand [--model <m>]                         write the step details
  tattva next                                         start the next step
  tattva done [step]                                  complete a step
  tattva status                                       show progress
  tattva list                                         list your projects
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	dir, err := os.Getwd()
	if err == nil {
		err = run(ctx, dir, os.Args[1:], os.Stdout)
	}
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tattva:", err)
		os.Exit(1)
	}
}

// run executes one command with dir as the working directory. It is main
// without the process exit, so tests can call it.
func run(ctx context.Context, dir string, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return errors.New("no command given")
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q (run `tattva help`)", cmd)
	}
}

// newFlags returns a flag set for one command that reports errors instead of
// printing them and exiting.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseArgs parses fs's flags wherever they appear among args and returns the
// positional arguments. The flag package alone stops at the first positional
// argument, which would ignore the --lang in `tattva new "Redis" --lang go`.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS (`ok  github.com/0x1DKFA/tattva`).

- [ ] **Step 7: Commit**

```bash
git add .gitignore go.mod main.go main_test.go
git commit -m "feat: add CLI skeleton"
```

---

### Task 2: Learning Spec types and validation

**Files:**
- Create: `spec.go`
- Test: `spec_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - Types: `Spec`, `ProjectInfo`, `Scope`, `Excluded`, `Architecture`, `Component`, `Program`, `Concept`, `Reference`, `Phase`, `Step` (with `Detail *Detail`), `Detail`, `Check`
  - Constants: `specFile = "curriculum/spec.json"`, `progressFile = "curriculum/progress.jsonl"`, `readmeFile = "curriculum/README.md"`, `stepsDir = "curriculum/steps"`
  - `Validate(s *Spec) (errs, warns []string)`
  - `detailErrors(kind string, d *Detail) (errs, warns []string)`
  - `checkErrors(kind string, c Check) []string`
  - `decodeSpec(data []byte) (*Spec, error)` (strict)
  - `loadSpec(path string) (*Spec, error)`
  - `encodeSpec(s *Spec) []byte`
  - `(s *Spec) stepIndex(id string) int` (-1 if missing)
  - `stepFile(i int, id string) string` (for example `curriculum/steps/01-create-run-script.md`)
  - Test helpers for later tasks: `testSpec() *Spec`, `testDetail() *Detail`, `containsAny(list []string, sub string) bool`

- [ ] **Step 1: Write the failing tests**

`spec_test.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testDetail is a valid detail for a step of a server program.
func testDetail() *Detail {
	return &Detail{
		Why:             "Every check starts your program through run.sh.",
		Context:         "Nothing exists yet.",
		Task:            "Write run.sh so it builds and starts a server listening on port 6379.",
		Constraints:     []string{"standard library only"},
		Hints:           []string{"Servers listen, then accept.", "Listen on TCP port 6379.", "listen on 6379; loop: accept a connection"},
		Checks:          []Check{{Name: "accepts a connection", Do: "tcp", Args: []string{}, Input: "PING\r\n", Match: "contains"}},
		ByHand:          "Run `./run.sh`, then `nc localhost 6379`.",
		ExpectedOutcome: "The server accepts connections.",
		CommonMistakes:  []string{"Forgetting to make run.sh executable."},
		Reflection:      "What happens if two programs listen on the same port?",
	}
}

// testSpec is a small valid curriculum: two phases, three steps, the first
// one expanded.
func testSpec() *Spec {
	return &Spec{
		SchemaVersion: 1,
		Project: ProjectInfo{ID: "00000000-0000-4000-8000-000000000000", Name: "Mini Redis", Target: "Redis",
			Language: "go", Summary: "A tiny Redis.", Assumes: []string{"comfortable with Go"}},
		Scope: Scope{In: []string{"TCP server", "PING"},
			Out: []Excluded{{Feature: "persistence", Why: "not needed to learn the protocol"}}},
		Architecture: Architecture{
			Real: "Redis is a single-threaded event-loop server.",
			Components: []Component{
				{ID: "server", Responsibility: "accept connections", TalksTo: []string{"parser"}},
				{ID: "parser", Responsibility: "parse RESP", TalksTo: []string{}},
			},
			Diagram: "flowchart LR\n  server --> parser",
		},
		Program: Program{Kind: "server", Port: 6379},
		Concepts: []Concept{
			{ID: "program-contract", Name: "Program contract", Summary: "How checks run your program."},
			{ID: "tcp-sockets", Name: "TCP sockets", Summary: "Listening for connections."},
			{ID: "message-framing", Name: "Message framing", Summary: "Finding message boundaries in a byte stream."},
		},
		References: []Reference{{Title: "RESP spec", URL: "https://redis.io/docs/latest/develop/reference/protocol-spec/"}},
		Phases: []Phase{
			{ID: "setup", Title: "Setup", Purpose: "Get a program running."},
			{ID: "protocol", Title: "Protocol", Purpose: "Speak RESP."},
		},
		Steps: []Step{
			{ID: "create-run-script", Phase: "setup", Title: "Create run.sh", Goal: "run.sh starts a server on port 6379.",
				Concepts: []string{"program-contract", "tcp-sockets"}, Prerequisites: []string{}, Detail: testDetail()},
			{ID: "respond-to-ping", Phase: "protocol", Title: "Respond to PING", Goal: "Reply +PONG to PING.",
				Concepts: []string{"message-framing"}, Prerequisites: []string{"create-run-script"}},
			{ID: "echo-command", Phase: "protocol", Title: "Echo", Goal: "Reply to ECHO with its argument.",
				Concepts: []string{"message-framing"}, Prerequisites: []string{"respond-to-ping"}},
		},
	}
}

func containsAny(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func addCheck(s *Spec, c Check) { s.Steps[0].Detail.Checks = append(s.Steps[0].Detail.Checks, c) }

func TestValidateAcceptsTestSpec(t *testing.T) {
	if errs, warns := Validate(testSpec()); len(errs) > 0 || len(warns) > 0 {
		t.Fatalf("errs=%q warns=%q", errs, warns)
	}
}

func TestValidateRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s *Spec)
		want   string
	}{
		{"schema version", func(s *Spec) { s.SchemaVersion = 2 }, "schema_version is 2"},
		{"kebab-case ids", func(s *Spec) { s.Architecture.Components[1].ID = "Parser" }, `component id "Parser" is not kebab-case`},
		{"duplicate ids", func(s *Spec) { s.Concepts = append(s.Concepts, s.Concepts[0]) }, `duplicate concept id "program-contract"`},
		{"no phases", func(s *Spec) { s.Phases = nil }, "there are no phases"},
		{"too many steps", func(s *Spec) {
			for i := len(s.Steps); i < 100; i++ {
				s.Steps = append(s.Steps, Step{ID: fmt.Sprintf("extra-%d", i), Phase: "protocol", Title: "x", Goal: "x",
					Concepts: []string{"message-framing"}, Prerequisites: []string{}})
			}
		}, "there are 100 steps, the maximum is 99"},
		{"undeclared phase", func(s *Spec) { s.Steps[2].Phase = "nope" }, `step "echo-command": phase "nope" is not declared`},
		{"phases out of order", func(s *Spec) { s.Phases[0], s.Phases[1] = s.Phases[1], s.Phases[0] }, "grouped by phase in declared order"},
		{"empty phase", func(s *Spec) { s.Phases = append(s.Phases, Phase{ID: "extra", Title: "Extra", Purpose: "x"}) }, "no empty phase"},
		{"forward prerequisite", func(s *Spec) { s.Steps[1].Prerequisites = []string{"echo-command"} }, `step "respond-to-ping": prerequisite "echo-command" is not an earlier step`},
		{"self prerequisite", func(s *Spec) { s.Steps[1].Prerequisites = []string{"respond-to-ping"} }, `prerequisite "respond-to-ping" is not an earlier step`},
		{"no concepts", func(s *Spec) { s.Steps[2].Concepts = nil }, `step "echo-command" has no concepts`},
		{"undeclared concept", func(s *Spec) { s.Steps[2].Concepts = []string{"message-framing", "ghost"} }, `concept "ghost" is not declared`},
		{"unused concept", func(s *Spec) { s.Concepts = append(s.Concepts, Concept{ID: "unused", Name: "Unused", Summary: "x"}) }, `concept "unused" is never used`},
		{"too many new concepts", func(s *Spec) {
			for _, id := range []string{"resp-arrays", "bulk-strings", "pipelining"} {
				s.Concepts = append(s.Concepts, Concept{ID: id, Name: id, Summary: "x"})
			}
			s.Steps[1].Concepts = []string{"resp-arrays", "bulk-strings", "pipelining"}
		}, `step "respond-to-ping" introduces 3 new concepts`},
		{"undeclared component", func(s *Spec) { s.Architecture.Components[1].TalksTo = []string{"ghost"} }, `talks to undeclared component "ghost"`},
		{"server port", func(s *Spec) { s.Program.Port = 80 }, "server port 80 must be between 1024 and 65535"},
		{"cli port", func(s *Spec) { s.Program = Program{Kind: "cli", Port: 7} }, "cli programs have port 0"},
		{"program kind", func(s *Spec) { s.Program.Kind = "daemon" }, `program kind "daemon"`},
		{"empty detail field", func(s *Spec) { s.Steps[0].Detail.Why = " " }, "why is empty"},
		{"hint count", func(s *Spec) { s.Steps[0].Detail.Hints = s.Steps[0].Detail.Hints[:2] }, "has 2 hints, want exactly 3"},
		{"no common mistakes", func(s *Spec) { s.Steps[0].Detail.CommonMistakes = nil }, "has no common mistakes"},
		{"exec on a server", func(s *Spec) { s.Steps[0].Detail.Checks[0] = Check{Name: "run", Do: "exec", Match: "exact"} }, "exec checks are only for cli programs"},
		{"tcp on a cli", func(s *Spec) { s.Program = Program{Kind: "cli"} }, "tcp checks are only for server programs"},
		{"tcp without input", func(s *Spec) { s.Steps[0].Detail.Checks[0].Input = "" }, "tcp checks need input"},
		{"unused check field", func(s *Spec) { s.Steps[0].Detail.Checks[0].ExitCode = 1 }, "tcp checks only use input, expect and match"},
		{"path escapes", func(s *Spec) { addCheck(s, Check{Name: "escape", Do: "write", Path: "../x", Input: "hi", Match: "exact"}) }, `path "../x" must be relative`},
		{"absolute path", func(s *Spec) { addCheck(s, Check{Name: "abs", Do: "file", Path: "/etc/passwd", Match: "exact"}) }, `path "/etc/passwd" must be relative`},
		{"bad regex", func(s *Spec) { c := &s.Steps[0].Detail.Checks[0]; c.Match, c.Expect = "regex", "(" }, "regex doesn't compile"},
		{"unknown match", func(s *Spec) { s.Steps[0].Detail.Checks[0].Match = "fuzzy" }, `match "fuzzy"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testSpec()
			tc.mutate(s)
			if errs, _ := Validate(s); !containsAny(errs, tc.want) {
				t.Fatalf("want an error containing %q, got %q", tc.want, errs)
			}
		})
	}
}

func TestValidateWarnsWhenAStepHasNoChecks(t *testing.T) {
	s := testSpec()
	s.Steps[0].Detail.Checks = nil
	errs, warns := Validate(s)
	if len(errs) > 0 || !containsAny(warns, `step "create-run-script": has no checks`) {
		t.Fatalf("errs=%q warns=%q", errs, warns)
	}
}

func TestLoadSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(path, encodeSpec(testSpec()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, testSpec()) {
		t.Fatalf("round trip changed the spec:\n%s", encodeSpec(got))
	}
	for _, bad := range []string{`{"schema_version":1,"bogus":true}`, `{"schema_version":`} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSpec(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("loadSpec(%s): err = %v, want an error naming the file", bad, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: Detail`, `undefined: Spec` and similar.

- [ ] **Step 3: Write `spec.go`**

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Paths inside a project, relative to its root.
const (
	specFile     = "curriculum/spec.json"
	progressFile = "curriculum/progress.jsonl"
	readmeFile   = "curriculum/README.md"
	stepsDir     = "curriculum/steps"
)

// Spec is the Learning Spec in curriculum/spec.json, the contract between
// Claude and tattva. Fields tagged schema:"-" are set by tattva and never
// requested from Claude.
type Spec struct {
	SchemaVersion int          `json:"schema_version" schema:"-"`
	Project       ProjectInfo  `json:"project"`
	Scope         Scope        `json:"scope"`
	Architecture  Architecture `json:"architecture"`
	Program       Program      `json:"program"`
	Concepts      []Concept    `json:"concepts"`
	References    []Reference  `json:"references"`
	Phases        []Phase      `json:"phases"`
	Steps         []Step       `json:"steps"`
}

type ProjectInfo struct {
	ID       string   `json:"id" schema:"-"`
	Name     string   `json:"name"`
	Target   string   `json:"target" schema:"-"`
	Language string   `json:"language" schema:"-"`
	Summary  string   `json:"summary"`
	Assumes  []string `json:"assumes"`
}

type Scope struct {
	In  []string   `json:"in"`
	Out []Excluded `json:"out"`
}

type Excluded struct {
	Feature string `json:"feature"`
	Why     string `json:"why"`
}

type Architecture struct {
	Real       string      `json:"real"`
	Components []Component `json:"components"`
	Diagram    string      `json:"diagram"`
}

type Component struct {
	ID             string   `json:"id"`
	Responsibility string   `json:"responsibility"`
	TalksTo        []string `json:"talks_to"`
}

type Program struct {
	Kind string `json:"kind" enum:"server,cli"`
	Port int    `json:"port"`
}

type Concept struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type Reference struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Phase struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Purpose string `json:"purpose"`
}

type Step struct {
	ID            string   `json:"id"`
	Phase         string   `json:"phase"`
	Title         string   `json:"title"`
	Goal          string   `json:"goal"`
	Concepts      []string `json:"concepts"`
	Prerequisites []string `json:"prerequisites"`
	Detail        *Detail  `json:"detail" schema:"-"` // nil until expanded
}

type Detail struct {
	Why             string   `json:"why"`
	Context         string   `json:"context"`
	Task            string   `json:"task"`
	Constraints     []string `json:"constraints"`
	Hints           []string `json:"hints"`
	Checks          []Check  `json:"checks"`
	ByHand          string   `json:"by_hand"`
	ExpectedOutcome string   `json:"expected_outcome"`
	CommonMistakes  []string `json:"common_mistakes"`
	Reflection      string   `json:"reflection"`
}

// Check is one black-box check. Every field is always present; fields a check
// doesn't use are empty.
type Check struct {
	Name     string   `json:"name"`
	Do       string   `json:"do" enum:"exec,tcp,write,file"`
	Args     []string `json:"args"`
	Input    string   `json:"input"`
	Path     string   `json:"path"`
	Expect   string   `json:"expect"`
	Match    string   `json:"match" enum:"exact,contains,regex"`
	ExitCode int      `json:"exit_code"`
}

// decodeSpec strictly decodes a spec: unknown fields are errors, so a typo in
// a hand edit isn't silently dropped.
func decodeSpec(data []byte) (*Spec, error) {
	var s Spec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// loadSpec reads and decodes the spec file at path.
func loadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := decodeSpec(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// encodeSpec is the exact bytes tattva writes to spec.json: indented, with
// <, > and & left readable.
func encodeSpec(s *Spec) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		panic(err) // Spec has only marshalable fields
	}
	return buf.Bytes()
}

// stepIndex returns the position of the step with id, or -1.
func (s *Spec) stepIndex(id string) int {
	for i, st := range s.Steps {
		if st.ID == id {
			return i
		}
	}
	return -1
}

// stepFile is the path of step i's markdown file, relative to the project root.
func stepFile(i int, id string) string {
	return fmt.Sprintf("%s/%02d-%s.md", stepsDir, i+1, id)
}

var kebab = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxNewConcepts is the "one conceptual leap" rule: how many concepts a step
// may introduce that no earlier step used.
const maxNewConcepts = 2 // ponytail: heuristic; tune after the first real curricula

// Validate checks s against the validity rules in the design spec. errs are
// rule violations; warns are printed but don't stop anything.
func Validate(s *Spec) (errs, warns []string) {
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	// Rule 9.
	if s.SchemaVersion != 1 {
		add("schema_version is %d, want 1", s.SchemaVersion)
	}

	// Rule 1: ids are kebab-case and unique within their kind.
	idSet := func(kind string, ids []string) map[string]bool {
		set := map[string]bool{}
		for _, id := range ids {
			if !kebab.MatchString(id) {
				add("%s id %q is not kebab-case", kind, id)
			}
			if set[id] {
				add("duplicate %s id %q", kind, id)
			}
			set[id] = true
		}
		return set
	}
	var phaseIDs, stepIDs, conceptIDs, componentIDs []string
	for _, p := range s.Phases {
		phaseIDs = append(phaseIDs, p.ID)
	}
	for _, st := range s.Steps {
		stepIDs = append(stepIDs, st.ID)
	}
	for _, c := range s.Concepts {
		conceptIDs = append(conceptIDs, c.ID)
	}
	for _, c := range s.Architecture.Components {
		componentIDs = append(componentIDs, c.ID)
	}
	phases := idSet("phase", phaseIDs)
	idSet("step", stepIDs)
	concepts := idSet("concept", conceptIDs)
	components := idSet("component", componentIDs)

	// Rule 2.
	if len(s.Phases) == 0 {
		add("there are no phases")
	}
	if len(s.Steps) > 99 {
		add("there are %d steps, the maximum is 99", len(s.Steps))
	}

	// Rule 3: steps grouped by phase, phases in declared order, none empty.
	var order []string
	for _, st := range s.Steps {
		if !phases[st.Phase] {
			add("step %q: phase %q is not declared", st.ID, st.Phase)
		}
		if len(order) == 0 || order[len(order)-1] != st.Phase {
			order = append(order, st.Phase)
		}
	}
	if strings.Join(order, ",") != strings.Join(phaseIDs, ",") {
		add("steps must be grouped by phase in declared order, with no empty phase: steps give [%s], phases are [%s]",
			strings.Join(order, " "), strings.Join(phaseIDs, " "))
	}

	// Rule 4: prerequisites point to earlier steps.
	earlier := map[string]bool{}
	for _, st := range s.Steps {
		for _, pre := range st.Prerequisites {
			if !earlier[pre] {
				add("step %q: prerequisite %q is not an earlier step", st.ID, pre)
			}
		}
		earlier[st.ID] = true
	}

	// Rules 5 and 6: concepts are declared and used; few new ones per step.
	seen := map[string]bool{}
	for _, st := range s.Steps {
		if len(st.Concepts) == 0 {
			add("step %q has no concepts", st.ID)
		}
		var fresh []string
		for _, c := range st.Concepts {
			if !concepts[c] {
				add("step %q: concept %q is not declared", st.ID, c)
			}
			if !seen[c] {
				fresh = append(fresh, c)
				seen[c] = true
			}
		}
		if len(fresh) > maxNewConcepts {
			add("step %q introduces %d new concepts (%s); the maximum is %d, so split the step",
				st.ID, len(fresh), strings.Join(fresh, ", "), maxNewConcepts)
		}
	}
	for _, c := range conceptIDs {
		if !seen[c] {
			add("concept %q is never used by a step", c)
		}
	}

	// Rule 7.
	for _, c := range s.Architecture.Components {
		for _, other := range c.TalksTo {
			if !components[other] {
				add("component %q talks to undeclared component %q", c.ID, other)
			}
		}
	}

	// Rule 8.
	switch {
	case s.Program.Kind == "server" && (s.Program.Port < 1024 || s.Program.Port > 65535):
		add("server port %d must be between 1024 and 65535", s.Program.Port)
	case s.Program.Kind == "cli" && s.Program.Port != 0:
		add("cli programs have port 0, got %d", s.Program.Port)
	case s.Program.Kind != "server" && s.Program.Kind != "cli":
		add("program kind %q must be server or cli", s.Program.Kind)
	}

	// Rules 10 and 11: expanded steps.
	for _, st := range s.Steps {
		if st.Detail == nil {
			continue
		}
		e, w := detailErrors(s.Program.Kind, st.Detail)
		for _, msg := range e {
			add("step %q: %s", st.ID, msg)
		}
		for _, msg := range w {
			warns = append(warns, fmt.Sprintf("step %q: %s", st.ID, msg))
		}
	}
	return errs, warns
}

// detailErrors checks one expanded step's detail (rules 10 and 11).
func detailErrors(kind string, d *Detail) (errs, warns []string) {
	fields := []struct{ name, value string }{
		{"why", d.Why}, {"context", d.Context}, {"task", d.Task},
		{"by_hand", d.ByHand}, {"expected_outcome", d.ExpectedOutcome}, {"reflection", d.Reflection},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			errs = append(errs, f.name+" is empty")
		}
	}
	if len(d.Hints) != 3 {
		errs = append(errs, fmt.Sprintf("has %d hints, want exactly 3", len(d.Hints)))
	}
	for i, h := range d.Hints {
		if strings.TrimSpace(h) == "" {
			errs = append(errs, fmt.Sprintf("hint %d is empty", i+1))
		}
	}
	if len(d.CommonMistakes) == 0 {
		errs = append(errs, "has no common mistakes")
	}
	if len(d.Checks) == 0 {
		warns = append(warns, "has no checks")
	}
	for i, c := range d.Checks {
		for _, msg := range checkErrors(kind, c) {
			errs = append(errs, fmt.Sprintf("check %d (%s): %s", i+1, c.Name, msg))
		}
	}
	return errs, warns
}

// checkErrors checks one check against the program kind (rule 11).
func checkErrors(kind string, c Check) []string {
	var errs []string
	bad := func(msg string) { errs = append(errs, msg) }
	if strings.TrimSpace(c.Name) == "" {
		bad("name is empty")
	}
	switch c.Do {
	case "exec":
		if kind != "cli" {
			bad("exec checks are only for cli programs")
		}
		if c.Path != "" {
			bad("exec checks don't use path")
		}
	case "tcp":
		if kind != "server" {
			bad("tcp checks are only for server programs")
		}
		if c.Input == "" {
			bad("tcp checks need input")
		}
		if len(c.Args) > 0 || c.Path != "" || c.ExitCode != 0 {
			bad("tcp checks only use input, expect and match")
		}
	case "write":
		if len(c.Args) > 0 || c.Expect != "" || c.ExitCode != 0 {
			bad("write checks only use path and input")
		}
	case "file":
		if len(c.Args) > 0 || c.Input != "" || c.ExitCode != 0 {
			bad("file checks only use path, expect and match")
		}
	default:
		bad(fmt.Sprintf("do %q must be exec, tcp, write or file", c.Do))
	}
	if (c.Do == "write" || c.Do == "file") && !safeRelPath(c.Path) {
		bad(fmt.Sprintf("path %q must be relative with no .. segment", c.Path))
	}
	switch c.Match {
	case "exact", "contains":
	case "regex":
		if _, err := regexp.Compile(c.Expect); err != nil {
			bad("regex doesn't compile: " + err.Error())
		}
	default:
		bad(fmt.Sprintf("match %q must be exact, contains or regex", c.Match))
	}
	return errs
}

// safeRelPath reports whether p stays inside the directory it's relative to.
func safeRelPath(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add spec.go spec_test.go
git commit -m "feat: add Learning Spec types and validation"
```

---

### Task 3: JSON Schemas from the spec types

**Files:**
- Modify: `spec.go` (add the `"reflect"` import and the code below)
- Test: `spec_test.go` (append)

**Interfaces:**
- Consumes: the `Spec`, `Step` and `Detail` types from Task 2
- Produces:
  - `PhaseDetails{Steps []StepDetail}` and `StepDetail{ID string; Detail Detail}`: what one expand call returns
  - `outlineSchema []byte` and `phaseSchema []byte`
  - `schemaFor(t reflect.Type) map[string]any`

- [ ] **Step 1: Write the failing tests**

Append to `spec_test.go`, and add `"encoding/json"` to its imports:

```go
func TestOutlineSchema(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(outlineSchema, &s); err != nil {
		t.Fatal(err)
	}
	if _, ok := dig(t, s, "properties").(map[string]any)["schema_version"]; ok {
		t.Error("schema_version must not be requested from Claude")
	}
	project := dig(t, s, "properties", "project", "properties").(map[string]any)
	for _, f := range []string{"id", "target", "language"} {
		if _, ok := project[f]; ok {
			t.Errorf("project.%s must not be requested from Claude", f)
		}
	}
	if _, ok := dig(t, s, "properties", "steps", "items", "properties").(map[string]any)["detail"]; ok {
		t.Error("the outline schema must not include step details")
	}
	if got := fmt.Sprint(dig(t, s, "properties", "program", "properties", "kind", "enum")); got != "[server cli]" {
		t.Errorf("program.kind enum = %s", got)
	}
	if s["additionalProperties"] != false || len(s["required"].([]any)) != len(s["properties"].(map[string]any)) {
		t.Error("objects must require every property and forbid extra ones")
	}
}

func TestPhaseSchema(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(phaseSchema, &s); err != nil {
		t.Fatal(err)
	}
	check := dig(t, s, "properties", "steps", "items", "properties", "detail", "properties", "checks", "items", "properties")
	if got := fmt.Sprint(dig(t, check, "do", "enum")); got != "[exec tcp write file]" {
		t.Errorf("check.do enum = %s", got)
	}
	if got := fmt.Sprint(dig(t, check, "match", "enum")); got != "[exact contains regex]" {
		t.Errorf("check.match enum = %s", got)
	}
}

// dig walks nested JSON objects by key.
func dig(t *testing.T, v any, keys ...string) any {
	t.Helper()
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("expected an object at %q", k)
		}
		v = m[k]
	}
	return v
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: outlineSchema` and `undefined: phaseSchema`.

- [ ] **Step 3: Add the schema code to `spec.go`**

Add `"reflect"` to the import block of `spec.go`, then append:

```go
// PhaseDetails is what one expand call returns.
type PhaseDetails struct {
	Steps []StepDetail `json:"steps"`
}

// StepDetail is one step's detail as returned by an expand call.
type StepDetail struct {
	ID     string `json:"id"`
	Detail Detail `json:"detail"`
}

// The schemas sent to Claude, generated from the types so they can't drift.
var (
	outlineSchema = mustSchema(Spec{})
	phaseSchema   = mustSchema(PhaseDetails{})
)

func mustSchema(v any) []byte {
	b, err := json.Marshal(schemaFor(reflect.TypeOf(v)))
	if err != nil {
		panic(err)
	}
	return b
}

// schemaFor builds a strict JSON Schema for t: every property is required,
// extra properties are forbidden, `enum` tags become enums, and fields tagged
// schema:"-" are left out.
func schemaFor(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		return schemaFor(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": schemaFor(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		for i := range t.NumField() {
			f := t.Field(i)
			if f.Tag.Get("schema") == "-" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			s := schemaFor(f.Type)
			if enum := f.Tag.Get("enum"); enum != "" {
				s["enum"] = strings.Split(enum, ",")
			}
			props[name] = s
			required = append(required, name)
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	panic("schemaFor: unsupported kind " + t.Kind().String())
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add spec.go spec_test.go
git commit -m "feat: generate JSON schemas from spec types"
```

---

### Task 4: Claude Code adapter

**Files:**
- Create: `claude.go`
- Test: `claude_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `call{System, Prompt string; Schema []byte; Web bool; Model string}`
  - `runClaude(ctx context.Context, c call) (json.RawMessage, float64, error)`
  - `callTimeout` (a var)
  - Test helpers: `readFile(t *testing.T, path string) string`, `installFakeClaude(t *testing.T, mode string) string`

- [ ] **Step 1: Write the failing tests**

`claude_test.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaudeScript stands in for the claude CLI. It records its arguments
// (NUL-separated), stdin and working directory, then replies per FAKE_MODE.
const fakeClaudeScript = `#!/bin/sh
printf '%s\0' "$@" > "$FAKE_DIR/args"
cat > "$FAKE_DIR/stdin"
pwd > "$FAKE_DIR/cwd"
case "$FAKE_MODE" in
ok) printf '%s' '{"type":"result","is_error":false,"result":"{}","structured_output":{"answer":42},"total_cost_usd":0.25}' ;;
error) printf '%s' '{"type":"result","is_error":true,"result":"There is an issue with the selected model","total_cost_usd":0}'; exit 1 ;;
garbage) echo "not json"; echo "segfault in module" >&2; exit 2 ;;
empty) printf '%s' '{"type":"result","is_error":false,"result":"I could not comply","total_cost_usd":0.1}' ;;
hang) exec sleep 10 ;;
esac
`

// installFakeClaude puts the fake first on PATH and returns the directory it
// records into.
func installFakeClaude(t *testing.T, mode string) string {
	t.Helper()
	bin, rec := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaudeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", rec)
	t.Setenv("FAKE_MODE", mode)
	return rec
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunClaudeSuccess(t *testing.T) {
	rec := installFakeClaude(t, "ok")
	prompt := "Target: \"Redis\" with 'quotes'\nand a second line ✓"
	out, cost, err := runClaude(context.Background(), call{
		System: "be precise", Prompt: prompt, Schema: []byte(`{"type":"object"}`), Web: true, Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"answer":42}` || cost != 0.25 {
		t.Fatalf("out=%s cost=%v", out, cost)
	}
	args := readFile(t, filepath.Join(rec, "args"))
	for _, want := range []string{"-p", "--output-format\x00json", "--json-schema\x00{\"type\":\"object\"}",
		"--system-prompt\x00be precise", "--tools\x00WebSearch,WebFetch", "--allowedTools\x00WebSearch,WebFetch",
		"--safe-mode", "--no-session-persistence", "--model\x00sonnet"} {
		if !strings.Contains(args, want+"\x00") {
			t.Errorf("args missing %q:\n%q", want, args)
		}
	}
	if got := readFile(t, filepath.Join(rec, "stdin")); got != prompt {
		t.Errorf("stdin = %q, want %q", got, prompt)
	}
	wd, _ := os.Getwd()
	if cwd := strings.TrimSpace(readFile(t, filepath.Join(rec, "cwd"))); cwd == wd {
		t.Errorf("claude ran in the caller's directory %s", cwd)
	}
}

func TestRunClaudeWithoutWebTools(t *testing.T) {
	rec := installFakeClaude(t, "ok")
	if _, _, err := runClaude(context.Background(), call{System: "s", Prompt: "p", Schema: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	args := readFile(t, filepath.Join(rec, "args"))
	if !strings.Contains(args, "--tools\x00\x00") {
		t.Errorf("tools should be empty: %q", args)
	}
	if strings.Contains(args, "--allowedTools") || strings.Contains(args, "--model") {
		t.Errorf("unexpected flags: %q", args)
	}
}

func TestRunClaudeErrors(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{
		{"error", "issue with the selected model"},
		{"garbage", "segfault in module"},
		{"empty", "no structured output"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			installFakeClaude(t, tc.mode)
			_, _, err := runClaude(context.Background(), call{Schema: []byte(`{}`)})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRunClaudeTimeout(t *testing.T) {
	installFakeClaude(t, "hang")
	old := callTimeout
	callTimeout = 200 * time.Millisecond
	t.Cleanup(func() { callTimeout = old })
	start := time.Now()
	_, _, err := runClaude(context.Background(), call{Schema: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("timeout took %s", took)
	}
}

func TestRunClaudeMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := runClaude(context.Background(), call{Schema: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "isn't on your PATH") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: runClaude`, `undefined: call` and `undefined: callTimeout`.

- [ ] **Step 3: Write `claude.go`**

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// call is one headless Claude Code invocation.
type call struct {
	System string // system prompt, replacing Claude Code's default
	Prompt string // sent on stdin
	Schema []byte // JSON Schema the output must match
	Web    bool   // allow WebSearch and WebFetch
	Model  string // empty means Claude Code's default
}

// callTimeout bounds one call. It's a var so tests can shorten it.
var callTimeout = 20 * time.Minute

// runClaude runs `claude -p` for c in an empty temp dir and returns the
// structured output and the call's cost in USD.
func runClaude(ctx context.Context, c call) (json.RawMessage, float64, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "tattva-call-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)

	tools := ""
	if c.Web {
		tools = "WebSearch,WebFetch"
	}
	args := []string{"-p",
		"--output-format", "json",
		"--json-schema", string(c.Schema),
		"--system-prompt", c.System,
		"--tools", tools,
		"--safe-mode", // keeps the user's CLAUDE.md, hooks, plugins and MCP servers out
		"--no-session-persistence",
	}
	if c.Web {
		args = append(args, "--allowedTools", tools)
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(c.Prompt)
	cmd.WaitDelay = 5 * time.Second // don't hang on pipes held open by a killed child's children
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	runErr := cmd.Run()
	if errors.Is(runErr, exec.ErrNotFound) {
		return nil, 0, errors.New("the claude CLI isn't on your PATH; install Claude Code and log in first")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, 0, fmt.Errorf("claude timed out after %s", callTimeout)
	}
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	var res struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		TotalCostUSD     float64         `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return nil, 0, fmt.Errorf("claude failed (%v): %s", runErr, lastLine(stderr.String()))
	}
	if res.IsError {
		return nil, res.TotalCostUSD, fmt.Errorf("claude: %s", res.Result)
	}
	if runErr != nil {
		return nil, res.TotalCostUSD, fmt.Errorf("claude failed (%v): %s", runErr, lastLine(stderr.String()))
	}
	if len(res.StructuredOutput) == 0 || string(res.StructuredOutput) == "null" {
		return nil, res.TotalCostUSD, fmt.Errorf("claude returned no structured output: %s", lastLine(res.Result))
	}
	return res.StructuredOutput, res.TotalCostUSD, nil
}

// lastLine returns the last line of s, for short error messages.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS. `TestRunClaudeTimeout` finishes in well under a second.

- [ ] **Step 5: Commit**

```bash
git add claude.go claude_test.go
git commit -m "feat: add Claude Code adapter"
```

---

### Task 5: Rendering README and step files

**Files:**
- Create: `render.go`
- Create: `templates/readme.md.tmpl`
- Create: `templates/step.md.tmpl`
- Test: `render_test.go`
- Create (generated in Step 5): `testdata/golden/README.md`, `testdata/golden/step-01.md`

**Interfaces:**
- Consumes: `Spec`, `Check`, `(*Spec).stepIndex` and `testSpec()` from Task 2
- Produces:
  - `renderReadme(s *Spec) []byte`
  - `renderStep(s *Spec, i int) []byte` (step `i` must be expanded)
  - `num(i int) string` (`0` → `"01"`)
  - `describeCheck(c Check) string`
  - `contract(p Program) string`

- [ ] **Step 1: Write the failing tests**

`render_test.go`:

```go
package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestRenderStep(t *testing.T) {
	got := string(renderStep(testSpec(), 0))
	for _, want := range []string{
		"<!-- Generated by tattva from spec.json.",
		"# 01 · Create run.sh",
		"Phase 1: Setup · Concepts: program-contract, tcp-sockets",
		"## Your task",
		"- standard library only",
		"<details><summary>Machine checks (run by the verifier)</summary>",
		"**accepts a connection**: send `\"PING\\r\\n\"`",
		"<details><summary>Hint 1 (nudge)</summary>",
		"<details><summary>Hint 3 (pseudocode)</summary>",
		"</details>\n\n## Common mistakes",
		"## Unlocks\n\n- 02 · Respond to PING",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("step file missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "<no value>") {
		t.Errorf("template printed <no value>:\n%s", got)
	}
}

func TestRenderReadme(t *testing.T) {
	got := string(renderReadme(testSpec()))
	for _, want := range []string{
		"# Mini Redis",
		"**Target:** Redis · **Language:** go",
		"- **persistence**: not needed to learn the protocol",
		"```mermaid\nflowchart LR\n  server --> parser\n```",
		"wait for port 6379",
		"- **Message framing** (`message-framing`): ",
		"- [RESP spec](https://redis.io/docs/latest/develop/reference/protocol-spec/)",
		"### Phase 2: Protocol",
		"- **02 · Respond to PING**: Reply +PONG to PING. *(after 01)* *(not expanded yet)*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("README missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "<no value>") {
		t.Errorf("template printed <no value>:\n%s", got)
	}
}

func TestDescribeCheck(t *testing.T) {
	for _, tc := range []struct {
		c    Check
		want string
	}{
		{Check{Name: "init", Do: "exec", Args: []string{"init"}, Match: "exact"}, "**init**: run `./run.sh init`, expect exit code 0"},
		{Check{Name: "pong", Do: "tcp", Input: "PING\r\n", Expect: "+PONG\r\n", Match: "exact"}, "**pong**: send `\"PING\\r\\n\"`, expect reply `\"+PONG\\r\\n\"` (exact)"},
		{Check{Name: "fixture", Do: "write", Path: "a.txt", Input: "hi", Match: "exact"}, "**fixture**: write `\"hi\"` to `a.txt`"},
		{Check{Name: "head", Do: "file", Path: ".git/HEAD", Expect: "ref: refs/heads/main\n", Match: "exact"}, "**head**: `.git/HEAD` exists with content `\"ref: refs/heads/main\\n\"` (exact)"},
	} {
		if got := describeCheck(tc.c); got != tc.want {
			t.Errorf("describeCheck(%s) = %q, want %q", tc.c.Name, got, tc.want)
		}
	}
}

func TestRenderGolden(t *testing.T) {
	s := testSpec()
	for name, got := range map[string][]byte{"README.md": renderReadme(s), "step-01.md": renderStep(s, 0)} {
		path := filepath.Join("testdata", "golden", name)
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run `go test ./... -run TestRenderGolden -update` to create it)", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its golden file; rerun with -update if the change is intended\n%s", name, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: renderStep`, `undefined: renderReadme` and `undefined: describeCheck`.

- [ ] **Step 3: Write the templates**

`templates/readme.md.tmpl`. The final line must be `{{end -}}`, which trims the file's trailing newline:

````
<!-- Generated by tattva from spec.json. Edits are detected and won't be overwritten; delete this file to regenerate it. -->
# {{.Project.Name}}

{{.Project.Summary}}

**Target:** {{.Project.Target}} · **Language:** {{.Project.Language}}

## What this curriculum assumes
{{range .Project.Assumes}}
- {{.}}
{{- end}}

## Scope

In scope:
{{range .Scope.In}}
- {{.}}
{{- end}}

Left out:
{{range .Scope.Out}}
- **{{.Feature}}**: {{.Why}}
{{- end}}

## How the real system is built

{{.Architecture.Real}}

## What you'll build
{{range .Architecture.Components}}
- **{{.ID}}**: {{.Responsibility}}{{if .TalksTo}} (talks to {{join .TalksTo ", "}}){{end}}
{{- end}}

```mermaid
{{.Architecture.Diagram}}
```

## Program contract

{{.Contract}}

## Concepts
{{range .Concepts}}
- **{{.Name}}** (`{{.ID}}`): {{.Summary}}
{{- end}}

## References
{{range .References}}
- [{{.Title}}]({{.URL}})
{{- end}}

## Steps

Run `tattva next` to start the next step and `tattva status` to see your progress.
{{range .PhaseList}}
### Phase {{.Num}}: {{.Title}}

{{.Purpose}}
{{range .Steps}}
- **{{.Num}} · {{.Title}}**: {{.Goal}}{{if .After}} *(after {{.After}})*{{end}}{{if not .Expanded}} *(not expanded yet)*{{end}}
{{- end}}
{{end -}}
````

`templates/step.md.tmpl`:

```
<!-- Generated by tattva from spec.json. Edits are detected and won't be overwritten; delete this file to regenerate it. -->
# {{.Num}} · {{.Step.Title}}

Phase {{.PhaseNum}}: {{.PhaseTitle}} · Concepts: {{join .Step.Concepts ", "}}{{if .After}} · After: {{.After}}{{end}}

## Goal

{{.Step.Goal}}

## Why

{{.Step.Detail.Why}}

## Context

{{.Step.Detail.Context}}

## Your task

{{.Step.Detail.Task}}
{{- if .Step.Detail.Constraints}}

## Constraints
{{range .Step.Detail.Constraints}}
- {{.}}
{{- end}}
{{- end}}

## Check it

{{.Step.Detail.ByHand}}

**Expected outcome:** {{.Step.Detail.ExpectedOutcome}}
{{- if .Checks}}

<details><summary>Machine checks (run by the verifier)</summary>

{{range .Checks}}- {{.}}
{{end}}
</details>
{{- end}}

## Hints
{{range .Hints}}
<details><summary>{{.Label}}</summary>

{{.Text}}

</details>
{{end}}
## Common mistakes
{{range .Step.Detail.CommonMistakes}}
- {{.}}
{{- end}}

## Reflect

{{.Step.Detail.Reflection}}
{{- if .Unlocks}}

## Unlocks
{{range .Unlocks}}
- {{.}}
{{- end}}
{{- end}}
```

- [ ] **Step 4: Write `render.go`**

```go
package main

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("").
	Funcs(template.FuncMap{"join": strings.Join}).
	ParseFS(templateFS, "templates/*.tmpl"))

type readmeView struct {
	*Spec
	Contract  string
	PhaseList []phaseView
}

type phaseView struct {
	Num            int
	Title, Purpose string
	Steps          []stepLine
}

type stepLine struct {
	Num, Title, Goal, After string
	Expanded                bool
}

type stepView struct {
	Num        string
	Step       Step
	PhaseNum   int
	PhaseTitle string
	After      string
	Checks     []string
	Hints      []hintView
	Unlocks    []string
}

type hintView struct{ Label, Text string }

// renderReadme returns curriculum/README.md for s.
func renderReadme(s *Spec) []byte {
	v := readmeView{Spec: s, Contract: contract(s.Program)}
	for pi, ph := range s.Phases {
		pv := phaseView{Num: pi + 1, Title: ph.Title, Purpose: ph.Purpose}
		for i, st := range s.Steps {
			if st.Phase == ph.ID {
				pv.Steps = append(pv.Steps, stepLine{Num: num(i), Title: st.Title, Goal: st.Goal,
					After: s.prereqNums(st), Expanded: st.Detail != nil})
			}
		}
		v.PhaseList = append(v.PhaseList, pv)
	}
	return execute("readme.md.tmpl", v)
}

// renderStep returns the markdown file for step i, which must be expanded.
func renderStep(s *Spec, i int) []byte {
	st := s.Steps[i]
	v := stepView{Num: num(i), Step: st, After: s.prereqNums(st)}
	for pi, ph := range s.Phases {
		if ph.ID == st.Phase {
			v.PhaseNum, v.PhaseTitle = pi+1, ph.Title
		}
	}
	for _, c := range st.Detail.Checks {
		v.Checks = append(v.Checks, describeCheck(c))
	}
	for hi, h := range st.Detail.Hints {
		v.Hints = append(v.Hints, hintView{Label: hintLabel(hi), Text: h})
	}
	for j, other := range s.Steps {
		for _, pre := range other.Prerequisites {
			if pre == st.ID {
				v.Unlocks = append(v.Unlocks, num(j)+" · "+other.Title)
			}
		}
	}
	return execute("step.md.tmpl", v)
}

func execute(name string, data any) []byte {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		panic(err) // the templates are fixed, so an error is a bug the tests catch
	}
	return buf.Bytes()
}

// num is a step's two-digit number from its position: 0 → "01".
func num(i int) string { return fmt.Sprintf("%02d", i+1) }

// prereqNums lists a step's prerequisites by number, such as "01, 03".
func (s *Spec) prereqNums(st Step) string {
	var nums []string
	for _, pre := range st.Prerequisites {
		if i := s.stepIndex(pre); i >= 0 {
			nums = append(nums, num(i))
		}
	}
	return strings.Join(nums, ", ")
}

func hintLabel(i int) string {
	labels := []string{"nudge", "specific", "pseudocode"}
	if i < len(labels) {
		return fmt.Sprintf("Hint %d (%s)", i+1, labels[i])
	}
	return fmt.Sprintf("Hint %d", i+1)
}

// contract explains how the checks run the learner's program.
func contract(p Program) string {
	if p.Kind == "server" {
		return fmt.Sprintf("Create an executable `run.sh` at the project root that builds your server if needed and starts it. "+
			"Checks start `./run.sh` with no arguments, wait for port %d, and talk to it over TCP.", p.Port)
	}
	return "Create an executable `run.sh` at the project root that builds your program if needed and runs it, passing its arguments through. " +
		"Checks run `./run.sh` with arguments in a scratch directory and read its output and the files it writes."
}

// describeCheck is a one-line, human-readable version of a check.
func describeCheck(c Check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**: ", c.Name)
	switch c.Do {
	case "exec":
		fmt.Fprintf(&b, "run `%s`", strings.TrimSpace("./run.sh "+strings.Join(c.Args, " ")))
		if c.Input != "" {
			fmt.Fprintf(&b, " with stdin `%q`", c.Input)
		}
		fmt.Fprintf(&b, ", expect exit code %d", c.ExitCode)
		if c.Expect != "" {
			fmt.Fprintf(&b, " and stdout `%q` (%s)", c.Expect, c.Match)
		}
	case "tcp":
		fmt.Fprintf(&b, "send `%q`", c.Input)
		if c.Expect != "" {
			fmt.Fprintf(&b, ", expect reply `%q` (%s)", c.Expect, c.Match)
		}
	case "write":
		fmt.Fprintf(&b, "write `%q` to `%s`", c.Input, c.Path)
	case "file":
		fmt.Fprintf(&b, "`%s` exists", c.Path)
		if c.Expect != "" {
			fmt.Fprintf(&b, " with content `%q` (%s)", c.Expect, c.Match)
		}
	}
	return b.String()
}
```

- [ ] **Step 5: Run the tests, create the golden files and read them**

Run: `gofmt -w . && go vet ./... && go test ./... -run 'TestRenderStep|TestRenderReadme|TestDescribeCheck'`
Expected: PASS. If a substring check fails, fix the template whitespace. Don't change the expected strings.

Run: `go test ./... -run TestRenderGolden -update && go test ./...`
Expected: PASS. Open `testdata/golden/README.md` and `testdata/golden/step-01.md` and check:
- Headings are separated by blank lines.
- Each hint and the machine checks sit in their own `<details>` block, followed by a blank line.
- `<no value>` appears nowhere.
- Both files end with exactly one newline.

- [ ] **Step 6: Commit**

```bash
git add render.go render_test.go templates testdata
git commit -m "feat: render README and step files"
```

---

### Task 6: Registry, project files and change detection

**Files:**
- Create: `registry.go`
- Test: `registry_test.go`

**Interfaces:**
- Consumes:
  - From Task 2: `Spec`, `decodeSpec`, `encodeSpec`, `Validate`, `stepFile` and the path constants
  - From Task 5: `renderReadme` and `renderStep`
  - Test helpers: `testSpec`, `containsAny` (Task 2) and `readFile` (Task 4)
- Produces:
  - `Registry{Projects []Entry}` and `Entry{ID, Path, Name string; Files map[string]string}`
  - `loadRegistry() (*Registry, string)`: the second value is a warning, or `""`
  - `saveEntry(e Entry) error`
  - `writeFileAtomic(path string, data []byte) error`
  - `hashOf(data []byte) string`
  - `findRoot(dir string) (string, error)`
  - `Project{Root string; Spec *Spec; entry Entry; notices, edited []string}`
  - `newProject(root string, s *Spec) (*Project, error)`
  - `openProject(dir string) (*Project, error)`
  - Methods on `*Project`: `abs(rel) string`, `write(rel, data) error`, `saveSpec() error`, `render() error`, `removeSteps() error`, `printNotices(out io.Writer)`
  - Test helper: `newTestProject(t) *Project`

- [ ] **Step 1: Write the failing tests**

`registry_test.go`:

```go
package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestProject creates a project from testSpec in a temp dir, with HOME
// pointing at another temp dir so the real registry is never touched.
func newTestProject(t *testing.T) *Project {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	p, err := newProject(t.TempDir(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.saveSpec(); err != nil {
		t.Fatal(err)
	}
	if err := p.render(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewProjectWritesFilesAndRegisters(t *testing.T) {
	p := newTestProject(t)
	for _, rel := range []string{specFile, readmeFile, stepFile(0, "create-run-script")} {
		if _, err := os.Stat(p.abs(rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if _, err := os.Stat(p.abs(stepFile(1, "respond-to-ping"))); err == nil {
		t.Error("an unexpanded step must not have a file")
	}
	reg, _ := loadRegistry()
	if len(reg.Projects) != 1 || reg.Projects[0].Path != p.Root || len(reg.Projects[0].Files) != 3 {
		t.Fatalf("registry = %+v", reg.Projects)
	}
}

func TestOpenProjectFromSubdirectory(t *testing.T) {
	p := newTestProject(t)
	sub := filepath.Join(p.Root, "internal", "server")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	q, err := openProject(sub)
	if err != nil {
		t.Fatal(err)
	}
	if q.Root != p.Root {
		t.Fatalf("root = %s, want %s", q.Root, p.Root)
	}
}

func TestOpenProjectOutsideProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := openProject(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not in a tattva project") || !strings.Contains(err.Error(), "tattva new") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenProjectRejectsBrokenSpecEdit(t *testing.T) {
	p := newTestProject(t)
	s := testSpec()
	s.Steps[1].Prerequisites = []string{"ghost"}
	if err := os.WriteFile(p.abs(specFile), encodeSpec(s), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := openProject(p.Root)
	if err == nil || !strings.Contains(err.Error(), "edited outside tattva") || !strings.Contains(err.Error(), `prerequisite "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenProjectRejectsMalformedJSON(t *testing.T) {
	p := newTestProject(t)
	if err := os.WriteFile(p.abs(specFile), []byte(`{"schema_version": 1,`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := openProject(p.Root)
	if err == nil || !strings.Contains(err.Error(), "curriculum/spec.json") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenProjectAcceptsValidSpecEditOnce(t *testing.T) {
	p := newTestProject(t)
	s := testSpec()
	s.Steps[0].Title = "Create run.sh by hand"
	if err := os.WriteFile(p.abs(specFile), encodeSpec(s), 0o644); err != nil {
		t.Fatal(err)
	}
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAny(q.notices, "edited outside tattva") {
		t.Fatalf("notices = %q", q.notices)
	}
	q2, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if containsAny(q2.notices, "edited outside tattva") {
		t.Fatalf("the edit was reported twice: %q", q2.notices)
	}
}

func TestRenderLeavesEditedFilesAlone(t *testing.T) {
	p := newTestProject(t)
	step := p.abs(stepFile(0, "create-run-script"))
	if err := os.WriteFile(step, []byte("my notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.Spec.Steps[0].Title = "Renamed"
	if err := p.render(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, step); got != "my notes" {
		t.Fatalf("the edited step file was overwritten: %q", got)
	}
	if !containsAny(p.edited, stepFile(0, "create-run-script")) {
		t.Fatalf("edited = %q", p.edited)
	}
	if !strings.Contains(readFile(t, p.abs(readmeFile)), "Renamed") {
		t.Fatal("the untouched README should be refreshed")
	}
}

func TestRenderRegeneratesDeletedFile(t *testing.T) {
	p := newTestProject(t)
	step := p.abs(stepFile(0, "create-run-script"))
	if err := os.Remove(step); err != nil {
		t.Fatal(err)
	}
	if err := p.render(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, step), "# 01 · Create run.sh") {
		t.Fatal("the deleted step file was not regenerated")
	}
}

func TestRenderDeletesStaleUntouchedFiles(t *testing.T) {
	p := newTestProject(t)
	p.Spec.Steps[0].Detail = nil
	if err := p.render(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.abs(stepFile(0, "create-run-script"))); err == nil {
		t.Fatal("a step file for an unexpanded step should be deleted")
	}
}

func TestNoBaselineAdoptsMatchingFiles(t *testing.T) {
	p := newTestProject(t)
	t.Setenv("HOME", t.TempDir()) // the registry is lost
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	q.Spec.Steps[0].Title = "Renamed"
	if err := q.render(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, q.abs(stepFile(0, "create-run-script"))), "Renamed") {
		t.Fatal("a file matching what tattva would generate should count as untouched")
	}
}

func TestRegistryFollowsMovedProject(t *testing.T) {
	p := newTestProject(t)
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(p.Root, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := openProject(moved); err != nil {
		t.Fatal(err)
	}
	reg, _ := loadRegistry()
	if len(reg.Projects) != 1 || reg.Projects[0].Path != moved {
		t.Fatalf("registry = %+v", reg.Projects)
	}
}

func TestCorruptRegistryIsIgnored(t *testing.T) {
	p := newTestProject(t)
	path, err := registryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAny(q.notices, "ignoring unreadable registry") {
		t.Fatalf("notices = %q", q.notices)
	}
	if reg, warn := loadRegistry(); warn != "" || len(reg.Projects) != 1 {
		t.Fatalf("registry not rebuilt: %q %+v", warn, reg)
	}
}

func TestRemoveSteps(t *testing.T) {
	p := newTestProject(t)
	if err := os.WriteFile(p.abs(stepFile(0, "create-run-script")), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.removeSteps(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.abs(stepsDir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the steps folder should be gone")
	}
	for rel := range p.entry.Files {
		if strings.HasPrefix(rel, stepsDir) {
			t.Fatalf("stale hash for %s", rel)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: Project`, `undefined: newProject` and similar.

- [ ] **Step 3: Write `registry.go`**

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Registry is ~/.tattva/projects.json: where each project lives, and the
// hashes of the files tattva last wrote there.
type Registry struct {
	Projects []Entry `json:"projects"`
}

// Entry is one project in the registry.
type Entry struct {
	ID    string            `json:"id"`
	Path  string            `json:"path"`
	Name  string            `json:"name"`
	Files map[string]string `json:"files"` // path relative to the project root → sha256
}

func registryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tattva", "projects.json"), nil
}

// loadRegistry reads the registry. A missing file is an empty registry. So is
// an unreadable one, with a warning, since the registry rebuilds itself.
func loadRegistry() (*Registry, string) {
	r := &Registry{}
	path, err := registryPath()
	if err != nil {
		return r, "can't find your home directory: " + err.Error()
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, ""
	}
	if err == nil {
		err = json.Unmarshal(data, r)
	}
	if err != nil {
		return &Registry{}, fmt.Sprintf("ignoring unreadable registry %s (%v); it rebuilds as you use tattva in each project", path, err)
	}
	return r, ""
}

// saveEntry writes e into the registry. It re-reads the registry first so
// tattva runs in other projects keep their entries, and drops any other entry
// with e's id or path.
func saveEntry(e Entry) error {
	r, _ := loadRegistry()
	kept := r.Projects[:0]
	for _, old := range r.Projects {
		if old.ID != e.ID && old.Path != e.Path {
			kept = append(kept, old)
		}
	}
	r.Projects = append(kept, e)
	path, err := registryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// writeFileAtomic writes data to path through a temp file and a rename, so a
// crash never leaves a half-written file.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tattva-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Project is an open tattva project: its root folder, its spec and its
// registry entry.
// ponytail: no lock across processes, so two commands on one project at once means the last write wins.
type Project struct {
	Root    string
	Spec    *Spec
	entry   Entry
	notices []string // shown by every command
	edited  []string // generated files changed outside tattva; shown by status
}

func (p *Project) abs(rel string) string { return filepath.Join(p.Root, filepath.FromSlash(rel)) }

// findRoot returns the nearest directory at or above dir that contains
// curriculum/spec.json.
func findRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, filepath.FromSlash(specFile))); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", errors.New("not in a tattva project: no curriculum/spec.json here or in any parent directory (start one with `tattva new`)")
		}
	}
}

// newProject registers a project that `tattva new` is creating in root.
func newProject(root string, s *Spec) (*Project, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: root, Spec: s, entry: Entry{ID: s.Project.ID, Path: root, Name: s.Project.Name, Files: map[string]string{}}}
	return p, saveEntry(p.entry)
}

// openProject finds the project at or above dir, validates its spec,
// refreshes its registry entry, and notes files changed outside tattva.
func openProject(dir string) (*Project, error) {
	root, err := findRoot(dir)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: root}
	data, err := os.ReadFile(p.abs(specFile))
	if err != nil {
		return nil, err
	}
	s, err := decodeSpec(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", specFile, err)
	}
	p.Spec = s

	reg, warn := loadRegistry()
	if warn != "" {
		p.notices = append(p.notices, warn)
	}
	p.entry = Entry{ID: s.Project.ID}
	for _, e := range reg.Projects {
		if e.ID == s.Project.ID {
			p.entry = e
		}
	}
	if p.entry.Files == nil {
		p.entry.Files = map[string]string{}
	}
	p.entry.Path, p.entry.Name = root, s.Project.Name

	edited := p.changed(specFile, data)
	if errs, _ := Validate(s); len(errs) > 0 {
		how := ""
		if edited {
			how = " (it was edited outside tattva)"
		}
		return nil, fmt.Errorf("%s breaks the curriculum rules%s:\n  %s", specFile, how, strings.Join(errs, "\n  "))
	}
	if edited {
		p.notices = append(p.notices, specFile+" was edited outside tattva; the edit is valid, so it's accepted")
	}
	p.entry.Files[specFile] = hashOf(data)
	if prog, err := os.ReadFile(p.abs(progressFile)); err == nil {
		if p.changed(progressFile, prog) {
			p.notices = append(p.notices, progressFile+" was edited outside tattva; accepted")
		}
		p.entry.Files[progressFile] = hashOf(prog)
	}
	p.adopt()
	return p, saveEntry(p.entry)
}

// changed reports whether data differs from the hash tattva stored for rel.
// With no stored hash there's nothing to compare against, so it isn't a change.
func (p *Project) changed(rel string, data []byte) bool {
	h, ok := p.entry.Files[rel]
	return ok && h != hashOf(data)
}

// untouched reports whether cur is exactly what tattva last wrote to rel.
func (p *Project) untouched(rel string, cur []byte) bool {
	h, ok := p.entry.Files[rel]
	return ok && h == hashOf(cur)
}

// generated is every markdown file the spec calls for, by relative path.
func (p *Project) generated() map[string][]byte {
	files := map[string][]byte{readmeFile: renderReadme(p.Spec)}
	for i, st := range p.Spec.Steps {
		if st.Detail != nil {
			files[stepFile(i, st.ID)] = renderStep(p.Spec, i)
		}
	}
	return files
}

// adopt records hashes for generated files tattva has no hash for (a new
// machine, a lost registry) when they match what it would generate now, so
// they count as untouched.
func (p *Project) adopt() {
	for rel, want := range p.generated() {
		if _, ok := p.entry.Files[rel]; ok {
			continue
		}
		if cur, err := os.ReadFile(p.abs(rel)); err == nil && bytes.Equal(cur, want) {
			p.entry.Files[rel] = hashOf(cur)
		}
	}
}

// write saves a tracked file atomically and records its hash.
func (p *Project) write(rel string, data []byte) error {
	if err := writeFileAtomic(p.abs(rel), data); err != nil {
		return err
	}
	p.entry.Files[rel] = hashOf(data)
	return saveEntry(p.entry)
}

// saveSpec writes the spec and records its hash.
func (p *Project) saveSpec() error { return p.write(specFile, encodeSpec(p.Spec)) }

// render brings the generated markdown in line with the spec. It writes
// missing files, refreshes files tattva wrote, leaves edited files alone
// (listing them in p.edited), and deletes untouched step files that are no
// longer part of the curriculum.
func (p *Project) render() error {
	want := p.generated()
	p.edited = nil
	for _, rel := range slices.Sorted(maps.Keys(want)) {
		cur, err := os.ReadFile(p.abs(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// missing: written below
		case err != nil:
			return err
		case bytes.Equal(cur, want[rel]):
			p.entry.Files[rel] = hashOf(cur)
			continue
		case !p.untouched(rel, cur):
			p.edited = append(p.edited, rel)
			continue
		}
		if err := p.write(rel, want[rel]); err != nil {
			return err
		}
	}
	existing, err := filepath.Glob(filepath.Join(p.abs(stepsDir), "*.md"))
	if err != nil {
		return err
	}
	for _, path := range existing {
		rel := stepsDir + "/" + filepath.Base(path)
		if _, ok := want[rel]; ok {
			continue
		}
		cur, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !p.untouched(rel, cur) {
			p.edited = append(p.edited, rel+" (no longer part of the curriculum)")
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		delete(p.entry.Files, rel)
	}
	return saveEntry(p.entry)
}

// removeSteps deletes every step file, edited or not (revise --force).
func (p *Project) removeSteps() error {
	if err := os.RemoveAll(p.abs(stepsDir)); err != nil {
		return err
	}
	for rel := range p.entry.Files {
		if strings.HasPrefix(rel, stepsDir+"/") {
			delete(p.entry.Files, rel)
		}
	}
	return saveEntry(p.entry)
}

// printNotices shows the notices every command reports.
func (p *Project) printNotices(out io.Writer) {
	for _, n := range p.notices {
		fmt.Fprintln(out, "note:", n)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add registry.go registry_test.go
git commit -m "feat: add project registry and change detection"
```

---

### Task 7: Progress log with `next`, `done` and `status`

**Files:**
- Create: `progress.go`
- Test: `progress_test.go`
- Modify: `main.go` (add three cases and `withProject`)
- Test: `main_test.go` (append)

**Interfaces:**
- Consumes:
  - From Task 6: `Project`, `openProject`, `(*Project).abs`, `render`, `printNotices`, `saveEntry` and `hashOf`
  - From Task 5: `num`
  - From Task 2: `Validate` and `stepFile`
  - Test helpers: `newTestProject` (Task 6), `containsAny` (Task 2)
- Produces:
  - `Event{At time.Time; Step, Event string}`
  - `now` (a var holding the clock)
  - `readEvents(path string) ([]Event, []string, error)`
  - Status constants `Locked`, `Available`, `InProgress`, `Done`
  - `Progress{Status map[string]Status; Current string; Done int; Warns []string}`
  - `progressOf(s *Spec, events []Event) Progress`
  - Methods on `*Project`: `appendEvent(step, event string) error`, `progress() (Progress, error)`
  - `cmdNext(p, out) error`, `cmdDone(p, arg string, out) error`, `cmdStatus(p, out) error`
  - `withProject(dir string, out io.Writer, fn func(*Project) error) error` (in `main.go`)

- [ ] **Step 1: Write the failing tests**

`progress_test.go`:

```go
package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ev(step, event string, minute int) Event {
	return Event{At: time.Date(2026, 9, 24, 10, minute, 0, 0, time.UTC), Step: step, Event: event}
}

func TestProgressOf(t *testing.T) {
	pr := progressOf(testSpec(), []Event{
		ev("create-run-script", "started", 1),
		ev("create-run-script", "completed", 2),
		ev("respond-to-ping", "started", 3),
		ev("ghost-step", "completed", 4),
		ev("respond-to-ping", "hint", 5), // an event type from a later version: ignored
	})
	want := map[string]Status{"create-run-script": Done, "respond-to-ping": InProgress, "echo-command": Locked}
	if !reflect.DeepEqual(pr.Status, want) {
		t.Fatalf("status = %v", pr.Status)
	}
	if pr.Current != "respond-to-ping" || pr.Done != 1 {
		t.Fatalf("current=%q done=%d", pr.Current, pr.Done)
	}
	if !containsAny(pr.Warns, "1 progress events refer to steps no longer in the curriculum") {
		t.Fatalf("warns = %q", pr.Warns)
	}
}

func TestProgressOfPicksMostRecentlyStarted(t *testing.T) {
	s := testSpec()
	s.Steps[2].Prerequisites = []string{"create-run-script"}
	pr := progressOf(s, []Event{
		ev("create-run-script", "completed", 1),
		ev("echo-command", "started", 3),
		ev("respond-to-ping", "started", 2),
	})
	if pr.Status["respond-to-ping"] != InProgress || pr.Status["echo-command"] != InProgress {
		t.Fatalf("status = %v", pr.Status)
	}
	if pr.Current != "echo-command" {
		t.Fatalf("current = %q, want the most recently started step", pr.Current)
	}
	if got := progressOf(s, []Event{ev("create-run-script", "completed", 1)}).Status["respond-to-ping"]; got != Available {
		t.Fatalf("respond-to-ping = %v, want Available", got)
	}
}

func TestReadEventsSkipsBrokenLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.jsonl")
	data := `{"at":"2026-09-24T10:00:00Z","step":"a","event":"started"}` + "\n" + `{"at":"2026-09-24T10:0`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	events, warns, err := readEvents(path)
	if err != nil || len(events) != 1 || !containsAny(warns, "line 2 isn't valid JSON") {
		t.Fatalf("events=%v warns=%q err=%v", events, warns, err)
	}
}

func TestNextStartsFirstAvailableStep(t *testing.T) {
	p := newTestProject(t)
	var out bytes.Buffer
	if err := cmdNext(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Started: 01 · Create run.sh →") {
		t.Fatalf("out = %s", out.String())
	}
	out.Reset()
	if err := cmdNext(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "In progress: 01 · Create run.sh") {
		t.Fatalf("out = %s", out.String())
	}
	if events, _, _ := readEvents(p.abs(progressFile)); len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
}

func TestNextRefusesUnexpandedStep(t *testing.T) {
	p := newTestProject(t)
	if err := cmdDone(p, "1", io.Discard); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdNext(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "isn't expanded yet") {
		t.Fatalf("out = %s", out.String())
	}
	if events, _, _ := readEvents(p.abs(progressFile)); len(events) != 1 {
		t.Fatalf("next must not start an unexpanded step: %v", events)
	}
}

func TestDone(t *testing.T) {
	p := newTestProject(t)
	var out bytes.Buffer
	if err := cmdDone(p, "", &out); err == nil || !strings.Contains(err.Error(), "no step is in progress") {
		t.Fatalf("err = %v", err)
	}
	if err := cmdNext(p, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdDone(p, "", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Done: 01 · Create run.sh (1/3 steps, 33%)") {
		t.Fatalf("out = %s", out.String())
	}
	out.Reset()
	if err := cmdDone(p, "create-run-script", &out); err != nil || !strings.Contains(out.String(), "already done") {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	out.Reset()
	if err := cmdDone(p, "3", &out); err != nil || !strings.Contains(out.String(), "prerequisite steps 02 aren't done") {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	for _, bad := range []string{"0", "4", "ghost"} {
		if err := cmdDone(p, bad, io.Discard); err == nil || !strings.Contains(err.Error(), "no step") {
			t.Errorf("done %s: err = %v", bad, err)
		}
	}
}

func TestStatus(t *testing.T) {
	p := newTestProject(t)
	if err := cmdDone(p, "1", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.abs(readmeFile), []byte("my own readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.render(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdStatus(p, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Mini Redis: Redis in go · 1/3 steps done (33%)",
		"Phase 1 · Setup",
		"  ✓ 01 Create run.sh",
		"  ○ 02 Respond to PING  (not expanded)",
		"  · 03 Echo",
		"edited outside tattva (not overwritten): curriculum/README.md",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status missing %q:\n%s", want, out.String())
		}
	}
}
```

Append to `main_test.go`:

```go
func TestRunNextDoneStatus(t *testing.T) {
	p := newTestProject(t)
	var out bytes.Buffer
	for _, args := range [][]string{{"next"}, {"done"}, {"status"}} {
		if err := run(context.Background(), p.Root, args, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, want := range []string{"Started: 01 · Create run.sh", "Done: 01 · Create run.sh", "✓ 01 Create run.sh"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	err := run(context.Background(), t.TempDir(), []string{"status"}, &out)
	if err == nil || !strings.Contains(err.Error(), "not in a tattva project") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: Event`, `undefined: progressOf`, `undefined: cmdNext` and similar.

- [ ] **Step 3: Write `progress.go`**

```go
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

// Event is one line of curriculum/progress.jsonl.
type Event struct {
	At    time.Time `json:"at"`
	Step  string    `json:"step"`
	Event string    `json:"event"`
}

// now is the clock for new events.
var now = func() time.Time { return time.Now().UTC().Truncate(time.Second) }

// readEvents reads the progress log. Lines that aren't valid JSON, such as a
// half-written last line after a crash, are skipped with a warning.
func readEvents(path string) ([]Event, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var events []Event
	var warns []string
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			warns = append(warns, fmt.Sprintf("%s line %d isn't valid JSON; ignored", progressFile, i+1))
			continue
		}
		events = append(events, ev)
	}
	return events, warns, nil
}

// Status is a step's progress, worked out from the log.
type Status int

const (
	Locked Status = iota
	Available
	InProgress
	Done
)

// Progress is the worked-out state of every step.
type Progress struct {
	Status  map[string]Status
	Current string // the most recently started step still in progress, or ""
	Done    int
	Warns   []string
}

// progressOf works out each step's status from the events. Event types it
// doesn't know are ignored, so later versions can add their own.
func progressOf(s *Spec, events []Event) Progress {
	started := map[string]time.Time{}
	completed := map[string]bool{}
	unknown := 0
	for _, ev := range events {
		if s.stepIndex(ev.Step) < 0 {
			unknown++
			continue
		}
		switch ev.Event {
		case "started":
			if t, ok := started[ev.Step]; !ok || ev.At.After(t) {
				started[ev.Step] = ev.At
			}
		case "completed":
			completed[ev.Step] = true
		}
	}
	pr := Progress{Status: map[string]Status{}}
	if unknown > 0 {
		pr.Warns = append(pr.Warns, fmt.Sprintf("%d progress events refer to steps no longer in the curriculum; ignored", unknown))
	}
	var latest time.Time
	for _, st := range s.Steps {
		t, isStarted := started[st.ID]
		switch {
		case completed[st.ID]:
			pr.Status[st.ID] = Done
			pr.Done++
		case isStarted:
			pr.Status[st.ID] = InProgress
			if pr.Current == "" || !t.Before(latest) {
				latest, pr.Current = t, st.ID
			}
		default:
			pr.Status[st.ID] = Available
			for _, pre := range st.Prerequisites {
				if !completed[pre] {
					pr.Status[st.ID] = Locked
				}
			}
		}
	}
	return pr
}

// appendEvent adds one event to the progress log and records the new hash.
func (p *Project) appendEvent(step, event string) error {
	line, err := json.Marshal(Event{At: now(), Step: step, Event: event})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p.abs(progressFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	data, err := os.ReadFile(p.abs(progressFile))
	if err != nil {
		return err
	}
	p.entry.Files[progressFile] = hashOf(data)
	return saveEntry(p.entry)
}

// progress reads the log and works out every step's status, adding any
// warnings to the project's notices.
func (p *Project) progress() (Progress, error) {
	events, warns, err := readEvents(p.abs(progressFile))
	if err != nil {
		return Progress{}, err
	}
	pr := progressOf(p.Spec, events)
	p.notices = append(p.notices, warns...)
	p.notices = append(p.notices, pr.Warns...)
	return pr, nil
}

// describeStep is "04 · Respond to PING → /path/to/curriculum/steps/04-respond-to-ping.md".
func (p *Project) describeStep(i int) string {
	st := p.Spec.Steps[i]
	return fmt.Sprintf("%s · %s → %s", num(i), st.Title, p.abs(stepFile(i, st.ID)))
}

// cmdNext prints the step in progress, or starts the next available one.
func cmdNext(p *Project, out io.Writer) error {
	pr, err := p.progress()
	if err != nil {
		return err
	}
	if pr.Current != "" {
		fmt.Fprintf(out, "In progress: %s\n", p.describeStep(p.Spec.stepIndex(pr.Current)))
		return nil
	}
	for i, st := range p.Spec.Steps {
		if pr.Status[st.ID] != Available {
			continue
		}
		if st.Detail == nil {
			fmt.Fprintf(out, "Next is step %s · %s, but it isn't expanded yet. Run `tattva expand` first.\n", num(i), st.Title)
			return nil
		}
		if err := p.appendEvent(st.ID, "started"); err != nil {
			return err
		}
		fmt.Fprintf(out, "Started: %s\n", p.describeStep(i))
		return nil
	}
	fmt.Fprintf(out, "All %d steps are done.\n", len(p.Spec.Steps))
	return nil
}

// cmdDone marks the current step, or the one arg names, as completed. arg is
// a step number if it's all digits, otherwise a step id.
func cmdDone(p *Project, arg string, out io.Writer) error {
	pr, err := p.progress()
	if err != nil {
		return err
	}
	i := -1
	switch {
	case arg == "" && pr.Current == "":
		return errors.New("no step is in progress; pass a step number or id, such as `tattva done 3`")
	case arg == "":
		i = p.Spec.stepIndex(pr.Current)
	default:
		if n, err := strconv.Atoi(arg); err == nil {
			if n >= 1 && n <= len(p.Spec.Steps) {
				i = n - 1
			}
		} else {
			i = p.Spec.stepIndex(arg)
		}
	}
	if i < 0 {
		return fmt.Errorf("no step %q in this curriculum (steps are 1-%d)", arg, len(p.Spec.Steps))
	}
	st := p.Spec.Steps[i]
	if pr.Status[st.ID] == Done {
		fmt.Fprintf(out, "Step %s · %s is already done.\n", num(i), st.Title)
		return nil
	}
	var missing []string
	for _, pre := range st.Prerequisites {
		if pr.Status[pre] != Done {
			missing = append(missing, num(p.Spec.stepIndex(pre)))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(out, "Note: prerequisite steps %s aren't done yet.\n", strings.Join(missing, ", "))
	}
	if err := p.appendEvent(st.ID, "completed"); err != nil {
		return err
	}
	total := len(p.Spec.Steps)
	fmt.Fprintf(out, "Done: %s · %s (%d/%d steps, %d%%). Run `tattva next` for the next step.\n",
		num(i), st.Title, pr.Done+1, total, (pr.Done+1)*100/total)
	return nil
}

var marks = map[Status]string{Done: "✓", InProgress: "→", Available: "○", Locked: "·"}

// cmdStatus prints the curriculum with each step's progress.
func cmdStatus(p *Project, out io.Writer) error {
	pr, err := p.progress()
	if err != nil {
		return err
	}
	s := p.Spec
	fmt.Fprintf(out, "%s: %s in %s · %d/%d steps done (%d%%)\n",
		s.Project.Name, s.Project.Target, s.Project.Language, pr.Done, len(s.Steps), pr.Done*100/len(s.Steps))
	for pi, ph := range s.Phases {
		fmt.Fprintf(out, "\nPhase %d · %s\n", pi+1, ph.Title)
		for i, st := range s.Steps {
			if st.Phase != ph.ID {
				continue
			}
			note := ""
			if st.Detail == nil {
				note = "  (not expanded)"
			}
			fmt.Fprintf(out, "  %s %s %s%s\n", marks[pr.Status[st.ID]], num(i), st.Title, note)
		}
	}
	if _, warns := Validate(s); len(warns) > 0 {
		fmt.Fprintln(out)
		for _, w := range warns {
			fmt.Fprintln(out, "warning:", w)
		}
	}
	for _, rel := range p.edited {
		fmt.Fprintln(out, "edited outside tattva (not overwritten):", rel)
	}
	return nil
}
```

- [ ] **Step 4: Wire the commands into `main.go`**

Insert these cases in `run`'s `switch`, before `default:`:

```go
	case "next", "status":
		pos, err := parseArgs(newFlags(cmd), args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return fmt.Errorf("usage: tattva %s", cmd)
		}
		return withProject(dir, out, func(p *Project) error {
			if cmd == "next" {
				return cmdNext(p, out)
			}
			return cmdStatus(p, out)
		})
	case "done":
		pos, err := parseArgs(newFlags(cmd), args)
		if err != nil {
			return err
		}
		if len(pos) > 1 {
			return errors.New("usage: tattva done [step]")
		}
		step := ""
		if len(pos) == 1 {
			step = pos[0]
		}
		return withProject(dir, out, func(p *Project) error { return cmdDone(p, step, out) })
```

Append to `main.go`:

```go
// withProject opens the project around dir, brings its generated files in
// line with the spec, runs fn, and prints any notices. Commands that change
// the spec render again themselves.
func withProject(dir string, out io.Writer, fn func(*Project) error) error {
	p, err := openProject(dir)
	if err != nil {
		return err
	}
	defer p.printNotices(out)
	if err := p.render(); err != nil {
		return err
	}
	return fn(p)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add progress.go progress_test.go main.go main_test.go
git commit -m "feat: track progress with next, done and status"
```

---

### Task 8: Design and revise with Claude

**Files:**
- Create: `generate.go`
- Create: `prompts/design.md`
- Test: `generate_test.go`
- Modify: `main.go` (add the `new` and `revise` cases)
- Test: `main_test.go` (append)

**Interfaces:**
- Consumes:
  - From Task 4: `call` and `runClaude`
  - From Task 3: `outlineSchema`
  - From Task 2: `Spec`, `Validate` and `encodeSpec`
  - From Task 6: `Project`, `newProject`, `writeFileAtomic`, `saveSpec`, `render` and `removeSteps`
  - From Task 7: `cmdDone`, used in the tests
  - Test helpers: `newTestProject`, `testSpec`, `containsAny`
- Produces:
  - `claude` (a var holding the runner; tests swap in a fake)
  - `generate(ctx context.Context, c call, check func(json.RawMessage) []string) (json.RawMessage, float64, error)`
  - `invalidOutput{Output json.RawMessage; Errors []string}`
  - `saveFailed(root, name string, e *invalidOutput) (string, error)`
  - `withSavedOutput(root, name string, err error) error`
  - `specFromOutline(out json.RawMessage, id, target, lang string) (*Spec, []string)`
  - `outlineOf(s *Spec) string`
  - `designOutline(ctx, prompt, id, target, lang, model string) (*Spec, float64, error)`
  - `cmdNew(ctx, dir, target, lang, model string, out io.Writer) error`
  - `cmdRevise(ctx, p *Project, feedback string, force bool, model string, out io.Writer) error`
  - `newUUID() string`
  - `designPrompt` and `repairPrompt`
  - Test helper: `fakeClaude(t, reply func(ctx context.Context, c call) (string, error)) *[]call`

- [ ] **Step 1: Write the failing tests**

`generate_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeClaude replaces the Claude runner for one test. reply receives every
// call and returns its output. Every call costs $0.01.
func fakeClaude(t *testing.T, reply func(ctx context.Context, c call) (string, error)) *[]call {
	t.Helper()
	var mu sync.Mutex
	var calls []call
	old := claude
	claude = func(ctx context.Context, c call) (json.RawMessage, float64, error) {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		out, err := reply(ctx, c)
		if err != nil {
			return nil, 0.01, err
		}
		return json.RawMessage(out), 0.01, nil
	}
	t.Cleanup(func() { claude = old })
	return &calls
}

func TestGenerateRepairsOnce(t *testing.T) {
	calls := fakeClaude(t, func(_ context.Context, c call) (string, error) {
		if c.System == repairPrompt {
			return `{"ok":true}`, nil
		}
		return `{"ok":false}`, nil
	})
	check := func(out json.RawMessage) []string {
		if string(out) == `{"ok":true}` {
			return nil
		}
		return []string{"ok must be true"}
	}
	out, cost, err := generate(context.Background(), call{System: "design", Web: true, Schema: []byte(`{}`)}, check)
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if len(*calls) != 2 || cost != 0.02 {
		t.Fatalf("calls=%d cost=%v", len(*calls), cost)
	}
	repair := (*calls)[1]
	if repair.Web || !strings.Contains(repair.Prompt, "ok must be true") || !strings.Contains(repair.Prompt, `{"ok":false}`) {
		t.Fatalf("repair call = %+v", repair)
	}
}

func TestGenerateGivesUpAfterOneRepair(t *testing.T) {
	calls := fakeClaude(t, func(context.Context, call) (string, error) { return `{"ok":false}`, nil })
	_, _, err := generate(context.Background(), call{Schema: []byte(`{}`)}, func(json.RawMessage) []string { return []string{"still wrong"} })
	var inv *invalidOutput
	if !errors.As(err, &inv) || len(*calls) != 2 || !containsAny(inv.Errors, "still wrong") {
		t.Fatalf("err=%v calls=%d", err, len(*calls))
	}
}

func TestNewCreatesProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	calls := fakeClaude(t, func(context.Context, call) (string, error) { return outlineOf(testSpec()), nil })
	var out bytes.Buffer
	if err := cmdNew(context.Background(), dir, "Redis", "go", "", &out); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if !c.Web || c.System != designPrompt || !strings.Contains(c.Prompt, "Target: Redis\nLanguage: go") {
		t.Fatalf("design call = %+v", c)
	}
	s, err := loadSpec(filepath.Join(dir, "curriculum", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Project.Target != "Redis" || s.Project.Language != "go" || len(s.Project.ID) != 36 || s.Project.ID == testSpec().Project.ID {
		t.Fatalf("project = %+v", s.Project)
	}
	if s.Steps[0].Detail != nil {
		t.Fatal("new must not keep step details")
	}
	if _, err := os.Stat(filepath.Join(dir, "curriculum", "README.md")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Mini Redis: 2 phases, 3 steps") {
		t.Fatalf("out = %s", out.String())
	}
	if err := cmdNew(context.Background(), dir, "Redis", "go", "", io.Discard); err == nil {
		t.Fatal("new must refuse when a curriculum already exists")
	}
}

func TestNewSavesFailedOutputAndAllowsRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	bad := testSpec()
	bad.Steps[1].Prerequisites = []string{"ghost"}
	fakeClaude(t, func(context.Context, call) (string, error) { return outlineOf(bad), nil })
	err := cmdNew(context.Background(), dir, "Redis", "go", "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "saved in") {
		t.Fatalf("err = %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "curriculum", ".failed", "design-*.json")); len(matches) != 1 {
		t.Fatalf("failed outputs = %v", matches)
	}
	if _, err := os.Stat(filepath.Join(dir, "curriculum", "spec.json")); err == nil {
		t.Fatal("no spec.json should be written")
	}
	fakeClaude(t, func(context.Context, call) (string, error) { return outlineOf(testSpec()), nil })
	if err := cmdNew(context.Background(), dir, "Redis", "go", "", io.Discard); err != nil {
		t.Fatalf("retry after a failure: %v", err)
	}
}

func TestRevise(t *testing.T) {
	p := newTestProject(t) // step 1 is expanded
	calls := fakeClaude(t, func(context.Context, call) (string, error) {
		s := testSpec()
		s.Steps[2].Title = "Echo, revised"
		return outlineOf(s), nil
	})
	if err := cmdRevise(context.Background(), p, "more echo", false, "", io.Discard); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("a refused revise must not call Claude")
	}
	if err := cmdDone(p, "1", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdRevise(context.Background(), p, "more echo", true, "", io.Discard); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if !strings.Contains(c.Prompt, "Current outline:") || !strings.Contains(c.Prompt, "more echo") || strings.Contains(c.Prompt, `"why"`) {
		t.Fatalf("revise prompt = %s", c.Prompt)
	}
	if p.Spec.Steps[2].Title != "Echo, revised" || p.Spec.Steps[0].Detail != nil {
		t.Fatal("the spec was not revised")
	}
	if _, err := os.Stat(p.abs(stepFile(0, "create-run-script"))); err == nil {
		t.Fatal("--force must delete step files")
	}
	if _, err := os.Stat(p.abs(progressFile)); err != nil {
		t.Fatal("progress must be kept")
	}
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if q.Spec.Project.ID != testSpec().Project.ID {
		t.Fatal("revise must keep the project id")
	}
}
```

Append to `main_test.go`, and add `"io"` to its imports:

```go
func TestRunNewNeedsLang(t *testing.T) {
	err := run(context.Background(), t.TempDir(), []string{"new", "Redis"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--lang") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: claude`, `undefined: generate`, `undefined: cmdNew` and similar.

- [ ] **Step 3: Write `prompts/design.md`**

```markdown
You are the curriculum designer for tattva. Tattva teaches experienced programmers how real systems work by having them build a miniature version themselves, in the spirit of "build your own X" tutorials and CodeCrafters challenges. The learner writes all the code; you design the path.

You receive a target (a system, protocol or tool the learner has used but never built, sometimes with notes about focus or their background) and the programming language they will use. You return the outline of a curriculum as JSON matching the provided schema.

# Research first

Before designing, use WebSearch and WebFetch to check how the real system works: its architecture, its wire protocol or file formats, and the exact commands and outputs its users see. Prefer primary sources: official documentation, specifications, RFCs, and the project's own source code. Record the sources you relied on in `references`. Later, the people writing each step will re-read them to write byte-exact checks.

# Choose the miniature

Pick the minimum interesting subset: the most conceptual coverage for the least implementation work. A learner should be able to finish in days or a few weeks of evenings, not months. List what the miniature includes in `scope.in`. In `scope.out`, list the important features you leave out and why, so the learner knows where the real system goes further.

Describe how the real system is built in `architecture.real` (a few paragraphs of markdown). Then describe the simplified architecture the learner will build: `architecture.components`, each with an id, one responsibility, and the ids of the components it talks to; and `architecture.diagram`, a Mermaid flowchart (for example `flowchart LR`) whose nodes are the component ids.

# The program contract

The learner's program always starts through an executable `run.sh` at the root of their project. It builds the program if needed, runs it, and passes its arguments through. Set `program`:

- `{"kind": "server", "port": <1024-65535>}` if the learner builds something clients connect to over TCP, such as a database server, a cache or an HTTP server. Use the real system's default port when it's in that range.
- `{"kind": "cli", "port": 0}` if the learner builds a command-line program, such as git, a shell or a compiler.

Every step is checked from outside, through `run.sh`: by connecting to the port and exchanging bytes, or by running `run.sh` with arguments and reading its output and the files it writes. So every step must add behaviour that can be observed that way. An internal refactor that changes no observable behaviour is not a step.

# Steps

- Step 1 always has the learner create `run.sh` and a minimal program for it to run: a server that accepts a TCP connection on the port, or a CLI that runs and exits cleanly.
- One conceptual leap per step: roughly 30 to 90 minutes of work for a competent programmer who is new to this system's internals.
- A step introduces at most 2 concepts that no earlier step used. If a step needs more, split it.
- List steps in build order. `prerequisites` holds the ids of the earlier steps a step builds on; a prerequisite must appear earlier in the list.
- Group steps into phases, each with an id, a title and its purpose. A phase's steps sit next to each other, and phases appear in the order you declare them.
- `goal` is one sentence describing the observable result of the step.

# Concepts

A concept is an idea worth a glossary entry, such as `message-framing`, `b-tree-page-split` or `content-addressable-storage`. It is not an API call or a language feature. Use generic ids that would mean the same thing in another project: `tcp-streams`, not `redis-tcp`. Every concept you declare must be used by at least one step, and every concept a step lists must be declared.

# The learner

State your assumptions about the learner in `project.assumes`: what they already know (for example "comfortable with Go") and what they don't need (for example "no networking background needed"). Unless the target says otherwise, assume a competent programmer in the chosen language who is new to this system's internals. The learner uses only the language's standard library unless the target genuinely needs something else. Never let a library do the part the learner is here to learn.

# Output

`project.name` is a short name such as "Mini Redis". `project.summary` is one or two sentences. Every id is kebab-case (lowercase letters and digits separated by single hyphens) and unique among ids of its kind.

# Revisions

If the input contains a current outline and feedback, return a complete revised outline. Apply the feedback, keep everything else that still fits, and keep the ids of steps that don't change.
```

- [ ] **Step 4: Write `generate.go`**

```go
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed prompts/design.md
var designPrompt string

const repairPrompt = `You repair JSON documents that failed validation. You receive a JSON document and the rules it breaks. Return the corrected document, matching the same schema, and change only what is needed to satisfy the rules. If a step introduces too many new concepts, split it into smaller steps and update the prerequisites that pointed at it.`

// claude runs one call. It's a var so tests can swap in a fake.
var claude = runClaude

// invalidOutput is Claude output that still broke the rules after one repair.
type invalidOutput struct {
	Output json.RawMessage
	Errors []string
}

func (e *invalidOutput) Error() string {
	return "Claude's output broke the curriculum rules even after a repair:\n  " + strings.Join(e.Errors, "\n  ")
}

// generate makes a call, checks the output, and makes at most one repair
// call. It returns the accepted output and the total cost.
func generate(ctx context.Context, c call, check func(json.RawMessage) []string) (json.RawMessage, float64, error) {
	out, cost, err := claude(ctx, c)
	if err != nil {
		return nil, cost, err
	}
	errs := check(out)
	if len(errs) == 0 {
		return out, cost, nil
	}
	prompt := "This document breaks these rules:\n- " + strings.Join(errs, "\n- ") + "\n\nDocument:\n" + string(out)
	fixed, more, err := claude(ctx, call{System: repairPrompt, Prompt: prompt, Schema: c.Schema, Model: c.Model})
	cost += more
	if err != nil {
		return nil, cost, err
	}
	if errs := check(fixed); len(errs) > 0 {
		return nil, cost, &invalidOutput{Output: fixed, Errors: errs}
	}
	return fixed, cost, nil
}

// saveFailed keeps output that broke the rules so the user can inspect it.
func saveFailed(root, name string, e *invalidOutput) (string, error) {
	path := filepath.Join(root, "curriculum", ".failed", fmt.Sprintf("%s-%s.json", name, time.Now().UTC().Format("20060102-150405")))
	data, err := json.MarshalIndent(map[string]any{"errors": e.Errors, "output": e.Output}, "", "  ")
	if err != nil {
		return "", err
	}
	return path, writeFileAtomic(path, append(data, '\n'))
}

// withSavedOutput saves rule-breaking output to curriculum/.failed and says
// where. Other errors pass through unchanged.
func withSavedOutput(root, name string, err error) error {
	var inv *invalidOutput
	if !errors.As(err, &inv) {
		return err
	}
	path, saveErr := saveFailed(root, name, inv)
	if saveErr != nil {
		return fmt.Errorf("%w (saving the output also failed: %v)", err, saveErr)
	}
	return fmt.Errorf("%w\nThe output is saved in %s", err, path)
}

// specFromOutline turns an outline from Claude into a spec with tattva's own
// fields set, and returns the rules it breaks.
func specFromOutline(out json.RawMessage, id, target, lang string) (*Spec, []string) {
	var s Spec
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, []string{"output isn't a valid outline: " + err.Error()}
	}
	s.SchemaVersion = 1
	s.Project.ID, s.Project.Target, s.Project.Language = id, target, lang
	for i := range s.Steps {
		s.Steps[i].Detail = nil
	}
	errs, _ := Validate(&s)
	return &s, errs
}

// outlineOf is s without step details, as JSON, for prompts.
func outlineOf(s *Spec) string {
	o := *s
	o.Steps = make([]Step, len(s.Steps))
	for i, st := range s.Steps {
		st.Detail = nil
		o.Steps[i] = st
	}
	return string(encodeSpec(&o))
}

// designOutline runs a design or revise call and returns a valid spec.
func designOutline(ctx context.Context, prompt, id, target, lang, model string) (*Spec, float64, error) {
	check := func(out json.RawMessage) []string {
		_, errs := specFromOutline(out, id, target, lang)
		return errs
	}
	out, cost, err := generate(ctx, call{System: designPrompt, Prompt: prompt, Schema: outlineSchema, Web: true, Model: model}, check)
	if err != nil {
		return nil, cost, err
	}
	s, _ := specFromOutline(out, id, target, lang)
	return s, cost, nil
}

// cmdNew designs a new curriculum in dir.
func cmdNew(ctx context.Context, dir, target, lang, model string, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(specFile))); err == nil {
		return fmt.Errorf("%s already exists here; use `tattva revise` to change it", specFile)
	}
	start := time.Now()
	fmt.Fprintln(out, "Designing the curriculum. This usually takes a few minutes…")
	s, cost, err := designOutline(ctx, fmt.Sprintf("Target: %s\nLanguage: %s\n", target, lang), newUUID(), target, lang, model)
	if err != nil {
		return withSavedOutput(dir, "design", err)
	}
	p, err := newProject(dir, s)
	if err != nil {
		return err
	}
	if err := p.saveSpec(); err != nil {
		return err
	}
	if err := p.render(); err != nil {
		return err
	}
	summarize(p, time.Since(start), cost, out)
	fmt.Fprintln(out, "Review curriculum/README.md, then run `tattva revise \"<feedback>\"` or `tattva expand`.")
	return nil
}

// cmdRevise rewrites the outline from the current one plus feedback.
func cmdRevise(ctx context.Context, p *Project, feedback string, force bool, model string, out io.Writer) error {
	expanded := false
	for _, st := range p.Spec.Steps {
		expanded = expanded || st.Detail != nil
	}
	if expanded && !force {
		return errors.New("some steps are already expanded; `tattva revise --force` drops all step details and deletes every step file")
	}
	start := time.Now()
	fmt.Fprintln(out, "Revising the curriculum. This usually takes a few minutes…")
	info := p.Spec.Project
	prompt := fmt.Sprintf("Target: %s\nLanguage: %s\n\nCurrent outline:\n%s\nRevise the outline according to this feedback:\n%s\n",
		info.Target, info.Language, outlineOf(p.Spec), feedback)
	s, cost, err := designOutline(ctx, prompt, info.ID, info.Target, info.Language, model)
	if err != nil {
		return withSavedOutput(p.Root, "revise", err)
	}
	if expanded {
		if err := p.removeSteps(); err != nil {
			return err
		}
	}
	p.Spec = s
	if err := p.saveSpec(); err != nil {
		return err
	}
	if err := p.render(); err != nil {
		return err
	}
	summarize(p, time.Since(start), cost, out)
	return nil
}

// summarize prints what a design or revise call produced.
func summarize(p *Project, took time.Duration, cost float64, out io.Writer) {
	s := p.Spec
	fmt.Fprintf(out, "%s: %d phases, %d steps (%s, $%.2f)\n", s.Project.Name, len(s.Phases), len(s.Steps), took.Round(time.Second), cost)
	for pi, ph := range s.Phases {
		fmt.Fprintf(out, "  Phase %d · %s\n", pi+1, ph.Title)
	}
	_, warns := Validate(s)
	for _, w := range warns {
		fmt.Fprintln(out, "warning:", w)
	}
}

// newUUID returns a random version 4 UUID.
func newUUID() string {
	var b [16]byte
	rand.Read(b[:]) // crypto/rand.Read never fails as of Go 1.24
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
```

- [ ] **Step 5: Wire `new` and `revise` into `main.go`**

Insert these cases in `run`'s `switch`, before `default:`:

```go
	case "new":
		fs := newFlags(cmd)
		lang := fs.String("lang", "", "language you'll build in")
		model := fs.String("model", "", "Claude model")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 1 || *lang == "" {
			return errors.New(`usage: tattva new "<target>" --lang <lang> [--model <m>]`)
		}
		return cmdNew(ctx, dir, pos[0], *lang, *model, out)
	case "revise":
		fs := newFlags(cmd)
		force := fs.Bool("force", false, "drop step details and step files")
		model := fs.String("model", "", "Claude model")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New(`usage: tattva revise "<feedback>" [--force] [--model <m>]`)
		}
		return withProject(dir, out, func(p *Project) error { return cmdRevise(ctx, p, pos[0], *force, *model, out) })
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add generate.go generate_test.go prompts/design.md main.go main_test.go
git commit -m "feat: design and revise curricula with Claude"
```

---

### Task 9: Expand phases in parallel

**Files:**
- Modify: `generate.go` (add `"slices"` and `"sync"` to the imports, plus the code below)
- Create: `prompts/expand.md`
- Test: `generate_test.go` (append)
- Modify: `main.go` (add the `expand` case)

**Interfaces:**
- Consumes:
  - From Task 8: `generate`, `withSavedOutput`, `outlineOf` and `claude`
  - From Task 3: `phaseSchema`, `PhaseDetails` and `StepDetail`
  - From Task 2: `detailErrors` and `Validate`
  - From Task 6: `Project`, `saveSpec` and `render`
  - Test helpers: `fakeClaude`, `newTestProject`, `testDetail`, `containsAny`
- Produces:
  - `expandPrompt`
  - `expandConcurrency = 3`
  - `phaseErrors(kind string, out json.RawMessage, want []string) []string`
  - `(p *Project) mergePhase(out json.RawMessage) error`
  - `cmdExpand(ctx context.Context, p *Project, model string, out io.Writer) error`

- [ ] **Step 1: Write the failing tests**

Append to `generate_test.go`:

```go
// phaseReply returns valid details for exactly the steps an expand prompt asks for.
func phaseReply(c call) string {
	_, ids, _ := strings.Cut(c.Prompt, "in this order: ")
	var pd PhaseDetails
	for _, id := range strings.Split(strings.TrimSpace(ids), ", ") {
		pd.Steps = append(pd.Steps, StepDetail{ID: id, Detail: *testDetail()})
	}
	return mustJSON(pd)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestPhaseErrors(t *testing.T) {
	good := json.RawMessage(mustJSON(PhaseDetails{Steps: []StepDetail{{ID: "a", Detail: *testDetail()}}}))
	if errs := phaseErrors("server", good, []string{"a"}); len(errs) > 0 {
		t.Fatalf("errs = %q", errs)
	}
	if errs := phaseErrors("server", good, []string{"a", "b"}); !containsAny(errs, "want exactly [a b]") {
		t.Fatalf("errs = %q", errs)
	}
	if errs := phaseErrors("cli", good, []string{"a"}); !containsAny(errs, "only for server programs") {
		t.Fatalf("errs = %q", errs)
	}
}

func TestExpandMergesPhasesAndResumes(t *testing.T) {
	p := newTestProject(t)
	p.Spec.Steps[0].Detail = nil // both phases now need expanding
	if err := p.saveSpec(); err != nil {
		t.Fatal(err)
	}
	failSetup := true
	calls := fakeClaude(t, func(_ context.Context, c call) (string, error) {
		if failSetup && strings.Contains(c.Prompt, "Phase to expand: setup") {
			return "", errors.New("usage limit reached")
		}
		return phaseReply(c), nil
	})
	var out bytes.Buffer
	err := cmdExpand(context.Background(), p, "", &out)
	if err == nil || !strings.Contains(err.Error(), "1 phases failed (setup)") {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
	for _, c := range *calls {
		if c.System != expandPrompt || !c.Web || string(c.Schema) != string(phaseSchema) || !strings.Contains(c.Prompt, "Full outline:") {
			t.Fatalf("expand call = %+v", c)
		}
	}
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if q.Spec.Steps[0].Detail != nil || q.Spec.Steps[1].Detail == nil || q.Spec.Steps[2].Detail == nil {
		t.Fatal("the finished phase must be saved and the failed one must not")
	}
	if _, err := os.Stat(q.abs(stepFile(1, "respond-to-ping"))); err != nil {
		t.Fatal("the finished phase should have step files")
	}

	failSetup = false
	n := len(*calls)
	if err := cmdExpand(context.Background(), q, "", io.Discard); err != nil {
		t.Fatal(err)
	}
	if retried := (*calls)[n:]; len(retried) != 1 || !strings.Contains(retried[0].Prompt, "Phase to expand: setup") {
		t.Fatalf("resume should retry only setup, got %d calls", len(retried))
	}
	if q.Spec.Steps[0].Detail == nil {
		t.Fatal("setup not expanded on retry")
	}
	out.Reset()
	if err := cmdExpand(context.Background(), q, "", &out); err != nil || !strings.Contains(out.String(), "already expanded") {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
}

func TestExpandSavesInvalidPhaseOutput(t *testing.T) {
	p := newTestProject(t)
	fakeClaude(t, func(context.Context, call) (string, error) { return `{"steps":[]}`, nil })
	err := cmdExpand(context.Background(), p, "", io.Discard)
	matches, _ := filepath.Glob(p.abs("curriculum/.failed/expand-protocol-*.json"))
	if err == nil || len(matches) != 1 {
		t.Fatalf("err=%v failed outputs=%v", err, matches)
	}
}

func TestExpandInterruptKeepsFinishedPhases(t *testing.T) {
	p := newTestProject(t)
	p.Spec.Steps[0].Detail = nil
	if err := p.saveSpec(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeClaude(t, func(_ context.Context, c call) (string, error) {
		if strings.Contains(c.Prompt, "Phase to expand: protocol") {
			cancel() // Ctrl-C while this phase is still running
			return "", context.Canceled
		}
		return phaseReply(c), nil
	})
	err := cmdExpand(ctx, p, "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "run `tattva expand` again") {
		t.Fatalf("err = %v", err)
	}
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if q.Spec.Steps[0].Detail == nil {
		t.Fatal("the phase that finished must be saved")
	}
	if q.Spec.Steps[1].Detail != nil {
		t.Fatal("the interrupted phase must not be saved")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: phaseErrors`, `undefined: cmdExpand` and `undefined: expandPrompt`.

- [ ] **Step 3: Write `prompts/expand.md`**

```markdown
You write the step-by-step instructions for a tattva curriculum. Tattva teaches experienced programmers how a real system works by having them build a miniature version themselves. You receive the full curriculum outline, the language the learner uses, one phase, and the ids of the steps in that phase to fill in. For exactly those steps, return the `detail` object defined by the schema.

# Principles

- The learner writes all the code. Explain what to build and why. Never give solution code.
- Describe behaviour, never internal structure. Don't name functions, types or files the learner must create, and don't assume internal APIs from other steps. Other phases are being written at the same time as yours, so they can only rely on the observable behaviour in each step's goal. Checks test observable behaviour only.
- Write for the learner's language. Constraints and hints can name the relevant parts of that language's standard library where it helps.
- Re-read the outline's references with WebFetch whenever you need exact bytes, formats, commands or outputs. Checks must be byte-exact.

# Fields

- `why`: why this step matters and which problem it solves in the real system.
- `context`: what the learner's program can already do (from earlier steps' goals) and where this step fits in the architecture.
- `task`: what to build, in markdown. Be specific about the required behaviour, inputs, outputs and edge cases. Stay silent on how to structure the code.
- `constraints`: short rules, such as "standard library only" or "handle partial reads". May be empty.
- `hints`: exactly three, escalating. First a conceptual nudge toward the key idea. Then a specific hint about the approach or the tricky part. Then pseudocode for the core logic, written as plain pseudocode rather than in the learner's language.
- `checks`: black-box checks, described below.
- `by_hand`: markdown telling the learner how to check the step themselves and what they should see. Use common tools where possible (redis-cli, nc, curl, git, sqlite3) or `./run.sh` directly.
- `expected_outcome`: one or two sentences on what works once the step is done.
- `common_mistakes`: at least one mistake learners typically make here.
- `reflection`: one question that makes the learner reason about why the solution works or what would break it.

# Checks

The program contract is in the outline's `program`. The checks of a step run in order, in a fresh empty scratch directory, against the learner's `run.sh`. Every check has all its fields; set unused ones to empty values (`""`, `[]`, `0`) and `match` to "exact".

- `exec` (cli programs only): runs `run.sh` with `args`, sending `input` on stdin. Passes if the exit status equals `exit_code` and, when `expect` isn't empty, stdout matches `expect`.
- `tcp` (server programs only): the server has already been started and is accepting connections. Opens a new connection to 127.0.0.1 on the program's port, sends `input`, and passes when the reply matches `expect`. `input` must not be empty.
- `write`: writes `input` to `path` in the scratch directory, to set up a fixture. Checks nothing.
- `file`: passes if `path` exists in the scratch directory and, when `expect` isn't empty, its content matches `expect`.

`path` is always relative and never contains `..`. `match` is `exact`, `contains` or `regex` (Go RE2 syntax, unanchored unless you anchor it); use `regex` for output that legitimately varies, such as hashes that depend on the time. Within a step, a server keeps running and the scratch directory persists across its checks; each step starts fresh. Payloads are text: write "\r\n" for CRLF. Prefer two or three focused checks per step. If a step's behaviour genuinely can't be checked this way (for example, concurrent clients), return no checks and explain how to check it in `by_hand`.
```

- [ ] **Step 4: Add expansion to `generate.go`**

Add `"slices"` and `"sync"` to the import block of `generate.go`, then append:

```go
//go:embed prompts/expand.md
var expandPrompt string

// expandConcurrency is how many phases expand at once.
const expandConcurrency = 3 // ponytail: fixed; lower it if calls start hitting rate limits

// phaseErrors checks one expand call's output (rules 10 to 12): exactly the
// requested steps, each with a valid detail.
func phaseErrors(kind string, out json.RawMessage, want []string) []string {
	var pd PhaseDetails
	if err := json.Unmarshal(out, &pd); err != nil {
		return []string{"output isn't valid phase details: " + err.Error()}
	}
	var errs, got []string
	for _, sd := range pd.Steps {
		got = append(got, sd.ID)
		d := sd.Detail
		e, _ := detailErrors(kind, &d)
		for _, msg := range e {
			errs = append(errs, fmt.Sprintf("step %q: %s", sd.ID, msg))
		}
	}
	if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
		errs = append(errs, fmt.Sprintf("returned steps [%s], want exactly [%s]", strings.Join(got, " "), strings.Join(want, " ")))
	}
	return errs
}

// mergePhase stores one phase's details, saves the spec and renders the new
// step files. The caller holds the expand mutex.
func (p *Project) mergePhase(out json.RawMessage) error {
	var pd PhaseDetails
	if err := json.Unmarshal(out, &pd); err != nil {
		return err
	}
	for _, sd := range pd.Steps {
		d := sd.Detail
		p.Spec.Steps[p.Spec.stepIndex(sd.ID)].Detail = &d
	}
	if err := p.saveSpec(); err != nil {
		return err
	}
	return p.render()
}

// cmdExpand fills in every unexpanded step, with one call per phase and a few
// phases at a time. Each phase is saved as soon as it finishes.
func cmdExpand(ctx context.Context, p *Project, model string, out io.Writer) error {
	type job struct {
		phase string
		ids   []string
	}
	var jobs []job
	for _, ph := range p.Spec.Phases {
		var ids []string
		for _, st := range p.Spec.Steps {
			if st.Phase == ph.ID && st.Detail == nil {
				ids = append(ids, st.ID)
			}
		}
		if len(ids) > 0 {
			jobs = append(jobs, job{ph.ID, ids})
		}
	}
	if len(jobs) == 0 {
		fmt.Fprintln(out, "Every step is already expanded.")
		return nil
	}

	start := time.Now()
	fmt.Fprintf(out, "Expanding %d phases, %d at a time. Each takes a few minutes…\n", len(jobs), expandConcurrency)
	outline, lang, kind := outlineOf(p.Spec), p.Spec.Project.Language, p.Spec.Program.Kind
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		sem    = make(chan struct{}, expandConcurrency)
		total  float64
		done   int
		failed []string
	)
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			began := time.Now()
			prompt := fmt.Sprintf("Language: %s\n\nFull outline:\n%s\nPhase to expand: %s\nFill in detail for exactly these step ids, in this order: %s\n",
				lang, outline, j.phase, strings.Join(j.ids, ", "))
			res, cost, err := generate(ctx, call{System: expandPrompt, Prompt: prompt, Schema: phaseSchema, Web: true, Model: model},
				func(o json.RawMessage) []string { return phaseErrors(kind, o, j.ids) })

			mu.Lock()
			defer mu.Unlock()
			total += cost
			done++
			if err == nil {
				err = p.mergePhase(res)
			}
			if err != nil {
				failed = append(failed, j.phase)
				fmt.Fprintf(out, "✗ phase %s failed (%d/%d): %v\n", j.phase, done, len(jobs), withSavedOutput(p.Root, "expand-"+j.phase, err))
				return
			}
			fmt.Fprintf(out, "✓ phase %s expanded (%d/%d, %s, $%.2f)\n", j.phase, done, len(jobs), time.Since(began).Round(time.Second), cost)
		}()
	}
	wg.Wait()
	fmt.Fprintf(out, "Finished in %s, $%.2f in total.\n", time.Since(start).Round(time.Second), total)
	_, warns := Validate(p.Spec)
	for _, w := range warns {
		fmt.Fprintln(out, "warning:", w)
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d phases failed (%s); run `tattva expand` again to retry them", len(failed), strings.Join(failed, ", "))
	}
	return nil
}
```

- [ ] **Step 5: Wire `expand` into `main.go`**

Insert this case in `run`'s `switch`, before `default:`:

```go
	case "expand":
		fs := newFlags(cmd)
		model := fs.String("model", "", "Claude model")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: tattva expand [--model <m>]")
		}
		return withProject(dir, out, func(p *Project) error { return cmdExpand(ctx, p, *model, out) })
```

- [ ] **Step 6: Run the tests, including the race detector**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS with no data races reported.

- [ ] **Step 7: Commit**

```bash
git add generate.go generate_test.go prompts/expand.md main.go
git commit -m "feat: expand phases in parallel"
```

---

### Task 10: `list` command

**Files:**
- Modify: `registry.go` (add `"time"` to the imports, plus the code below)
- Test: `registry_test.go` (append)
- Modify: `main.go` (add the `list` case)

**Interfaces:**
- Consumes:
  - From Task 6: `loadRegistry`, `saveEntry` and `Entry`
  - From Task 2: `loadSpec` and the path constants
  - From Task 7: `readEvents` and `progressOf`
  - From Task 5: `num`
  - Test helpers: `newTestProject` and `cmdNext`
- Produces: `cmdList(out io.Writer) error` and `lastActivity(root string, events []Event) string`

- [ ] **Step 1: Write the failing tests**

Append to `registry_test.go`, and add `"bytes"` and `"io"` to its imports:

```go
func TestList(t *testing.T) {
	p := newTestProject(t)
	if err := cmdNext(p, io.Discard); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	if err := saveEntry(Entry{ID: "x", Path: gone, Name: "Gone Project", Files: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdList(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Gone Project  (missing: " + gone + ")", "Mini Redis  0%  → 01 Create run.sh  last activity "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list missing %q:\n%s", want, out.String())
		}
	}
	if strings.Index(out.String(), "Gone Project") > strings.Index(out.String(), "Mini Redis") {
		t.Error("projects should be sorted by name")
	}
}

func TestListWithNoProjects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	if err := cmdList(&out); err != nil || !strings.Contains(out.String(), "No projects yet") {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: cmdList`.

- [ ] **Step 3: Add `cmdList` to `registry.go`**

Add `"time"` to the import block of `registry.go`, then append:

```go
// cmdList prints every registered project with its progress.
func cmdList(out io.Writer) error {
	reg, warn := loadRegistry()
	if warn != "" {
		fmt.Fprintln(out, "note:", warn)
	}
	if len(reg.Projects) == 0 {
		fmt.Fprintln(out, "No projects yet. Start one with `tattva new \"<target>\" --lang <lang>`.")
		return nil
	}
	projects := slices.Clone(reg.Projects)
	slices.SortFunc(projects, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	for _, e := range projects {
		s, err := loadSpec(filepath.Join(e.Path, filepath.FromSlash(specFile)))
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(out, "%s  (missing: %s)\n", e.Name, e.Path)
			continue
		}
		if err != nil {
			fmt.Fprintf(out, "%s  (can't read its spec: %v)\n", e.Name, err)
			continue
		}
		events, _, err := readEvents(filepath.Join(e.Path, filepath.FromSlash(progressFile)))
		if err != nil {
			return err
		}
		pr := progressOf(s, events)
		current := "no step in progress"
		if i := s.stepIndex(pr.Current); i >= 0 {
			current = "→ " + num(i) + " " + s.Steps[i].Title
		} else if pr.Done == len(s.Steps) {
			current = "all done"
		}
		fmt.Fprintf(out, "%s  %d%%  %s  last activity %s  %s\n",
			s.Project.Name, pr.Done*100/len(s.Steps), current, lastActivity(e.Path, events), e.Path)
	}
	return nil
}

// lastActivity is the time of the last progress event, or of the spec's last
// change if there are no events.
func lastActivity(root string, events []Event) string {
	var t time.Time
	for _, ev := range events {
		if ev.At.After(t) {
			t = ev.At
		}
	}
	if t.IsZero() {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(specFile))); err == nil {
			t = fi.ModTime()
		}
	}
	return t.Local().Format("2006-01-02 15:04")
}
```

- [ ] **Step 4: Wire `list` into `main.go`**

Insert this case in `run`'s `switch`, before `default:`:

```go
	case "list":
		pos, err := parseArgs(newFlags(cmd), args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: tattva list")
		}
		return cmdList(out)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add registry.go registry_test.go main.go
git commit -m "feat: list projects"
```

---

### Task 11: Live smoke run (spends real tokens, so ask the user before starting)

**Files:** none are created. Prompt changes come out of this task only if the results call for them.

**Interfaces:**
- Consumes: the built `tattva` binary
- Produces: a report for the user covering success criteria 1–2, plus the paths they need to judge criteria 3–4

- [ ] **Step 1: Ask the user before spending tokens**

This task calls the real Claude three times for design and about 15–25 times for expansion. Confirm the user is happy to spend that usage, and agree where the smoke projects should live. The default is `$SMOKE=<session scratchpad>/smoke`.

- [ ] **Step 2: Build and run the full test suite**

Run: `cd /Users/rohitkk074/Documents/projects/tattva && gofmt -l . && go vet ./... && go test -race ./... && go build -o tattva .`
Expected: `gofmt -l` prints nothing, the tests PASS, and the `./tattva` binary exists.

- [ ] **Step 3: Generate the three curricula**

```bash
T=/Users/rohitkk074/Documents/projects/tattva/tattva
for target in "Redis" "Git" "a small SQL database like SQLite"; do
  dir="$SMOKE/$(echo "$target" | tr ' ' '-' | tr -cd '[:alnum:]-' | cut -c1-24)"
  mkdir -p "$dir" && cd "$dir"
  $T new "$target" --lang go && $T expand && $T status
done
```

For each target, record the design time and cost, the expand time and cost, the number of repairs (visible in the output), the number of steps, and any warnings. If a call fails, record the error and the `curriculum/.failed/` file.

- [ ] **Step 4: Check the machine-checkable criteria**

- Criterion 1: all three `spec.json` files load, meaning `$T status` succeeds in each folder, with at most one repair per call.
- Criterion 2: design took about 5 minutes or less and expand about 15 minutes or less for Redis. If expand was slower, note whether lowering or raising `expandConcurrency` would help.
- Read a sample of checks in each curriculum (`curriculum/steps/*.md`, under "Machine checks"). Flag any whose bytes look wrong against the references.

- [ ] **Step 5: Report to the user**

Give the user:
- a table with target, steps, phases, design time and cost, expand time and cost, repairs and warnings
- the three `curriculum/README.md` paths, for criterion 3
- the path of the Redis step 1 file, so they can start criterion 4 with `$T next`
- any prompt changes you'd suggest, based on patterns in the output

Don't change the prompts without the user agreeing.
