package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

// Event is one line of curriculum/progress.jsonl.
type Event struct {
	At    time.Time `json:"at"`
	Step  string    `json:"step"`
	Event string    `json:"event"`
}

// now is the clock for new events.
var now = func() time.Time { return time.Now().UTC().Truncate(time.Second) }

// readEvents reads the progress log. Lines that aren't valid JSON, such as a
// half-written last line after a crash, are skipped with a warning.
func readEvents(path string) ([]Event, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var events []Event
	var warns []string
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			warns = append(warns, fmt.Sprintf("%s line %d isn't valid JSON; ignored", progressFile, i+1))
			continue
		}
		events = append(events, ev)
	}
	return events, warns, nil
}

// Status is a step's progress, worked out from the log.
type Status int

const (
	Locked Status = iota
	Available
	InProgress
	Done
)

// Progress is the worked-out state of every step.
type Progress struct {
	Status  map[string]Status
	Current string // the most recently started step still in progress, or ""
	Done    int
	Warns   []string
}

// progressOf works out each step's status from the events. Event types it
// doesn't know are ignored, so later versions can add their own.
func progressOf(s *Spec, events []Event) Progress {
	started := map[string]time.Time{}
	completed := map[string]bool{}
	unknown := 0
	for _, ev := range events {
		if s.stepIndex(ev.Step) < 0 {
			unknown++
			continue
		}
		switch ev.Event {
		case "started":
			if t, ok := started[ev.Step]; !ok || ev.At.After(t) {
				started[ev.Step] = ev.At
			}
		case "completed":
			completed[ev.Step] = true
		}
	}
	pr := Progress{Status: map[string]Status{}}
	if unknown > 0 {
		pr.Warns = append(pr.Warns, fmt.Sprintf("%d progress events refer to steps no longer in the curriculum; ignored", unknown))
	}
	var latest time.Time
	for _, st := range s.Steps {
		t, isStarted := started[st.ID]
		switch {
		case completed[st.ID]:
			pr.Status[st.ID] = Done
			pr.Done++
		case isStarted:
			pr.Status[st.ID] = InProgress
			if pr.Current == "" || !t.Before(latest) {
				latest, pr.Current = t, st.ID
			}
		default:
			pr.Status[st.ID] = Available
			for _, pre := range st.Prerequisites {
				if !completed[pre] {
					pr.Status[st.ID] = Locked
				}
			}
		}
	}
	return pr
}

// appendEvent adds one event to the progress log and records the new hash.
func (p *Project) appendEvent(step, event string) error {
	line, err := json.Marshal(Event{At: now(), Step: step, Event: event})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p.abs(progressFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	data, err := os.ReadFile(p.abs(progressFile))
	if err != nil {
		return err
	}
	p.entry.Files[progressFile] = hashOf(data)
	return saveEntry(p.entry)
}

// progress reads the log and works out every step's status, adding any
// warnings to the project's notices.
func (p *Project) progress() (Progress, error) {
	events, warns, err := readEvents(p.abs(progressFile))
	if err != nil {
		return Progress{}, err
	}
	pr := progressOf(p.Spec, events)
	p.notices = append(p.notices, warns...)
	p.notices = append(p.notices, pr.Warns...)
	return pr, nil
}

// describeStep is "04 · Respond to PING → /path/to/curriculum/steps/04-respond-to-ping.md".
func (p *Project) describeStep(i int) string {
	st := p.Spec.Steps[i]
	return fmt.Sprintf("%s · %s → %s", num(i), st.Title, p.abs(stepFile(i, st.ID)))
}

// cmdNext prints the step in progress, or starts the next available one.
func cmdNext(p *Project, out io.Writer) error {
	pr, err := p.progress()
	if err != nil {
		return err
	}
	if pr.Current != "" {
		fmt.Fprintf(out, "In progress: %s\n", p.describeStep(p.Spec.stepIndex(pr.Current)))
		return nil
	}
	for i, st := range p.Spec.Steps {
		if pr.Status[st.ID] != Available {
			continue
		}
		if st.Detail == nil {
			fmt.Fprintf(out, "Next is step %s · %s, but it isn't expanded yet. Run `tattva expand` first.\n", num(i), st.Title)
			return nil
		}
		if err := p.appendEvent(st.ID, "started"); err != nil {
			return err
		}
		fmt.Fprintf(out, "Started: %s\n", p.describeStep(i))
		return nil
	}
	fmt.Fprintf(out, "All %d steps are done.\n", len(p.Spec.Steps))
	return nil
}

// cmdDone marks the current step, or the one arg names, as completed. arg is
// a step number if it's all digits, otherwise a step id.
func cmdDone(p *Project, arg string, out io.Writer) error {
	pr, err := p.progress()
	if err != nil {
		return err
	}
	i := -1
	switch {
	case arg == "" && pr.Current == "":
		return errors.New("no step is in progress; pass a step number or id, such as `tattva done 3`")
	case arg == "":
		i = p.Spec.stepIndex(pr.Current)
	default:
		if n, err := strconv.Atoi(arg); err == nil {
			if n >= 1 && n <= len(p.Spec.Steps) {
				i = n - 1
			}
		} else {
			i = p.Spec.stepIndex(arg)
		}
	}
	if i < 0 {
		return fmt.Errorf("no step %q in this curriculum (steps are 1-%d)", arg, len(p.Spec.Steps))
	}
	st := p.Spec.Steps[i]
	if pr.Status[st.ID] == Done {
		fmt.Fprintf(out, "Step %s · %s is already done.\n", num(i), st.Title)
		return nil
	}
	var missing []string
	for _, pre := range st.Prerequisites {
		if pr.Status[pre] != Done {
			missing = append(missing, num(p.Spec.stepIndex(pre)))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(out, "Note: prerequisite steps %s aren't done yet.\n", strings.Join(missing, ", "))
	}
	if err := p.appendEvent(st.ID, "completed"); err != nil {
		return err
	}
	total := len(p.Spec.Steps)
	fmt.Fprintf(out, "Done: %s · %s (%d/%d steps, %d%%). Run `tattva next` for the next step.\n",
		num(i), st.Title, pr.Done+1, total, (pr.Done+1)*100/total)
	return nil
}

var marks = map[Status]string{Done: "✓", InProgress: "→", Available: "○", Locked: "·"}

// cmdStatus prints the curriculum with each step's progress.
func cmdStatus(p *Project, out io.Writer) error {
	pr, err := p.progress()
	if err != nil {
		return err
	}
	s := p.Spec
	fmt.Fprintf(out, "%s: %s in %s · %d/%d steps done (%d%%)\n",
		s.Project.Name, s.Project.Target, s.Project.Language, pr.Done, len(s.Steps), pr.Done*100/len(s.Steps))
	for pi, ph := range s.Phases {
		fmt.Fprintf(out, "\nPhase %d · %s\n", pi+1, ph.Title)
		for i, st := range s.Steps {
			if st.Phase != ph.ID {
				continue
			}
			note := ""
			if st.Detail == nil {
				note = "  (not expanded)"
			}
			fmt.Fprintf(out, "  %s %s %s%s\n", marks[pr.Status[st.ID]], num(i), st.Title, note)
		}
	}
	if _, warns := Validate(s); len(warns) > 0 {
		fmt.Fprintln(out)
		for _, w := range warns {
			fmt.Fprintln(out, "warning:", w)
		}
	}
	for _, rel := range p.edited {
		fmt.Fprintln(out, "edited outside tattva (not overwritten):", rel)
	}
	return nil
}
