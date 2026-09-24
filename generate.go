package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed prompts/design.md
var designPrompt string

const repairPrompt = `You repair JSON documents that failed validation. You receive a JSON document and the rules it breaks. Return the corrected document, matching the same schema, and change only what is needed to satisfy the rules. If a step introduces too many new concepts, split it into smaller steps and update the prerequisites that pointed at it.`

// claude runs one call. It's a var so tests can swap in a fake.
var claude = runClaude

// invalidOutput is Claude output that still broke the rules after one repair.
type invalidOutput struct {
	Output json.RawMessage
	Errors []string
}

func (e *invalidOutput) Error() string {
	return "Claude's output broke the curriculum rules even after a repair:\n  " + strings.Join(e.Errors, "\n  ")
}

// generate makes a call, checks the output, and makes at most one repair
// call. It returns the accepted output and the total cost.
func generate(ctx context.Context, c call, check func(json.RawMessage) []string) (json.RawMessage, float64, error) {
	out, cost, err := claude(ctx, c)
	if err != nil {
		return nil, cost, err
	}
	errs := check(out)
	if len(errs) == 0 {
		return out, cost, nil
	}
	prompt := "This document breaks these rules:\n- " + strings.Join(errs, "\n- ") + "\n\nDocument:\n" + string(out)
	fixed, more, err := claude(ctx, call{System: repairPrompt, Prompt: prompt, Schema: c.Schema, Model: c.Model})
	cost += more
	if err != nil {
		return nil, cost, err
	}
	if errs := check(fixed); len(errs) > 0 {
		return nil, cost, &invalidOutput{Output: fixed, Errors: errs}
	}
	return fixed, cost, nil
}

// saveFailed keeps output that broke the rules so the user can inspect it.
func saveFailed(root, name string, e *invalidOutput) (string, error) {
	path := filepath.Join(root, "curriculum", ".failed", fmt.Sprintf("%s-%s.json", name, time.Now().UTC().Format("20060102-150405")))
	data, err := json.MarshalIndent(map[string]any{"errors": e.Errors, "output": e.Output}, "", "  ")
	if err != nil {
		return "", err
	}
	return path, writeFileAtomic(path, append(data, '\n'))
}

// withSavedOutput saves rule-breaking output to curriculum/.failed and says
// where. Other errors pass through unchanged.
func withSavedOutput(root, name string, err error) error {
	var inv *invalidOutput
	if !errors.As(err, &inv) {
		return err
	}
	path, saveErr := saveFailed(root, name, inv)
	if saveErr != nil {
		return fmt.Errorf("%w (saving the output also failed: %v)", err, saveErr)
	}
	return fmt.Errorf("%w\nThe output is saved in %s", err, path)
}

// specFromOutline turns an outline from Claude into a spec with tattva's own
// fields set, and returns the rules it breaks.
func specFromOutline(out json.RawMessage, id, target, lang string) (*Spec, []string) {
	var s Spec
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, []string{"output isn't a valid outline: " + err.Error()}
	}
	s.SchemaVersion = 1
	s.Project.ID, s.Project.Target, s.Project.Language = id, target, lang
	for i := range s.Steps {
		s.Steps[i].Detail = nil
	}
	errs, _ := Validate(&s)
	return &s, errs
}

// outlineOf is s without step details, as JSON, for prompts.
func outlineOf(s *Spec) string {
	o := *s
	o.Steps = make([]Step, len(s.Steps))
	for i, st := range s.Steps {
		st.Detail = nil
		o.Steps[i] = st
	}
	return string(encodeSpec(&o))
}

// designOutline runs a design or revise call and returns a valid spec.
func designOutline(ctx context.Context, prompt, id, target, lang, model string) (*Spec, float64, error) {
	check := func(out json.RawMessage) []string {
		_, errs := specFromOutline(out, id, target, lang)
		return errs
	}
	out, cost, err := generate(ctx, call{System: designPrompt, Prompt: prompt, Schema: outlineSchema, Web: true, Model: model}, check)
	if err != nil {
		return nil, cost, err
	}
	s, _ := specFromOutline(out, id, target, lang)
	return s, cost, nil
}

// cmdNew designs a new curriculum in dir.
func cmdNew(ctx context.Context, dir, target, lang, model string, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(specFile))); err == nil {
		return fmt.Errorf("%s already exists here; use `tattva revise` to change it", specFile)
	}
	start := time.Now()
	fmt.Fprintln(out, "Designing the curriculum. This usually takes a few minutes…")
	s, cost, err := designOutline(ctx, fmt.Sprintf("Target: %s\nLanguage: %s\n", target, lang), newUUID(), target, lang, model)
	if err != nil {
		return withSavedOutput(dir, "design", err)
	}
	p, err := newProject(dir, s)
	if err != nil {
		return err
	}
	if err := p.saveSpec(); err != nil {
		return err
	}
	if err := p.render(); err != nil {
		return err
	}
	summarize(p, time.Since(start), cost, out)
	fmt.Fprintln(out, "Review curriculum/README.md, then run `tattva revise \"<feedback>\"` or `tattva expand`.")
	return nil
}

// cmdRevise rewrites the outline from the current one plus feedback.
func cmdRevise(ctx context.Context, p *Project, feedback string, force bool, model string, out io.Writer) error {
	expanded := false
	for _, st := range p.Spec.Steps {
		expanded = expanded || st.Detail != nil
	}
	if expanded && !force {
		return errors.New("some steps are already expanded; `tattva revise --force` drops all step details and deletes every step file")
	}
	start := time.Now()
	fmt.Fprintln(out, "Revising the curriculum. This usually takes a few minutes…")
	info := p.Spec.Project
	prompt := fmt.Sprintf("Target: %s\nLanguage: %s\n\nCurrent outline:\n%s\nRevise the outline according to this feedback:\n%s\n",
		info.Target, info.Language, outlineOf(p.Spec), feedback)
	s, cost, err := designOutline(ctx, prompt, info.ID, info.Target, info.Language, model)
	if err != nil {
		return withSavedOutput(p.Root, "revise", err)
	}
	if expanded {
		if err := p.removeSteps(); err != nil {
			return err
		}
	}
	p.Spec = s
	if err := p.saveSpec(); err != nil {
		return err
	}
	if err := p.render(); err != nil {
		return err
	}
	summarize(p, time.Since(start), cost, out)
	return nil
}

// summarize prints what a design or revise call produced.
func summarize(p *Project, took time.Duration, cost float64, out io.Writer) {
	s := p.Spec
	fmt.Fprintf(out, "%s: %d phases, %d steps (%s, $%.2f)\n", s.Project.Name, len(s.Phases), len(s.Steps), took.Round(time.Second), cost)
	for pi, ph := range s.Phases {
		fmt.Fprintf(out, "  Phase %d · %s\n", pi+1, ph.Title)
	}
	_, warns := Validate(s)
	for _, w := range warns {
		fmt.Fprintln(out, "warning:", w)
	}
}

// newUUID returns a random version 4 UUID.
func newUUID() string {
	var b [16]byte
	rand.Read(b[:]) // crypto/rand.Read never fails as of Go 1.24
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
