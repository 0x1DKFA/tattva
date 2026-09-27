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
	"syscall"
	"time"
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

// saveEntry writes e into the registry. Under the registry lock it re-reads
// the registry, so tattva runs in other projects keep their entries, and drops
// any other entry with e's id or path.
func saveEntry(e Entry) error {
	unlock, err := lockRegistry()
	if err != nil {
		return err
	}
	defer unlock()
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

// lockRegistry takes an exclusive lock on ~/.tattva/projects.lock, so
// concurrent tattva runs update the registry one at a time. The returned
// function releases it.
func lockRegistry() (func(), error) {
	path, err := registryPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(strings.TrimSuffix(path, ".json")+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil // closing the file releases the lock
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
// ponytail: only registry writes are locked; two commands on one project at once can still overwrite each other's spec.json (last write wins).
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

// projectView is one registered project as `tattva list` and the workspace
// show it. Loading it never writes anything.
type projectView struct {
	N                  int // position in name order, from 1: the window number in the top bar
	ID, Name, Slug     string
	Target, Language   string
	Done, Total, Hints int
	Focus              string // "→ 08 Key expiry", "all done" or "no step in progress"
	Problem            string // why the project can't be shown, or ""
	root               string
	spec               *Spec
	events             []Event
}

// loadProjects reads every registered project, sorted by name, without
// writing anything. The second result is a warning about the registry itself.
func loadProjects() ([]projectView, string) {
	reg, warn := loadRegistry()
	entries := slices.Clone(reg.Projects)
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	views := make([]projectView, 0, len(entries))
	for i, e := range entries {
		v := projectView{N: i + 1, ID: e.ID, Name: e.Name, Slug: slug(e.Name), root: e.Path}
		v.Problem = v.load()
		views = append(views, v)
	}
	return views, warn
}

// load fills v from its project folder, and returns why it couldn't, or "".
func (v *projectView) load() string {
	s, err := loadSpec(filepath.Join(v.root, filepath.FromSlash(specFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return "missing: " + v.root
	}
	if err != nil {
		return "can't read its spec: " + err.Error()
	}
	if errs, _ := Validate(s); len(errs) > 0 {
		return "its spec breaks the curriculum rules; run `tattva status` in " + v.root + " for details"
	}
	events, _, err := readEvents(filepath.Join(v.root, filepath.FromSlash(progressFile)))
	if err != nil {
		return "can't read its progress: " + err.Error()
	}
	pr := progressOf(s, events)
	v.Name, v.Slug = s.Project.Name, slug(s.Project.Name)
	v.Target, v.Language = s.Project.Target, s.Project.Language
	v.Done, v.Total = pr.Done, len(s.Steps)
	for _, n := range pr.Hints {
		v.Hints += n
	}
	v.Focus = focusOf(s, pr)
	v.spec, v.events = s, events
	return ""
}

// focusOf is what a project is up to.
func focusOf(s *Spec, pr Progress) string {
	if i := s.stepIndex(pr.Current); i >= 0 {
		return "→ " + num(i) + " " + s.Steps[i].Title
	}
	if pr.Done == len(s.Steps) {
		return "all done"
	}
	return "no step in progress"
}

// cmdList prints every registered project with its progress.
func cmdList(out io.Writer) error {
	projects, warn := loadProjects()
	if warn != "" {
		fmt.Fprintln(out, "note:", warn)
	}
	if len(projects) == 0 {
		fmt.Fprintln(out, "No projects yet. Start one with `tattva new \"<target>\" --lang <lang>`.")
		return nil
	}
	for _, v := range projects {
		if v.Problem != "" {
			fmt.Fprintf(out, "%s  (%s)\n", v.Name, v.Problem)
			continue
		}
		fmt.Fprintf(out, "%s  %d%%  %s  last activity %s  %s\n",
			v.Name, v.Done*100/v.Total, v.Focus, lastActivity(v.root, v.events), v.root)
	}
	return nil
}

// lastActivity is the time of the last progress event, or of the spec's last
// change if there are no events.
func lastActivity(root string, events []Event) string {
	var t time.Time
	for _, ev := range events {
		if ev.At.After(t) {
			t = ev.At
		}
	}
	if t.IsZero() {
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(specFile))); err == nil {
			t = fi.ModTime()
		}
	}
	return t.Local().Format("2006-01-02 15:04")
}
