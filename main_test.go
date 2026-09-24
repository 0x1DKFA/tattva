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
