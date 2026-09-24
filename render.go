package main

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("").
	Funcs(template.FuncMap{"join": strings.Join}).
	ParseFS(templateFS, "templates/*.tmpl"))

type readmeView struct {
	*Spec
	Contract  string
	PhaseList []phaseView
}

type phaseView struct {
	Num            int
	Title, Purpose string
	Steps          []stepLine
}

type stepLine struct {
	Num, Title, Goal, After string
	Expanded                bool
}

type stepView struct {
	Num        string
	Step       Step
	PhaseNum   int
	PhaseTitle string
	After      string
	Checks     []string
	Hints      []hintView
	Unlocks    []string
}

type hintView struct{ Label, Text string }

// renderReadme returns curriculum/README.md for s.
func renderReadme(s *Spec) []byte {
	v := readmeView{Spec: s, Contract: contract(s.Program)}
	for pi, ph := range s.Phases {
		pv := phaseView{Num: pi + 1, Title: ph.Title, Purpose: ph.Purpose}
		for i, st := range s.Steps {
			if st.Phase == ph.ID {
				pv.Steps = append(pv.Steps, stepLine{Num: num(i), Title: st.Title, Goal: st.Goal,
					After: s.prereqNums(st), Expanded: st.Detail != nil})
			}
		}
		v.PhaseList = append(v.PhaseList, pv)
	}
	return execute("readme.md.tmpl", v)
}

// renderStep returns the markdown file for step i, which must be expanded.
func renderStep(s *Spec, i int) []byte {
	st := s.Steps[i]
	v := stepView{Num: num(i), Step: st, After: s.prereqNums(st)}
	for pi, ph := range s.Phases {
		if ph.ID == st.Phase {
			v.PhaseNum, v.PhaseTitle = pi+1, ph.Title
		}
	}
	for _, c := range st.Detail.Checks {
		v.Checks = append(v.Checks, describeCheck(c))
	}
	for hi, h := range st.Detail.Hints {
		v.Hints = append(v.Hints, hintView{Label: hintLabel(hi), Text: h})
	}
	for j, other := range s.Steps {
		for _, pre := range other.Prerequisites {
			if pre == st.ID {
				v.Unlocks = append(v.Unlocks, num(j)+" · "+other.Title)
			}
		}
	}
	return execute("step.md.tmpl", v)
}

func execute(name string, data any) []byte {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		panic(err) // the templates are fixed, so an error is a bug the tests catch
	}
	return buf.Bytes()
}

// num is a step's two-digit number from its position: 0 → "01".
func num(i int) string { return fmt.Sprintf("%02d", i+1) }

// prereqNums lists a step's prerequisites by number, such as "01, 03".
func (s *Spec) prereqNums(st Step) string {
	var nums []string
	for _, pre := range st.Prerequisites {
		if i := s.stepIndex(pre); i >= 0 {
			nums = append(nums, num(i))
		}
	}
	return strings.Join(nums, ", ")
}

func hintLabel(i int) string {
	labels := []string{"nudge", "specific", "pseudocode"}
	if i < len(labels) {
		return fmt.Sprintf("Hint %d (%s)", i+1, labels[i])
	}
	return fmt.Sprintf("Hint %d", i+1)
}

// contract explains how the checks run the learner's program.
func contract(p Program) string {
	if p.Kind == "server" {
		return fmt.Sprintf("Create an executable `run.sh` at the project root that builds your server if needed and starts it. "+
			"Checks start `./run.sh` with no arguments, wait for port %d, and talk to it over TCP.", p.Port)
	}
	return "Create an executable `run.sh` at the project root that builds your program if needed and runs it, passing its arguments through. " +
		"Checks run `./run.sh` with arguments in a scratch directory and read its output and the files it writes."
}

// describeCheck is a one-line, human-readable version of a check.
func describeCheck(c Check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**: ", c.Name)
	switch c.Do {
	case "exec":
		fmt.Fprintf(&b, "run `%s`", strings.TrimSpace("./run.sh "+strings.Join(c.Args, " ")))
		if c.Input != "" {
			fmt.Fprintf(&b, " with stdin `%q`", c.Input)
		}
		fmt.Fprintf(&b, ", expect exit code %d", c.ExitCode)
		if c.Expect != "" {
			fmt.Fprintf(&b, " and stdout `%q` (%s)", c.Expect, c.Match)
		}
	case "tcp":
		fmt.Fprintf(&b, "send `%q`", c.Input)
		if c.Expect != "" {
			fmt.Fprintf(&b, ", expect reply `%q` (%s)", c.Expect, c.Match)
		}
	case "write":
		fmt.Fprintf(&b, "write `%q` to `%s`", c.Input, c.Path)
	case "file":
		fmt.Fprintf(&b, "`%s` exists", c.Path)
		if c.Expect != "" {
			fmt.Fprintf(&b, " with content `%q` (%s)", c.Expect, c.Match)
		}
	}
	return b.String()
}
