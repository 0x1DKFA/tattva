package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
