package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

// Paths inside a project, relative to its root.
const (
	specFile     = "curriculum/spec.json"
	progressFile = "curriculum/progress.jsonl"
	readmeFile   = "curriculum/README.md"
	stepsDir     = "curriculum/steps"
)

// Spec is the Learning Spec in curriculum/spec.json, the contract between
// Claude and tattva. Fields tagged schema:"-" are set by tattva and never
// requested from Claude.
type Spec struct {
	SchemaVersion int          `json:"schema_version" schema:"-"`
	Project       ProjectInfo  `json:"project"`
	Scope         Scope        `json:"scope"`
	Architecture  Architecture `json:"architecture"`
	Program       Program      `json:"program"`
	Concepts      []Concept    `json:"concepts"`
	References    []Reference  `json:"references"`
	Phases        []Phase      `json:"phases"`
	Steps         []Step       `json:"steps"`
}

type ProjectInfo struct {
	ID       string   `json:"id" schema:"-"`
	Name     string   `json:"name"`
	Target   string   `json:"target" schema:"-"`
	Language string   `json:"language" schema:"-"`
	Summary  string   `json:"summary"`
	Assumes  []string `json:"assumes"`
}

type Scope struct {
	In  []string   `json:"in"`
	Out []Excluded `json:"out"`
}

type Excluded struct {
	Feature string `json:"feature"`
	Why     string `json:"why"`
}

type Architecture struct {
	Real       string      `json:"real"`
	Components []Component `json:"components"`
	Diagram    string      `json:"diagram"`
}

type Component struct {
	ID             string   `json:"id"`
	Responsibility string   `json:"responsibility"`
	TalksTo        []string `json:"talks_to"`
}

type Program struct {
	Kind string `json:"kind" enum:"server,cli"`
	Port int    `json:"port"`
}

type Concept struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type Reference struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Phase struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Purpose string `json:"purpose"`
}

type Step struct {
	ID            string   `json:"id"`
	Phase         string   `json:"phase"`
	Title         string   `json:"title"`
	Goal          string   `json:"goal"`
	Concepts      []string `json:"concepts"`
	Prerequisites []string `json:"prerequisites"`
	Detail        *Detail  `json:"detail" schema:"-"` // nil until expanded
}

type Detail struct {
	Why             string   `json:"why"`
	Context         string   `json:"context"`
	Task            string   `json:"task"`
	Constraints     []string `json:"constraints"`
	Hints           []string `json:"hints"`
	Checks          []Check  `json:"checks"`
	ByHand          string   `json:"by_hand"`
	ExpectedOutcome string   `json:"expected_outcome"`
	CommonMistakes  []string `json:"common_mistakes"`
	Reflection      string   `json:"reflection"`
}

// Check is one black-box check. Every field is always present; fields a check
// doesn't use are empty.
type Check struct {
	Name     string   `json:"name"`
	Do       string   `json:"do" enum:"exec,tcp,write,file"`
	Args     []string `json:"args"`
	Input    string   `json:"input"`
	Path     string   `json:"path"`
	Expect   string   `json:"expect"`
	Match    string   `json:"match" enum:"exact,contains,regex"`
	ExitCode int      `json:"exit_code"`
}

// decodeSpec strictly decodes a spec: unknown fields are errors, so a typo in
// a hand edit isn't silently dropped.
func decodeSpec(data []byte) (*Spec, error) {
	var s Spec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// loadSpec reads and decodes the spec file at path.
func loadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := decodeSpec(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// encodeSpec is the exact bytes tattva writes to spec.json: indented, with
// <, > and & left readable.
func encodeSpec(s *Spec) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		panic(err) // Spec has only marshalable fields
	}
	return buf.Bytes()
}

// stepIndex returns the position of the step with id, or -1.
func (s *Spec) stepIndex(id string) int {
	for i, st := range s.Steps {
		if st.ID == id {
			return i
		}
	}
	return -1
}

// stepFile is the path of step i's markdown file, relative to the project root.
func stepFile(i int, id string) string {
	return fmt.Sprintf("%s/%02d-%s.md", stepsDir, i+1, id)
}

var kebab = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxNewConcepts is the "one conceptual leap" rule: how many concepts a step
// may introduce that no earlier step used.
const maxNewConcepts = 2 // ponytail: heuristic; tune after the first real curricula

// Validate checks s against the validity rules in the design spec. errs are
// rule violations; warns are printed but don't stop anything.
func Validate(s *Spec) (errs, warns []string) {
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	// Rule 9.
	if s.SchemaVersion != 1 {
		add("schema_version is %d, want 1", s.SchemaVersion)
	}

	// Rule 1: ids are kebab-case and unique within their kind.
	idSet := func(kind string, ids []string) map[string]bool {
		set := map[string]bool{}
		for _, id := range ids {
			if !kebab.MatchString(id) {
				add("%s id %q is not kebab-case", kind, id)
			}
			if set[id] {
				add("duplicate %s id %q", kind, id)
			}
			set[id] = true
		}
		return set
	}
	var phaseIDs, stepIDs, conceptIDs, componentIDs []string
	for _, p := range s.Phases {
		phaseIDs = append(phaseIDs, p.ID)
	}
	for _, st := range s.Steps {
		stepIDs = append(stepIDs, st.ID)
	}
	for _, c := range s.Concepts {
		conceptIDs = append(conceptIDs, c.ID)
	}
	for _, c := range s.Architecture.Components {
		componentIDs = append(componentIDs, c.ID)
	}
	phases := idSet("phase", phaseIDs)
	idSet("step", stepIDs)
	concepts := idSet("concept", conceptIDs)
	components := idSet("component", componentIDs)

	// Rule 2.
	if len(s.Phases) == 0 {
		add("there are no phases")
	}
	if len(s.Steps) > 99 {
		add("there are %d steps, the maximum is 99", len(s.Steps))
	}

	// Rule 3: steps grouped by phase, phases in declared order, none empty.
	var order []string
	for _, st := range s.Steps {
		if !phases[st.Phase] {
			add("step %q: phase %q is not declared", st.ID, st.Phase)
		}
		if len(order) == 0 || order[len(order)-1] != st.Phase {
			order = append(order, st.Phase)
		}
	}
	if strings.Join(order, ",") != strings.Join(phaseIDs, ",") {
		add("steps must be grouped by phase in declared order, with no empty phase: steps give [%s], phases are [%s]",
			strings.Join(order, " "), strings.Join(phaseIDs, " "))
	}

	// Rule 4: prerequisites point to earlier steps.
	earlier := map[string]bool{}
	for _, st := range s.Steps {
		for _, pre := range st.Prerequisites {
			if !earlier[pre] {
				add("step %q: prerequisite %q is not an earlier step", st.ID, pre)
			}
		}
		earlier[st.ID] = true
	}

	// Rules 5 and 6: concepts are declared and used; few new ones per step.
	seen := map[string]bool{}
	for _, st := range s.Steps {
		if len(st.Concepts) == 0 {
			add("step %q has no concepts", st.ID)
		}
		var fresh []string
		for _, c := range st.Concepts {
			if !concepts[c] {
				add("step %q: concept %q is not declared", st.ID, c)
			}
			if !seen[c] {
				fresh = append(fresh, c)
				seen[c] = true
			}
		}
		if len(fresh) > maxNewConcepts {
			add("step %q introduces %d new concepts (%s); the maximum is %d, so split the step",
				st.ID, len(fresh), strings.Join(fresh, ", "), maxNewConcepts)
		}
	}
	for _, c := range conceptIDs {
		if !seen[c] {
			add("concept %q is never used by a step", c)
		}
	}

	// Rule 7.
	for _, c := range s.Architecture.Components {
		for _, other := range c.TalksTo {
			if !components[other] {
				add("component %q talks to undeclared component %q", c.ID, other)
			}
		}
	}

	// Rule 8.
	switch {
	case s.Program.Kind == "server" && (s.Program.Port < 1024 || s.Program.Port > 65535):
		add("server port %d must be between 1024 and 65535", s.Program.Port)
	case s.Program.Kind == "cli" && s.Program.Port != 0:
		add("cli programs have port 0, got %d", s.Program.Port)
	case s.Program.Kind != "server" && s.Program.Kind != "cli":
		add("program kind %q must be server or cli", s.Program.Kind)
	}

	// Rules 10 and 11: expanded steps.
	for _, st := range s.Steps {
		if st.Detail == nil {
			continue
		}
		e, w := detailErrors(s.Program.Kind, st.Detail)
		for _, msg := range e {
			add("step %q: %s", st.ID, msg)
		}
		for _, msg := range w {
			warns = append(warns, fmt.Sprintf("step %q: %s", st.ID, msg))
		}
	}
	return errs, warns
}

// detailErrors checks one expanded step's detail (rules 10 and 11).
func detailErrors(kind string, d *Detail) (errs, warns []string) {
	fields := []struct{ name, value string }{
		{"why", d.Why}, {"context", d.Context}, {"task", d.Task},
		{"by_hand", d.ByHand}, {"expected_outcome", d.ExpectedOutcome}, {"reflection", d.Reflection},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			errs = append(errs, f.name+" is empty")
		}
	}
	if len(d.Hints) != 3 {
		errs = append(errs, fmt.Sprintf("has %d hints, want exactly 3", len(d.Hints)))
	}
	for i, h := range d.Hints {
		if strings.TrimSpace(h) == "" {
			errs = append(errs, fmt.Sprintf("hint %d is empty", i+1))
		}
	}
	if len(d.CommonMistakes) == 0 {
		errs = append(errs, "has no common mistakes")
	}
	if len(d.Checks) == 0 {
		warns = append(warns, "has no checks")
	}
	for i, c := range d.Checks {
		for _, msg := range checkErrors(kind, c) {
			errs = append(errs, fmt.Sprintf("check %d (%s): %s", i+1, c.Name, msg))
		}
	}
	return errs, warns
}

// checkErrors checks one check against the program kind (rule 11).
func checkErrors(kind string, c Check) []string {
	var errs []string
	bad := func(msg string) { errs = append(errs, msg) }
	if strings.TrimSpace(c.Name) == "" {
		bad("name is empty")
	}
	switch c.Do {
	case "exec":
		if kind != "cli" {
			bad("exec checks are only for cli programs")
		}
		if c.Path != "" {
			bad("exec checks don't use path")
		}
	case "tcp":
		if kind != "server" {
			bad("tcp checks are only for server programs")
		}
		if c.Input == "" {
			bad("tcp checks need input")
		}
		if len(c.Args) > 0 || c.Path != "" || c.ExitCode != 0 {
			bad("tcp checks only use input, expect and match")
		}
	case "write":
		if len(c.Args) > 0 || c.Expect != "" || c.ExitCode != 0 {
			bad("write checks only use path and input")
		}
	case "file":
		if len(c.Args) > 0 || c.Input != "" || c.ExitCode != 0 {
			bad("file checks only use path, expect and match")
		}
	default:
		bad(fmt.Sprintf("do %q must be exec, tcp, write or file", c.Do))
	}
	for _, f := range []struct{ name, value string }{{"input", c.Input}, {"expect", c.Expect}} {
		if !isText(f.value) {
			bad(f.name + " contains binary bytes; checks are text only, so create binary files by running the program " +
				"earlier in the step, or drop the check and explain it in by_hand")
		}
	}
	if (c.Do == "write" || c.Do == "file") && !safeRelPath(c.Path) {
		bad(fmt.Sprintf("path %q must be relative with no .. segment", c.Path))
	}
	switch c.Match {
	case "exact", "contains":
	case "regex":
		if _, err := regexp.Compile(c.Expect); err != nil {
			bad("regex doesn't compile: " + err.Error())
		}
	default:
		bad(fmt.Sprintf("match %q must be exact, contains or regex", c.Match))
	}
	return errs
}

// isText reports whether s is plain text: no control characters other than
// tab, newline and carriage return.
func isText(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f {
			return false
		}
	}
	return true
}

// safeRelPath reports whether p stays inside the directory it's relative to.
func safeRelPath(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// PhaseDetails is what one expand call returns.
type PhaseDetails struct {
	Steps []StepDetail `json:"steps"`
}

// StepDetail is one step's detail as returned by an expand call.
type StepDetail struct {
	ID     string `json:"id"`
	Detail Detail `json:"detail"`
}

// The schemas sent to Claude, generated from the types so they can't drift.
var (
	outlineSchema = mustSchema(Spec{})
	phaseSchema   = mustSchema(PhaseDetails{})
)

func mustSchema(v any) []byte {
	b, err := json.Marshal(schemaFor(reflect.TypeOf(v)))
	if err != nil {
		panic(err)
	}
	return b
}

// schemaFor builds a strict JSON Schema for t: every property is required,
// extra properties are forbidden, `enum` tags become enums, and fields tagged
// schema:"-" are left out.
func schemaFor(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		return schemaFor(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": schemaFor(t.Elem())}
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		for i := range t.NumField() {
			f := t.Field(i)
			if f.Tag.Get("schema") == "-" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			s := schemaFor(f.Type)
			if enum := f.Tag.Get("enum"); enum != "" {
				s["enum"] = strings.Split(enum, ",")
			}
			props[name] = s
			required = append(required, name)
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	panic("schemaFor: unsupported kind " + t.Kind().String())
}
