package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeCodexScript = `#!/bin/sh
printf '%s\0' "$@" > "$FAKE_DIR/args"
cat > "$FAKE_DIR/stdin"
schema=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-schema) schema="$2"; shift 2 ;;
    --output-last-message) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cp "$schema" "$FAKE_DIR/schema"
if [ -n "$FAKE_OUTPUT" ]; then
  printf '%s\n' "$FAKE_OUTPUT" > "$output"
else
  printf '%s\n' '{"answer":42}' > "$output"
fi
printf '%s\n' '{"type":"turn.started"}' '{"type":"item.completed","item":{"type":"agent_message","text":"Done."}}'
`

func installFakeCodex(t *testing.T) string {
	t.Helper()
	bin, rec := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(fakeCodexScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", rec)
	return rec
}

func TestRunCodexStructuredOutput(t *testing.T) {
	rec := installFakeCodex(t)
	prompt := "Research Redis and return JSON."
	var terminal strings.Builder
	out, cost, err := runGeneration(context.Background(), call{
		Provider: "codex", System: "be precise", Prompt: prompt, Schema: []byte(`{"type":"object"}`), Web: true, Model: "gpt-test", Log: &runLog{out: &terminal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"answer":42}` {
		t.Fatalf("output = %s", out)
	}
	if cost >= 0 || costLabel(cost) != "cost not reported" {
		t.Fatalf("cost = %v (%s)", cost, costLabel(cost))
	}
	args := readFile(t, filepath.Join(rec, "args"))
	for _, want := range []string{"--search", "--ignore-user-config", "exec", "--json", "--ephemeral", "--sandbox\x00read-only", "--ask-for-approval\x00never", "--model\x00gpt-test", "--output-schema", "--output-last-message", "-"} {
		if !strings.Contains(args, want+"\x00") {
			t.Errorf("args missing %q: %q", want, args)
		}
	}
	if got := readFile(t, filepath.Join(rec, "schema")); got != `{"type":"object"}` {
		t.Errorf("schema = %q", got)
	}
	if got := readFile(t, filepath.Join(rec, "stdin")); got != "Instructions:\nbe precise\n\nRequest:\n"+prompt {
		t.Errorf("stdin = %q", got)
	}
	if !strings.Contains(terminal.String(), "Done.") {
		t.Errorf("Codex progress not shown: %q", terminal.String())
	}
}

func TestRunCodexWithoutSearch(t *testing.T) {
	rec := installFakeCodex(t)
	if _, _, err := runCodex(context.Background(), call{Schema: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	args := readFile(t, filepath.Join(rec, "args"))
	if strings.Contains(args, "--search\x00") || strings.Contains(args, "--model\x00") {
		t.Fatalf("unexpected flags: %q", args)
	}
}
