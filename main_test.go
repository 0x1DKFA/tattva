package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain hides real model CLIs so no test can call them accidentally.
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

func TestRunNewNeedsLang(t *testing.T) {
	err := run(context.Background(), t.TempDir(), []string{"new", "Redis"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--lang") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRejectsUnknownProvider(t *testing.T) {
	err := run(context.Background(), t.TempDir(), []string{"new", "Redis", "--lang", "go", "--provider", "other"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "choose claude or codex") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunNewWithCodexProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeCodex(t)
	t.Setenv("FAKE_OUTPUT", outlineOf(testSpec()))
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run(context.Background(), dir, []string{"new", "Redis", "--lang", "go", "--provider", "codex"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "curriculum", "spec.json")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cost not reported") {
		t.Errorf("output doesn't describe Codex cost: %s", out.String())
	}
}

// Without the handler, this SIGTERM would kill the test binary outright,
// just as `kill` would leave a running generator call orphaned.
func TestInterruptContextCatchesSIGTERM(t *testing.T) {
	ctx, stop := interruptContext()
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("SIGTERM did not cancel the context")
	}
}
