package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// cmdServe runs the workspace on 127.0.0.1:port until ctx is cancelled.
func cmdServe(ctx context.Context, port int, out io.Writer) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("can't listen on 127.0.0.1:%d: %v (pick another port with `tattva serve --port <n>`)", port, err)
	}
	addr := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Fprintf(out, "tattva workspace: %s (Ctrl-C to stop)\n", addr)
	openBrowser(addr)
	srv := &http.Server{Handler: newServer(port), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// openBrowser opens url in the default browser when it can. It's a var so
// tests don't open windows.
var openBrowser = func(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, url).Start()
}

// newServer returns the workspace's handler for a server on port.
func newServer(port int) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFiles)))
	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /p/{id}", handleProject)
	mux.HandleFunc("GET /p/{id}/s/{n}", handleProject)
	return onlyLocal(port, secure(http.NewCrossOriginProtection().Handler(mux)))
}

// onlyLocal answers only requests addressed to 127.0.0.1 or localhost on our
// port, so a web page can't reach the workspace through DNS rebinding.
func onlyLocal(port int, next http.Handler) http.Handler {
	allowed := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", port): true,
		fmt.Sprintf("localhost:%d", port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "the tattva workspace only answers on 127.0.0.1 and localhost", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// secure adds headers that stop Claude's content from running scripts or
// the pages from being framed.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// pageView is what every page template receives.
type pageView struct {
	Projects []projectView
	Current  *projectView  // the project being shown; nil on the landing page
	Phases   []phaseNav    // project page: the steps pane
	Overview *overviewPage // project page showing the overview
	Step     *stepPage     // project page showing a step
	Problem  string        // project page for a project that can't be shown
}

// IsCurrent reports whether id is the project being shown.
func (v pageView) IsCurrent(id string) bool { return v.Current != nil && v.Current.ID == id }

var pageFuncs = template.FuncMap{
	"markdown":    markdown,
	"join":        strings.Join,
	"hintNote":    hintNote,
	"progressBar": progressBar,
	"themes":      func() []string { return themes },
	"safeURL":     safeURL,
}

// page parses the page frame together with one page's template.
func page(name string) *template.Template {
	return template.Must(template.New("base.html").Funcs(pageFuncs).
		ParseFS(webFS, "web/templates/base.html", "web/templates/"+name))
}

var homePage = page("home.html")
var projectPage = page("project.html")

// show renders a page, or a plain error if the template fails.
func show(w http.ResponseWriter, t *template.Template, v pageView) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	projects, _ := loadProjects()
	show(w, homePage, pageView{Projects: projects})
}

// slug turns "Mini Redis" into "mini-redis", for the window list.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			dash = true
			continue
		}
		if dash && b.Len() > 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
		dash = false
	}
	return b.String()
}

// progressBar is a 12-cell text bar such as "███████░░░░░".
func progressBar(done, total int) string {
	const cells = 12
	full := 0
	if total > 0 {
		full = done * cells / total
	}
	return strings.Repeat("█", full) + strings.Repeat("░", cells-full)
}

// phaseNav is one phase in the steps pane.
type phaseNav struct {
	Title string
	Steps []stepNav
}

// stepNav is one step in the steps pane.
type stepNav struct {
	N                  int
	Num, Title         string
	Status, Mark       string
	Hints              string // "2 hints", or ""
	Expanded, Selected bool
}

// overviewPage is the curriculum README view.
type overviewPage struct {
	*Spec
	Contract string
}

// stepPage is the step view: what the step's markdown file shows, plus the
// workspace's buttons and hint state.
type stepPage struct {
	stepView
	File       string // "08-key-expiry.md", the pane title
	Base       string // "/p/<id>/s/8", where the buttons post
	Status     string // done, in-progress, available or locked
	Missing    string // prerequisites that aren't done, by number
	Reveal     []hintItem
	References []Reference
}

// hintItem is one hint and whether it's open.
type hintItem struct {
	Level          int
	Label, Text    string
	Revealed, Next bool // Next: the one hint that can be opened now
}

var statusNames = map[Status]string{Done: "done", InProgress: "in-progress", Available: "available", Locked: "locked"}

// handleProject shows a project: its overview, or step n.
func handleProject(w http.ResponseWriter, r *http.Request) {
	projects, _ := loadProjects()
	i := slices.IndexFunc(projects, func(p projectView) bool { return p.ID == r.PathValue("id") })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	cur := &projects[i]
	v := pageView{Projects: projects, Current: cur}
	if cur.Problem != "" {
		v.Problem = cur.Problem
		show(w, projectPage, v)
		return
	}
	s := cur.spec
	pr := progressOf(s, cur.events)
	selected := -1
	if n := r.PathValue("n"); n != "" {
		k, err := strconv.Atoi(n)
		if err != nil || k < 1 || k > len(s.Steps) || s.Steps[k-1].Detail == nil {
			http.NotFound(w, r)
			return
		}
		selected = k - 1
		v.Step = newStepPage(cur.ID, s, pr, selected)
	} else {
		v.Overview = &overviewPage{Spec: s, Contract: contract(s.Program)}
	}
	v.Phases = phaseNavs(s, pr, selected)
	show(w, projectPage, v)
}

// phaseNavs builds the steps pane: each phase with its steps and progress.
func phaseNavs(s *Spec, pr Progress, selected int) []phaseNav {
	var navs []phaseNav
	for _, ph := range s.Phases {
		n := phaseNav{Title: ph.Title}
		for i, st := range s.Steps {
			if st.Phase != ph.ID {
				continue
			}
			n.Steps = append(n.Steps, stepNav{
				N: i + 1, Num: num(i), Title: st.Title,
				Status: statusNames[pr.Status[st.ID]], Mark: marks[pr.Status[st.ID]],
				Hints: hintNote(pr.Hints[st.ID]), Expanded: st.Detail != nil, Selected: i == selected,
			})
		}
		navs = append(navs, n)
	}
	return navs
}

// newStepPage is the step view for step i of project id.
func newStepPage(id string, s *Spec, pr Progress, i int) *stepPage {
	st := s.Steps[i]
	v := &stepPage{
		stepView:   stepViewOf(s, i),
		File:       path.Base(stepFile(i, st.ID)),
		Base:       fmt.Sprintf("/p/%s/s/%d", id, i+1),
		Status:     statusNames[pr.Status[st.ID]],
		References: s.References,
	}
	var missing []string
	for _, pre := range st.Prerequisites {
		if pr.Status[pre] != Done {
			missing = append(missing, num(s.stepIndex(pre)))
		}
	}
	v.Missing = strings.Join(missing, ", ")
	opened := pr.Hints[st.ID]
	for hi, h := range v.Hints {
		level := hi + 1
		v.Reveal = append(v.Reveal, hintItem{Level: level, Label: h.Label, Text: h.Text,
			Revealed: level <= opened, Next: level == opened+1})
	}
	return v
}

// safeURL reports whether u is an http or https link, the only kind the
// workspace turns into a link.
func safeURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https")
}
