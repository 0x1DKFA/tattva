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
		{"path escapes", func(s *Spec) {
			addCheck(s, Check{Name: "escape", Do: "write", Path: "../x", Input: "hi", Match: "exact"})
		}, `path "../x" must be relative`},
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
