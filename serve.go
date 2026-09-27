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
	"os/exec"
	"runtime"
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
	Current  *projectView // the project being shown; nil on the landing page
}

// IsCurrent reports whether id is the project being shown.
func (v pageView) IsCurrent(id string) bool { return v.Current != nil && v.Current.ID == id }

var pageFuncs = template.FuncMap{
	"markdown":    markdown,
	"join":        strings.Join,
	"hintNote":    hintNote,
	"progressBar": progressBar,
	"themes":      func() []string { return themes },
}

// page parses the page frame together with one page's template.
func page(name string) *template.Template {
	return template.Must(template.New("base.html").Funcs(pageFuncs).
		ParseFS(webFS, "web/templates/base.html", "web/templates/"+name))
}

var homePage = page("home.html")

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
