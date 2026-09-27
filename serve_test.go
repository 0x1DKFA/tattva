package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// request sends a request to the workspace the way a browser on this machine would.
func request(t *testing.T, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	req.Host = "127.0.0.1:4747"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	newServer(4747).ServeHTTP(rec, req)
	return rec
}

// noBrowser stops cmdServe from opening a browser window during a test.
func noBrowser(t *testing.T) {
	t.Helper()
	old := openBrowser
	openBrowser = func(string) {}
	t.Cleanup(func() { openBrowser = old })
}

func TestServerOnlyAnswersLocalHostNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for host, want := range map[string]int{
		"127.0.0.1:4747": 200, "localhost:4747": 200, "evil.example:4747": 403, "127.0.0.1:9999": 403,
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		newServer(4747).ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %s: status %d, want %d", host, rec.Code, want)
		}
	}
}

func TestServerRejectsCrossSitePosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := httptest.NewRequest("POST", "/p/x/s/1/start", nil)
	req.Host = "127.0.0.1:4747"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	newServer(4747).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestServerSetsSecurityHeaders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rec := request(t, "GET", "/", nil)
	want := "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if got := rec.Header().Get("Content-Security-Policy"); got != want {
		t.Errorf("CSP = %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("missing nosniff or no-referrer")
	}
}

func TestServerServesStaticFiles(t *testing.T) {
	for path, typ := range map[string]string{
		"/static/app.css": "text/css", "/static/app.js": "javascript", "/static/theme-boot.js": "javascript",
	} {
		rec := request(t, "GET", path, nil)
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), typ) {
			t.Errorf("%s: status %d, type %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestHomeShowsProjectCards(t *testing.T) {
	p := newTestProject(t)
	if err := cmdNext(p, io.Discard); err != nil {
		t.Fatal(err)
	}
	gone := t.TempDir() + "/gone"
	if err := saveEntry(Entry{ID: "gone", Path: gone, Name: "Gone Project", Files: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	body := request(t, "GET", "/", nil).Body.String()
	for _, want := range []string{
		`href="/p/` + p.Spec.Project.ID + `"`,
		"2:mini-redis", "Mini Redis", "Redis · go", "[░░░░░░░░░░░░]", "0/3", "→ 01 Create run.sh",
		`<section class="pane card missing">`, "Gone Project", "missing: " + gone,
		`<option value="catppuccin">catppuccin</option>`,
		`data-themes="catppuccin tokyo-night gruvbox"`, `data-theme="catppuccin"`,
		`<script src="/static/theme-boot.js"></script>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page missing %q", want)
		}
	}
	if n := strings.Count(body, `href="/p/gone"`); n != 1 {
		t.Errorf("the missing project should only appear in the window list, found %d links", n)
	}
}

func TestHomeWithNoProjects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if body := request(t, "GET", "/", nil).Body.String(); !strings.Contains(body, "No projects yet") {
		t.Fatalf("body = %s", body)
	}
}

func TestServeReportsABusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	err = cmdServe(context.Background(), ln.Addr().(*net.TCPAddr).Port, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--port") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeStopsWhenCancelled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	noBrowser(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cmdServe(ctx, port, io.Discard) }()
	var resp *http.Response
	for range 100 {
		if resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port)); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve didn't stop after cancel")
	}
}

func TestProjectOverview(t *testing.T) {
	p := newTestProject(t)
	body := request(t, "GET", "/p/"+p.Spec.Project.ID, nil).Body.String()
	for _, want := range []string{
		`<h2 class="pane-title">README.md</h2>`, `class="nav-item selected"`, "▸ overview",
		"<h1>Mini Redis</h1>", "not needed to learn the protocol", "Redis is a single-threaded event-loop server.",
		`<pre class="mermaid" data-source="flowchart LR`,
		`<a href="https://redis.io/docs/latest/develop/reference/protocol-spec/"`,
		`<script src="/static/mermaid.min.js"></script>`,
		"1:mini-redis*", "0/3 ✓",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview missing %q", want)
		}
	}
}

func TestProjectStepPage(t *testing.T) {
	p := newTestProject(t)
	body := request(t, "GET", "/p/"+p.Spec.Project.ID+"/s/1", nil).Body.String()
	for _, want := range []string{
		`<h2 class="pane-title">01-create-run-script.md</h2>`, "01 · Create run.sh", "phase 1: Setup",
		"[ start step ]", "[ mark done ]", "[ show hint 1 ]", "opens after the hint above",
		"machine checks (run by the verifier)", "<strong>accepts a connection</strong>",
		"Forgetting to make run.sh executable.", "What happens if two programs listen on the same port?",
		"02 · Respond to PING",
		`class="nav-item status-available selected"`, `<span class="nav-item status-locked unexpanded"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("step page missing %q", want)
		}
	}
	if strings.Contains(body, "Servers listen, then accept.") {
		t.Error("hint 1 must stay closed until it's opened")
	}
}

func TestProjectShowsOpenedHints(t *testing.T) {
	p := newTestProject(t)
	if err := p.appendEvent(Event{Step: "create-run-script", Event: "hint", Level: 1}); err != nil {
		t.Fatal(err)
	}
	body := request(t, "GET", "/p/"+p.Spec.Project.ID+"/s/1", nil).Body.String()
	if !strings.Contains(body, "Servers listen, then accept.") || !strings.Contains(body, "[ show hint 2 ]") || strings.Contains(body, "[ show hint 1 ]") {
		t.Fatalf("hint state wrong:\n%s", body)
	}
	if !strings.Contains(body, `<span class="hint-count">1 hint</span>`) {
		t.Error("the steps pane should show the hint count")
	}
}

func TestProjectEscapesClaudeContent(t *testing.T) {
	p := newTestProject(t)
	p.Spec.Steps[0].Title = `<img src=x onerror=alert(1)>`
	p.Spec.Steps[0].Detail.Task = "Build it. <script>alert(1)</script>"
	p.Spec.References = append(p.Spec.References, Reference{Title: "evil", URL: "javascript:alert(1)"})
	if err := p.saveSpec(); err != nil {
		t.Fatal(err)
	}
	body := request(t, "GET", "/p/"+p.Spec.Project.ID+"/s/1", nil).Body.String()
	for _, bad := range []string{"<script>alert", "<img src=x", `href="javascript:`} {
		if strings.Contains(body, bad) {
			t.Errorf("unsafe %q reached the page", bad)
		}
	}
}

func TestProjectNotFound(t *testing.T) {
	p := newTestProject(t)
	id := p.Spec.Project.ID
	for _, path := range []string{"/p/nope", "/p/" + id + "/s/2", "/p/" + id + "/s/9", "/p/" + id + "/s/x"} {
		if code := request(t, "GET", path, nil).Code; code != 404 {
			t.Errorf("%s: status %d, want 404", path, code)
		}
	}
}

func TestProjectWithBrokenSpecShowsTheError(t *testing.T) {
	p := newTestProject(t)
	if err := os.WriteFile(p.abs(specFile), []byte(`{"schema_version": 1,`), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := request(t, "GET", "/p/"+p.Spec.Project.ID, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "can&#39;t read its spec") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
}

func TestActionsStartAndDone(t *testing.T) {
	p := newTestProject(t)
	base := "/p/" + p.Spec.Project.ID + "/s/1"
	for _, action := range []string{"start", "start", "done", "done"} {
		rec := request(t, "POST", base+"/"+action, url.Values{})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != base {
			t.Fatalf("%s: status %d, location %q", action, rec.Code, rec.Header().Get("Location"))
		}
	}
	events, _, err := readEvents(p.abs(progressFile))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, ev.Event)
	}
	if strings.Join(kinds, ",") != "started,completed" {
		t.Fatalf("events = %v; repeated clicks must add nothing", kinds)
	}
}

func TestActionsOpenHintsInOrder(t *testing.T) {
	p := newTestProject(t)
	base := "/p/" + p.Spec.Project.ID + "/s/1"
	for _, level := range []string{"2", "1", "1", "3", "2", "3", "4", "x"} {
		rec := request(t, "POST", base+"/hint", url.Values{"level": {level}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != base+"#hints" {
			t.Fatalf("hint %s: status %d, location %q", level, rec.Code, rec.Header().Get("Location"))
		}
	}
	events, _, _ := readEvents(p.abs(progressFile))
	var levels []int
	for _, ev := range events {
		levels = append(levels, ev.Level)
	}
	if fmt.Sprint(levels) != "[1 2 3]" {
		t.Fatalf("hint levels recorded = %v, want [1 2 3]", levels)
	}
}

func TestActionsRejectUnknownTargets(t *testing.T) {
	p := newTestProject(t)
	id := p.Spec.Project.ID
	for _, path := range []string{"/p/nope/s/1/start", "/p/" + id + "/s/2/start", "/p/" + id + "/s/9/done", "/p/" + id + "/s/1/explode"} {
		if code := request(t, "POST", path, url.Values{}).Code; code != 404 {
			t.Errorf("%s: status %d, want 404", path, code)
		}
	}
	if _, err := os.Stat(p.abs(progressFile)); err == nil {
		t.Error("a rejected action must not write progress")
	}
}

func TestViewingPagesWritesNothing(t *testing.T) {
	p := newTestProject(t)
	if err := cmdNext(p, io.Discard); err != nil {
		t.Fatal(err)
	}
	reg, err := registryPath()
	if err != nil {
		t.Fatal(err)
	}
	files := []string{reg, p.abs(specFile), p.abs(progressFile), p.abs(readmeFile)}
	before := map[string]string{}
	for _, f := range files {
		before[f] = readFile(t, f)
	}
	for _, path := range []string{"/", "/p/" + p.Spec.Project.ID, "/p/" + p.Spec.Project.ID + "/s/1", "/static/app.css"} {
		if code := request(t, "GET", path, nil).Code; code != 200 {
			t.Fatalf("%s: status %d", path, code)
		}
	}
	for _, f := range files {
		if readFile(t, f) != before[f] {
			t.Errorf("viewing pages changed %s", f)
		}
	}
}

func TestActionsAgreeWithTheCLI(t *testing.T) {
	p := newTestProject(t)
	base := "/p/" + p.Spec.Project.ID + "/s/1"
	request(t, "POST", base+"/hint", url.Values{"level": {"1"}})
	request(t, "POST", base+"/done", url.Values{})
	q, err := openProject(p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if containsAny(q.notices, "edited outside tattva") {
		t.Fatalf("the workspace's writes must not look like outside edits: %q", q.notices)
	}
	var out bytes.Buffer
	if err := cmdStatus(q, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "✓ 01 Create run.sh (1 hint)") {
		t.Fatalf("status = %s", out.String())
	}
}
