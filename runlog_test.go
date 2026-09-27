package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOpenLogWritesATimestampedFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var term bytes.Buffer
	lg := openLog("new", &term)
	lg.Printf("hello %d", 1)
	lg.Show("  shown %s", "too")
	lg.Close(nil)
	if !regexp.MustCompile(`/\.tattva/logs/\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-new-\d+\.log$`).MatchString(lg.Path) {
		t.Fatalf("path = %s", lg.Path)
	}
	text := readFile(t, lg.Path)
	if !regexp.MustCompile(`(?m)^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} hello 1$`).MatchString(text) || !strings.Contains(text, " shown too\n") {
		t.Fatalf("log = %q", text)
	}
	if want := "  log: " + lg.Path + "\n  shown too\n"; term.String() != want {
		t.Fatalf("terminal = %q, want %q", term.String(), want)
	}
}

func TestOpenLogCarriesOnWithoutAFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(home, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	var term bytes.Buffer
	lg := openLog("new", &term)
	lg.Printf("into the void")
	lg.Show("still shown")
	lg.Close(nil)
	if !strings.Contains(term.String(), "log: unavailable") || !strings.Contains(term.String(), "still shown\n") {
		t.Fatalf("terminal = %q", term.String())
	}
	var none *runLog // a nil log does nothing
	none.Printf("x")
	none.Show("x")
	none.Close(nil)
}
