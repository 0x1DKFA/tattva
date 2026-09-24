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
