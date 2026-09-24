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
