package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Registry is ~/.tattva/projects.json: where each project lives, and the
// hashes of the files tattva last wrote there.
type Registry struct {
	Projects []Entry `json:"projects"`
}

// Entry is one project in the registry.
type Entry struct {
	ID    string            `json:"id"`
	Path  string            `json:"path"`
	Name  string            `json:"name"`
	Files map[string]string `json:"files"` // path relative to the project root → sha256
}

func registryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tattva", "projects.json"), nil
}

// loadRegistry reads the registry. A missing file is an empty registry. So is
// an unreadable one, with a warning, since the registry rebuilds itself.
func loadRegistry() (*Registry, string) {
	r := &Registry{}
	path, err := registryPath()
	if err != nil {
		return r, "can't find your home directory: " + err.Error()
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, ""
	}
	if err == nil {
		err = json.Unmarshal(data, r)
	}
	if err != nil {
		return &Registry{}, fmt.Sprintf("ignoring unreadable registry %s (%v); it rebuilds as you use tattva in each project", path, err)
	}
	return r, ""
}

// saveEntry writes e into the registry. It re-reads the registry first so
// tattva runs in other projects keep their entries, and drops any other entry
// with e's id or path.
func saveEntry(e Entry) error {
	r, _ := loadRegistry()
	kept := r.Projects[:0]
	for _, old := range r.Projects {
		if old.ID != e.ID && old.Path != e.Path {
			kept = append(kept, old)
		}
	}
	r.Projects = append(kept, e)
	path, err := registryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// writeFileAtomic writes data to path through a temp file and a rename, so a
// crash never leaves a half-written file.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tattva-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Project is an open tattva project: its root folder, its spec and its
// registry entry.
// ponytail: no lock across processes, so two commands on one project at once means the last write wins.
type Project struct {
	Root    string
	Spec    *Spec
	entry   Entry
	notices []string // shown by every command
	edited  []string // generated files changed outside tattva; shown by status
}

func (p *Project) abs(rel string) string { return filepath.Join(p.Root, filepath.FromSlash(rel)) }

// findRoot returns the nearest directory at or above dir that contains
// curriculum/spec.json.
func findRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, filepath.FromSlash(specFile))); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", errors.New("not in a tattva project: no curriculum/spec.json here or in any parent directory (start one with `tattva new`)")
		}
	}
}

// newProject registers a project that `tattva new` is creating in root.
func newProject(root string, s *Spec) (*Project, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: root, Spec: s, entry: Entry{ID: s.Project.ID, Path: root, Name: s.Project.Name, Files: map[string]string{}}}
	return p, saveEntry(p.entry)
}

// openProject finds the project at or above dir, validates its spec,
// refreshes its registry entry, and notes files changed outside tattva.
func openProject(dir string) (*Project, error) {
	root, err := findRoot(dir)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: root}
	data, err := os.ReadFile(p.abs(specFile))
	if err != nil {
		return nil, err
	}
	s, err := decodeSpec(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", specFile, err)
	}
	p.Spec = s

	reg, warn := loadRegistry()
	if warn != "" {
		p.notices = append(p.notices, warn)
	}
	p.entry = Entry{ID: s.Project.ID}
	for _, e := range reg.Projects {
		if e.ID == s.Project.ID {
			p.entry = e
		}
	}
	if p.entry.Files == nil {
		p.entry.Files = map[string]string{}
	}
	p.entry.Path, p.entry.Name = root, s.Project.Name

	edited := p.changed(specFile, data)
	if errs, _ := Validate(s); len(errs) > 0 {
		how := ""
		if edited {
			how = " (it was edited outside tattva)"
		}
		return nil, fmt.Errorf("%s breaks the curriculum rules%s:\n  %s", specFile, how, strings.Join(errs, "\n  "))
	}
	if edited {
		p.notices = append(p.notices, specFile+" was edited outside tattva; the edit is valid, so it's accepted")
	}
	p.entry.Files[specFile] = hashOf(data)
	if prog, err := os.ReadFile(p.abs(progressFile)); err == nil {
		if p.changed(progressFile, prog) {
			p.notices = append(p.notices, progressFile+" was edited outside tattva; accepted")
		}
		p.entry.Files[progressFile] = hashOf(prog)
	}
	p.adopt()
	return p, saveEntry(p.entry)
}

// changed reports whether data differs from the hash tattva stored for rel.
// With no stored hash there's nothing to compare against, so it isn't a change.
func (p *Project) changed(rel string, data []byte) bool {
	h, ok := p.entry.Files[rel]
	return ok && h != hashOf(data)
}

// untouched reports whether cur is exactly what tattva last wrote to rel.
func (p *Project) untouched(rel string, cur []byte) bool {
	h, ok := p.entry.Files[rel]
	return ok && h == hashOf(cur)
}

// generated is every markdown file the spec calls for, by relative path.
func (p *Project) generated() map[string][]byte {
	files := map[string][]byte{readmeFile: renderReadme(p.Spec)}
	for i, st := range p.Spec.Steps {
		if st.Detail != nil {
			files[stepFile(i, st.ID)] = renderStep(p.Spec, i)
		}
	}
	return files
}

// adopt records hashes for generated files tattva has no hash for (a new
// machine, a lost registry) when they match what it would generate now, so
// they count as untouched.
func (p *Project) adopt() {
	for rel, want := range p.generated() {
		if _, ok := p.entry.Files[rel]; ok {
			continue
		}
		if cur, err := os.ReadFile(p.abs(rel)); err == nil && bytes.Equal(cur, want) {
			p.entry.Files[rel] = hashOf(cur)
		}
	}
}

// write saves a tracked file atomically and records its hash.
func (p *Project) write(rel string, data []byte) error {
	if err := writeFileAtomic(p.abs(rel), data); err != nil {
		return err
	}
	p.entry.Files[rel] = hashOf(data)
	return saveEntry(p.entry)
}

// saveSpec writes the spec and records its hash.
func (p *Project) saveSpec() error { return p.write(specFile, encodeSpec(p.Spec)) }

// render brings the generated markdown in line with the spec. It writes
// missing files, refreshes files tattva wrote, leaves edited files alone
// (listing them in p.edited), and deletes untouched step files that are no
// longer part of the curriculum.
func (p *Project) render() error {
	want := p.generated()
	p.edited = nil
	for _, rel := range slices.Sorted(maps.Keys(want)) {
		cur, err := os.ReadFile(p.abs(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// missing: written below
		case err != nil:
			return err
		case bytes.Equal(cur, want[rel]):
			p.entry.Files[rel] = hashOf(cur)
			continue
		case !p.untouched(rel, cur):
			p.edited = append(p.edited, rel)
			continue
		}
		if err := p.write(rel, want[rel]); err != nil {
			return err
		}
	}
	existing, err := filepath.Glob(filepath.Join(p.abs(stepsDir), "*.md"))
	if err != nil {
		return err
	}
	for _, path := range existing {
		rel := stepsDir + "/" + filepath.Base(path)
		if _, ok := want[rel]; ok {
			continue
		}
		cur, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !p.untouched(rel, cur) {
			p.edited = append(p.edited, rel+" (no longer part of the curriculum)")
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		delete(p.entry.Files, rel)
	}
	return saveEntry(p.entry)
}

// removeSteps deletes every step file, edited or not (revise --force).
func (p *Project) removeSteps() error {
	if err := os.RemoveAll(p.abs(stepsDir)); err != nil {
		return err
	}
	for rel := range p.entry.Files {
		if strings.HasPrefix(rel, stepsDir+"/") {
			delete(p.entry.Files, rel)
		}
	}
	return saveEntry(p.entry)
}

// printNotices shows the notices every command reports.
func (p *Project) printNotices(out io.Writer) {
	for _, n := range p.notices {
		fmt.Fprintln(out, "note:", n)
	}
}
