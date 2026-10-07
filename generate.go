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
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed prompts/design.md
var designPrompt string

const repairPrompt = `You repair JSON answers that failed validation. You receive the original request, the answer that was given, and the rules the answer breaks. Return the corrected answer, matching the same schema, and change only what is needed to satisfy the rules. If a requested item is missing, write it from the request. If a step introduces too many new concepts, split it into smaller steps and update the prerequisites that pointed at it.`

// claude runs one call. It's a var so tests can swap in a fake.
var claude = runClaude

// invalidOutput is output that still broke the rules after one repair.
type invalidOutput struct {
	Output json.RawMessage
	Errors []string
}

func (e *invalidOutput) Error() string {
	return "generated output broke the curriculum rules even after a repair:\n  " + strings.Join(e.Errors, "\n  ")
}

// generate makes a call, checks the output, and makes at most one repair
// call. It returns the accepted output, the total cost, and the rules the
// first draft broke when a repair was needed.
func generate(ctx context.Context, c call, check func(json.RawMessage) []string) (json.RawMessage, float64, []string, error) {
	out, cost, err := runGeneration(ctx, c)
	if err != nil {
		return nil, cost, nil, err
	}
	errs := check(out)
	if len(errs) == 0 {
		c.Log.Printf("[%s] rules: ok", c.Label)
		return out, cost, nil, nil
	}
	rules := "rules"
	if len(errs) == 1 {
		rules = "rule"
	}
	c.Log.Show("  [%s] the first draft broke %d %s; asking the selected provider to repair it", c.Label, len(errs), rules)
	for _, e := range errs {
		c.Log.Printf("[%s]   broke: %s", c.Label, e)
	}
	prompt := "The answer below breaks these rules:\n- " + strings.Join(errs, "\n- ") +
		"\n\nOriginal request:\n" + c.Prompt + "\n\nAnswer:\n" + string(out)
	r := call{Provider: c.Provider, System: repairPrompt, Prompt: prompt, Schema: c.Schema, Model: c.Model, Label: c.Label + " repair", Log: c.Log}
	fixed, more, err := runGeneration(ctx, r)
	cost += more
	if err != nil {
		return nil, cost, errs, err
	}
	if still := check(fixed); len(still) > 0 {
		return nil, cost, errs, &invalidOutput{Output: fixed, Errors: still}
	}
	c.Log.Printf("[%s] rules: ok", r.Label)
	return fixed, cost, errs, nil
}

// showRepairs tells the user which rules a first draft broke before its
// repair.
func showRepairs(lg *runLog, broken []string) {
	if len(broken) > 0 {
		lg.Show("  repaired after the first draft broke these rules:\n    - %s", strings.Join(broken, "\n    - "))
	}
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

// specFromOutline turns an outline from the selected provider into a spec with tattva's own
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

// designOutline runs a design or revise call, c with its prompt, model and
// log, and returns a valid spec.
func designOutline(ctx context.Context, c call, id, target, lang string) (*Spec, float64, []string, error) {
	check := func(out json.RawMessage) []string {
		_, errs := specFromOutline(out, id, target, lang)
		return errs
	}
	c.System, c.Schema, c.Web = designPrompt, outlineSchema, true
	out, cost, broken, err := generate(ctx, c, check)
	if err != nil {
		return nil, cost, broken, err
	}
	s, _ := specFromOutline(out, id, target, lang)
	return s, cost, broken, nil
}

// cmdNew designs a new curriculum in dir.
func cmdNew(ctx context.Context, dir, target, lang, model, provider string, out io.Writer) (err error) {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(specFile))); err == nil {
		return fmt.Errorf("%s already exists here; use `tattva revise` to change it", specFile)
	}
	start := time.Now()
	fmt.Fprintln(out, "Designing the curriculum. This usually takes a few minutes…")
	lg := openLog("new", out)
	defer func() { lg.Close(err) }()
	lg.Printf("tattva new %q --lang %s --provider %s in %s", target, lang, provider, dir)
	prompt := fmt.Sprintf("Target: %s\nLanguage: %s\n", target, lang)
	s, cost, broken, err := designOutline(ctx, call{Provider: provider, Prompt: prompt, Model: model, Label: "design", Log: lg}, newUUID(), target, lang)
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
	summarize(p, time.Since(start), cost, broken, lg)
	fmt.Fprintln(out, "Review curriculum/README.md, then run `tattva revise \"<feedback>\"` or `tattva expand`.")
	return nil
}

// cmdRevise rewrites the outline from the current one plus feedback.
func cmdRevise(ctx context.Context, p *Project, feedback string, force bool, model, provider string, out io.Writer) (err error) {
	expanded := false
	for _, st := range p.Spec.Steps {
		expanded = expanded || st.Detail != nil
	}
	if expanded && !force {
		return errors.New("some steps are already expanded; `tattva revise --force` drops all step details and deletes every step file")
	}
	start := time.Now()
	fmt.Fprintln(out, "Revising the curriculum. This usually takes a few minutes…")
	lg := openLog("revise", out)
	defer func() { lg.Close(err) }()
	lg.Printf("tattva revise %q --provider %s in %s", feedback, provider, p.Root)
	info := p.Spec.Project
	prompt := fmt.Sprintf("Target: %s\nLanguage: %s\n\nCurrent outline:\n%s\nRevise the outline according to this feedback:\n%s\n",
		info.Target, info.Language, outlineOf(p.Spec), feedback)
	s, cost, broken, err := designOutline(ctx, call{Provider: provider, Prompt: prompt, Model: model, Label: "revise", Log: lg}, info.ID, info.Target, info.Language)
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
	summarize(p, time.Since(start), cost, broken, lg)
	return nil
}

// summarize shows what a design or revise call produced.
func summarize(p *Project, took time.Duration, cost float64, broken []string, lg *runLog) {
	s := p.Spec
	lg.Show("%s: %d phases, %d steps (%s, %s)", s.Project.Name, len(s.Phases), len(s.Steps), took.Round(time.Second), costLabel(cost))
	showRepairs(lg, broken)
	for pi, ph := range s.Phases {
		lg.Show("  Phase %d · %s", pi+1, ph.Title)
	}
	_, warns := Validate(s)
	for _, w := range warns {
		lg.Show("warning: %s", w)
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

//go:embed prompts/expand.md
var expandPrompt string

// expandConcurrency is how many phases expand at once.
const expandConcurrency = 3 // ponytail: fixed; lower it if calls start hitting rate limits

// phaseErrors checks one expand call's output (rules 10 to 12): exactly the
// requested steps, each with a valid detail.
func phaseErrors(kind string, out json.RawMessage, want []string) []string {
	var pd PhaseDetails
	if err := json.Unmarshal(out, &pd); err != nil {
		return []string{"output isn't valid phase details: " + err.Error()}
	}
	var errs, got []string
	for _, sd := range pd.Steps {
		got = append(got, sd.ID)
		d := sd.Detail
		e, _ := detailErrors(kind, &d)
		for _, msg := range e {
			errs = append(errs, fmt.Sprintf("step %q: %s", sd.ID, msg))
		}
	}
	if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
		errs = append(errs, fmt.Sprintf("returned steps [%s], want exactly [%s]", strings.Join(got, " "), strings.Join(want, " ")))
	}
	return errs
}

// mergePhase stores one phase's details, saves the spec and renders the new
// step files. The caller holds the expand mutex.
func (p *Project) mergePhase(out json.RawMessage) error {
	var pd PhaseDetails
	if err := json.Unmarshal(out, &pd); err != nil {
		return err
	}
	for _, sd := range pd.Steps {
		d := sd.Detail
		p.Spec.Steps[p.Spec.stepIndex(sd.ID)].Detail = &d
	}
	if err := p.saveSpec(); err != nil {
		return err
	}
	return p.render()
}

// cmdExpand fills in every unexpanded step, with one call per phase and a few
// phases at a time. Each phase is saved as soon as it finishes.
func cmdExpand(ctx context.Context, p *Project, model, provider string, out io.Writer) (err error) {
	type job struct {
		phase string
		ids   []string
	}
	var jobs []job
	for _, ph := range p.Spec.Phases {
		var ids []string
		for _, st := range p.Spec.Steps {
			if st.Phase == ph.ID && st.Detail == nil {
				ids = append(ids, st.ID)
			}
		}
		if len(ids) > 0 {
			jobs = append(jobs, job{ph.ID, ids})
		}
	}
	if len(jobs) == 0 {
		fmt.Fprintln(out, "Every step is already expanded.")
		return nil
	}

	start := time.Now()
	fmt.Fprintf(out, "Expanding %d phases, %d at a time. Each takes a few minutes…\n", len(jobs), expandConcurrency)
	lg := openLog("expand", out)
	defer func() { lg.Close(err) }()
	lg.Printf("tattva expand --provider %s in %s", provider, p.Root)
	outline, lang, kind := outlineOf(p.Spec), p.Spec.Project.Language, p.Spec.Program.Kind
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		sem    = make(chan struct{}, expandConcurrency)
		total  float64
		done   int
		failed []string
	)
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			began := time.Now()
			prompt := fmt.Sprintf("Language: %s\n\nFull outline:\n%s\nPhase to expand: %s\nFill in detail for exactly these step ids, in this order: %s\n",
				lang, outline, j.phase, strings.Join(j.ids, ", "))
			c := call{Provider: provider, System: expandPrompt, Prompt: prompt, Schema: phaseSchema, Web: true, Model: model, Label: "expand " + j.phase, Log: lg}
			res, cost, broken, err := generate(ctx, c, func(o json.RawMessage) []string { return phaseErrors(kind, o, j.ids) })

			mu.Lock()
			defer mu.Unlock()
			total += cost
			done++
			if err == nil {
				err = p.mergePhase(res)
			}
			if err != nil {
				failed = append(failed, j.phase)
				lg.Show("✗ phase %s failed (%d/%d): %v", j.phase, done, len(jobs), withSavedOutput(p.Root, "expand-"+j.phase, err))
				return
			}
			lg.Show("✓ phase %s expanded (%d/%d, %s, %s)", j.phase, done, len(jobs), time.Since(began).Round(time.Second), costLabel(cost))
			showRepairs(lg, broken)
		}()
	}
	wg.Wait()
	lg.Show("Finished in %s, %s in total.", time.Since(start).Round(time.Second), costLabel(total))
	_, warns := Validate(p.Spec)
	for _, w := range warns {
		lg.Show("warning: %s", w)
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d phases failed (%s); run `tattva expand` again to retry them", len(failed), strings.Join(failed, ", "))
	}
	return nil
}
