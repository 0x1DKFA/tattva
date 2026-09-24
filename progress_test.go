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
