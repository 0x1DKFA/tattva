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
