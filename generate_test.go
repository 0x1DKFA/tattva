package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	out, cost, broken, err := generate(context.Background(), call{System: "design", Prompt: "Target: Redis", Web: true, Schema: []byte(`{}`)}, check)
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if strings.Join(broken, "|") != "ok must be true" {
		t.Fatalf("broken = %q, want the first draft's rule errors", broken)
	}
	if len(*calls) != 2 || cost != 0.02 {
		t.Fatalf("calls=%d cost=%v", len(*calls), cost)
	}
	repair := (*calls)[1]
	if repair.Web || !strings.Contains(repair.Prompt, "ok must be true") || !strings.Contains(repair.Prompt, `{"ok":false}`) ||
		!strings.Contains(repair.Prompt, "Target: Redis") {
		t.Fatalf("repair call = %+v", repair)
	}
}

func TestGenerateGivesUpAfterOneRepair(t *testing.T) {
	calls := fakeClaude(t, func(context.Context, call) (string, error) { return `{"ok":false}`, nil })
	_, _, _, err := generate(context.Background(), call{Schema: []byte(`{}`)}, func(json.RawMessage) []string { return []string{"still wrong"} })
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
	if err := cmdNew(context.Background(), dir, "Redis", "go", "", "claude", &out); err != nil {
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
	if err := cmdNew(context.Background(), dir, "Redis", "go", "", "claude", io.Discard); err == nil {
		t.Fatal("new must refuse when a curriculum already exists")
	}
}

func TestNewSavesFailedOutputAndAllowsRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	bad := testSpec()
	bad.Steps[1].Prerequisites = []string{"ghost"}
	fakeClaude(t, func(context.Context, call) (string, error) { return outlineOf(bad), nil })
	err := cmdNew(context.Background(), dir, "Redis", "go", "", "claude", io.Discard)
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
	if err := cmdNew(context.Background(), dir, "Redis", "go", "", "claude", io.Discard); err != nil {
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
	if err := cmdRevise(context.Background(), p, "more echo", false, "", "claude", io.Discard); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("a refused revise must not call Claude")
	}
	if err := cmdDone(p, "1", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdRevise(context.Background(), p, "more echo", true, "", "claude", io.Discard); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if !strings.Contains(c.Prompt, "Current outline:") || !strings.Contains(c.Prompt, "more echo") || strings.Contains(c.Prompt, `"hints"`) {
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
	err := cmdExpand(context.Background(), p, "", "claude", &out)
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
	if err := cmdExpand(context.Background(), q, "", "claude", io.Discard); err != nil {
		t.Fatal(err)
	}
	if retried := (*calls)[n:]; len(retried) != 1 || !strings.Contains(retried[0].Prompt, "Phase to expand: setup") {
		t.Fatalf("resume should retry only setup, got %d calls", len(retried))
	}
	if q.Spec.Steps[0].Detail == nil {
		t.Fatal("setup not expanded on retry")
	}
	out.Reset()
	if err := cmdExpand(context.Background(), q, "", "claude", &out); err != nil || !strings.Contains(out.String(), "already expanded") {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
}

func TestExpandSavesInvalidPhaseOutput(t *testing.T) {
	p := newTestProject(t)
	fakeClaude(t, func(context.Context, call) (string, error) { return `{"steps":[]}`, nil })
	err := cmdExpand(context.Background(), p, "", "claude", io.Discard)
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
	err := cmdExpand(ctx, p, "", "claude", io.Discard)
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

func TestNewReportsARepair(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bad := testSpec()
	bad.Steps[1].Prerequisites = []string{"ghost"}
	fakeClaude(t, func(_ context.Context, c call) (string, error) {
		if c.System == repairPrompt {
			return outlineOf(testSpec()), nil
		}
		return outlineOf(bad), nil
	})
	var out bytes.Buffer
	if err := cmdNew(context.Background(), t.TempDir(), "Redis", "go", "", "claude", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "repaired after the first draft broke these rules") || !strings.Contains(out.String(), `prerequisite "ghost"`) {
		t.Fatalf("out = %s", out.String())
	}
}

func TestExpandReportsARepair(t *testing.T) {
	p := newTestProject(t) // only the protocol phase needs expanding
	both := PhaseDetails{Steps: []StepDetail{{ID: "respond-to-ping", Detail: *testDetail()}, {ID: "echo-command", Detail: *testDetail()}}}
	fakeClaude(t, func(_ context.Context, c call) (string, error) {
		if c.System == repairPrompt {
			return mustJSON(both), nil
		}
		return mustJSON(PhaseDetails{Steps: both.Steps[:1]}), nil
	})
	var out bytes.Buffer
	if err := cmdExpand(context.Background(), p, "", "claude", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "repaired after the first draft broke these rules") || !strings.Contains(out.String(), "want exactly") {
		t.Fatalf("out = %s", out.String())
	}
}

// logPathIn finds the "log: <path>" line a command printed.
func logPathIn(t *testing.T, out string) string {
	t.Helper()
	m := regexp.MustCompile(`log: (\S+\.log)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no log path in:\n%s", out)
	}
	return m[1]
}

func TestGenerateLogsTheRuleCheckAndRepair(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := fakeClaude(t, func(_ context.Context, c call) (string, error) {
		if c.System == repairPrompt {
			return `{"ok":true}`, nil
		}
		return `{"ok":false}`, nil
	})
	var term bytes.Buffer
	lg := openLog("test", &term)
	_, _, _, err := generate(context.Background(), call{Label: "design", Log: lg, Schema: []byte(`{}`)}, func(out json.RawMessage) []string {
		if string(out) == `{"ok":true}` {
			return nil
		}
		return []string{"ok must be true"}
	})
	lg.Close(nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := (*calls)[1]; r.Label != "design repair" || r.Log != lg {
		t.Fatalf("repair call label=%q, shares the log=%v", r.Label, r.Log == lg)
	}
	log := readFile(t, lg.Path)
	for _, want := range []string{"[design] the first draft broke 1 rule; asking the selected provider to repair it", "[design]   broke: ok must be true", "[design repair] rules: ok"} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
	if !strings.Contains(term.String(), "  [design] the first draft broke 1 rule; asking the selected provider to repair it\n") {
		t.Errorf("the repair should show in the terminal:\n%s", term.String())
	}
}

func TestNewWritesALog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeClaude(t, func(context.Context, call) (string, error) { return outlineOf(testSpec()), nil })
	var out bytes.Buffer
	if err := cmdNew(context.Background(), t.TempDir(), "Redis", "go", "", "claude", &out); err != nil {
		t.Fatal(err)
	}
	log := readFile(t, logPathIn(t, out.String()))
	for _, want := range []string{`tattva new "Redis" --lang go in `, "[design] rules: ok", "Mini Redis: 2 phases, 3 steps (", " done\n"} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
}

func TestExpandLogsEachPhase(t *testing.T) {
	p := newTestProject(t)
	calls := fakeClaude(t, func(_ context.Context, c call) (string, error) { return phaseReply(c), nil })
	var out bytes.Buffer
	if err := cmdExpand(context.Background(), p, "", "claude", &out); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].Label != "expand protocol" {
		t.Errorf("call label = %q", (*calls)[0].Label)
	}
	log := readFile(t, logPathIn(t, out.String()))
	for _, want := range []string{"tattva expand in ", "[expand protocol] rules: ok", "✓ phase protocol expanded"} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
}
