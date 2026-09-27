# Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tattva serve`, a local web workspace for reading curricula and tracking progress, over the same files the CLI uses. It has a terminal look (Hyprland-style tiled panes and a tmux-style status bar) and switchable colour themes.

**Architecture:**
- **Server:** Go `net/http` plus `html/template`, in the existing single `main` package, with markdown rendered by goldmark.
- **Browser files:** a hand-written stylesheet whose colours all come from CSS variables, two small scripts, and a bundled copy of mermaid.js. These live in `web/` and are built into the binary with `embed`.
- **Data:**
  - Page views re-read `spec.json`, `progress.jsonl` and the registry on every request, and never write.
  - The three buttons are form posts that add progress events through the same project code as the CLI.

**Tech Stack:** Go 1.25 (`net/http`, `html/template`, `embed`), `github.com/yuin/goldmark` (the first third-party module), mermaid 11 (vendored, MIT), no JS framework, no build step.

**Spec:** `docs/superpowers/specs/2026-09-27-workspace-design.md`. It builds on `docs/superpowers/specs/2026-09-24-curriculum-engine-design.md`. Read both.

## Global Constraints

- **Branch and commits:** work on branch `workspace`, which already exists, is checked out, and holds the spec. One commit per task with a conventional prefix. **No `Co-Authored-By` or any other Claude attribution in commits** (the user's instruction).
- **Before each commit:** run `gofmt -w .`; `go vet ./...` and `go test -race ./...` must pass.
- **Dependencies:** Go 1.25, module `github.com/0x1DKFA/tattva`. The only new module allowed is `github.com/yuin/goldmark`. No JavaScript framework, no bundler, no npm.
- **Tests stay offline:** `TestMain` already limits `PATH` to `/usr/bin:/bin`, and every test that touches the registry sets `HOME` to a temp dir. Any test that runs `cmdServe` replaces `openBrowser` with a no-op, so no browser window opens.
- **Colours:** every colour in `web/static/app.css` comes from a CSS variable set in the theme blocks between `/* === themes === */` and `/* === end themes === */`. Tests in Task 3 enforce this.
- **Exact values:**
  - default port 4747; the server listens on `127.0.0.1` only
  - allowed `Host` values: `127.0.0.1:<port>` and `localhost:<port>`
  - themes, in this order: `catppuccin`, `tokyo-night`, `gruvbox` (the first is the default); modes `dark` and `light`
  - `localStorage` keys: `tattva-theme`, `tattva-mode`, `tattva-left-width`
  - divider range 200px to half the window; arrow keys move it 20px
  - progress bar 12 cells of `█`/`░`
- **Content-Security-Policy**, verbatim: `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`. Also set `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.
- **Viewing never writes:** GET requests never write any file.
- **Existing helpers to reuse**, not re-implement:
  - `loadRegistry`, `saveEntry`, `openProject`, `(*Project).progress`, `appendEvent`, `readEvents`, `progressOf`, `marks`
  - `num`, `hintLabel`, `describeCheck`, `contract`, `stepFile`, `(*Spec).stepIndex`, `(*Spec).prereqNums`, `lastActivity`
  - test helpers `newTestProject`, `testSpec`, `testDetail`, `containsAny`, `readFile`

## Review Focus

1. **A curriculum whose text contains HTML, script tags or `javascript:` links** (Claude's output is untrusted). None of it should run or become a link. Tests: `TestMarkdownDropsUnsafeContent` (Task 2) and `TestProjectEscapesClaudeContent` (Task 5).
2. **Another website in the same browser posting to `127.0.0.1:4747`** to mark steps done or open hints. It must be rejected. Tests: `TestServerRejectsCrossSitePosts` and `TestServerOnlyAnswersLocalHostNames` (Task 4).
3. **Double-clicking a button, or reloading after a post.** No duplicate events should appear. Test: `TestActionsStartAndDone`, which posts every action twice and checks the redirect (Task 6).
4. **The CLI and the browser used side by side.** Progress and hint counts should agree, and the browser's writes shouldn't cause false "edited outside tattva" notices. Test: `TestActionsAgreeWithTheCLI` (Task 6).
5. **A project whose folder moved or whose `spec.json` is broken.** The landing page and project page should still work and show the reason. Tests: `TestHomeShowsProjectCards` (Task 4) and `TestProjectWithBrokenSpecShowsTheError` (Task 5).

---

### Task 1: Hint events and hint counts

**Files:**
- Modify: `progress.go`
- Test: `progress_test.go`

**Interfaces:**
- Consumes: the existing `Event`, `Progress`, `progressOf`, `(*Project).appendEvent`, `cmdNext`, `cmdDone` and `cmdStatus`
- Produces:
  - `Event.Level int` (JSON `level`, omitempty), the hint level for `hint` events
  - `Progress.Hints map[string]int`: the highest hint level opened per step id
  - `(*Project).appendEvent(ev Event) error`: sets `ev.At` itself. The signature changes from `(step, event string)`.
  - `hintNote(n int) string`: `""`, `"1 hint"` or `"N hints"`

- [ ] **Step 1: Write the failing tests**

In `progress_test.go`, change the line
`		ev("respond-to-ping", "hint", 5), // an event type from a later version: ignored`
to
`		ev("respond-to-ping", "reflected", 5), // an event type from a later version: ignored`
(`hint` is now a known event).

Append to `progress_test.go`:

```go
func TestProgressCountsHints(t *testing.T) {
	pr := progressOf(testSpec(), []Event{
		{Step: "create-run-script", Event: "hint", Level: 1},
		{Step: "create-run-script", Event: "hint", Level: 2},
		{Step: "respond-to-ping", Event: "hint", Level: 1},
	})
	if pr.Hints["create-run-script"] != 2 || pr.Hints["respond-to-ping"] != 1 || pr.Hints["echo-command"] != 0 {
		t.Fatalf("hints = %v", pr.Hints)
	}
	if pr.Status["create-run-script"] != Available {
		t.Fatalf("opening a hint must not change a step's status, got %v", pr.Status["create-run-script"])
	}
}

func TestStatusShowsHintCounts(t *testing.T) {
	p := newTestProject(t)
	for level := 1; level <= 2; level++ {
		if err := p.appendEvent(Event{Step: "create-run-script", Event: "hint", Level: level}); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := cmdStatus(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "  ○ 01 Create run.sh (2 hints)\n") {
		t.Fatalf("status = %s", out.String())
	}
	events, _, _ := readEvents(p.abs(progressFile))
	if len(events) != 2 || events[1].Level != 2 || events[1].At.IsZero() {
		t.Fatalf("events = %+v", events)
	}
}

func TestHintNote(t *testing.T) {
	for n, want := range map[int]string{0: "", 1: "1 hint", 3: "3 hints"} {
		if got := hintNote(n); got != want {
			t.Errorf("hintNote(%d) = %q, want %q", n, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile. The errors include `unknown field Level in struct literal`, `pr.Hints undefined`, `too many arguments` or `cannot use ... as string value` for `appendEvent`, and `undefined: hintNote`.

- [ ] **Step 3: Implement**

In `progress.go`:

1. Add the field to `Event`, so the struct reads:
```go
type Event struct {
	At    time.Time `json:"at"`
	Step  string    `json:"step"`
	Event string    `json:"event"`
	Level int       `json:"level,omitempty"` // which hint, for "hint" events
}
```
2. Add a field to `Progress` after `Done int`:
```go
	Hints   map[string]int // the highest hint level opened, per step
```
3. In `progressOf`:
   - Under `completed := map[string]bool{}`, add `hints := map[string]int{}`.
   - In the `switch ev.Event`, add after the `completed` case:
```go
		case "hint":
			if ev.Level > hints[ev.Step] {
				hints[ev.Step] = ev.Level
			}
```
   - Change `pr := Progress{Status: map[string]Status{}}` to `pr := Progress{Status: map[string]Status{}, Hints: hints}`.
4. Replace the first lines of `appendEvent`:
```go
func (p *Project) appendEvent(step, event string) error {
	line, err := json.Marshal(Event{At: now(), Step: step, Event: event})
```
with:
```go
func (p *Project) appendEvent(ev Event) error {
	ev.At = now()
	line, err := json.Marshal(ev)
```
   Also update the comment above it to say it adds one event, stamped with the current time, to the progress log and records the new hash.
5. Update the two callers:
   - In `cmdNext`, change `p.appendEvent(st.ID, "started")` to `p.appendEvent(Event{Step: st.ID, Event: "started"})`.
   - In `cmdDone`, change `p.appendEvent(st.ID, "completed")` to `p.appendEvent(Event{Step: st.ID, Event: "completed"})`.
6. In `cmdStatus`, replace
```go
			note := ""
			if st.Detail == nil {
				note = "  (not expanded)"
			}
```
with
```go
			note := ""
			if h := hintNote(pr.Hints[st.ID]); h != "" {
				note = " (" + h + ")"
			}
			if st.Detail == nil {
				note += "  (not expanded)"
			}
```
7. Append:
```go
// hintNote is "1 hint", "3 hints", or "" when no hint was opened.
func hintNote(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 hint"
	}
	return fmt.Sprintf("%d hints", n)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS, including every existing progress test.

- [ ] **Step 5: Commit**

```bash
git add progress.go progress_test.go
git commit -m "feat: record hint events and show hint counts in status"
```

---

### Task 2: Markdown rendering with goldmark

**Files:**
- Create: `markdown.go`
- Test: `markdown_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Consumes: nothing
- Produces: `markdown(s string) template.HTML` (`html/template`). Claude's markdown becomes safe HTML: raw HTML is dropped and dangerous link targets are removed.

- [ ] **Step 1: Write the failing tests**

`markdown_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestMarkdownRendersFormatting(t *testing.T) {
	got := string(markdown("**bold**, `code` and a [link](https://redis.io)\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"))
	for _, want := range []string{"<strong>bold</strong>", "<code>code</code>", `<a href="https://redis.io">link</a>`, "<table>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownDropsUnsafeContent(t *testing.T) {
	got := string(markdown("<script>alert(1)</script>\n\n[click](javascript:alert(1)) and <img src=x onerror=alert(1)>\n"))
	for _, bad := range []string{"<script", "javascript:", "onerror", "<img"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe %q reached the page:\n%s", bad, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: markdown`.

- [ ] **Step 3: Add goldmark and write `markdown.go`**

Run: `go get github.com/yuin/goldmark@latest`

`markdown.go`:

```go
package main

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// md renders the markdown Claude writes. goldmark leaves raw HTML out and
// drops dangerous link targets unless it's told to trust its input, and it
// isn't: this text comes from Claude, not from us.
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// markdown renders s as HTML for the workspace pages.
func markdown(s string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(s))
	}
	return template.HTML(buf.String())
}
```

Then run `go mod tidy`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS. `go.mod` now has a `require github.com/yuin/goldmark v1.x.y` line.

- [ ] **Step 5: Commit**

```bash
git add markdown.go markdown_test.go go.mod go.sum
git commit -m "feat: render markdown safely with goldmark"
```

---

### Task 3: Themes, browser scripts and bundled mermaid

**Files:**
- Create: `web.go`
- Create: `web/static/app.css`
- Create: `web/static/theme-boot.js`
- Create: `web/static/app.js`
- Create: `web/static/mermaid.min.js` and `web/static/mermaid.LICENSE` (downloaded)
- Test: `web_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `webFS embed.FS`, which embeds `web/`
  - `themes []string`: `catppuccin`, `tokyo-night`, `gruvbox`
  - `staticFiles fs.FS`, which is `web/static`
  - the CSS classes and element ids the templates in Tasks 4–5 use: `bar`, `brand`, `windows`, `stats`, `pane`, `pane-title`, `pane-body`, `tiles`, `card`, `card-name`, `card-meta`, `card-bar`, `card-current`, `missing`, `panes`, `steps`, `content`, `divider`, `nav-item`, `selected`, `phase`, `mark`, `status-done`, `status-in-progress`, `status-available`, `status-locked`, `unexpanded`, `hint-count`, `md`, `meta`, `dim`, `note`, `actions`, `done-note`, `hint`, `checks`, and ids `theme`, `mode`, `divider`, `panes`
  - the `<html>` attributes the scripts rely on: `data-themes`, `data-theme`, `data-mode`
  - `<pre class="mermaid" data-source="…">` for diagrams

- [ ] **Step 1: Write the failing tests**

`web_test.go`:

```go
package main

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func readStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(staticFiles, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// themeSection splits app.css into its theme blocks and everything else.
func themeSection(t *testing.T) (blocks, rest string) {
	t.Helper()
	css := readStatic(t, "app.css")
	start := strings.Index(css, "/* === themes === */")
	end := strings.Index(css, "/* === end themes === */")
	if start < 0 || end < start {
		t.Fatal("app.css must keep its theme blocks between /* === themes === */ and /* === end themes === */")
	}
	return css[start:end], css[:start] + css[end:]
}

func TestCSSColoursOnlyInThemeBlocks(t *testing.T) {
	_, rest := themeSection(t)
	colour := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch)\(`)
	if m := colour.FindString(rest); m != "" {
		t.Errorf("colour %q is written outside the theme blocks; use a variable instead", m)
	}
}

func TestEveryThemeHasDarkAndLightBlocks(t *testing.T) {
	blocks, _ := themeSection(t)
	block := regexp.MustCompile(`:root\[data-theme="([a-z0-9-]+)"\]\[data-mode="(dark|light)"\]\s*\{([^}]*)\}`)
	varName := regexp.MustCompile(`(--[a-z0-9-]+)\s*:`)
	sets := map[string][]string{}
	var first []string
	for _, m := range block.FindAllStringSubmatch(blocks, -1) {
		var names []string
		for _, v := range varName.FindAllStringSubmatch(m[3], -1) {
			names = append(names, v[1])
		}
		slices.Sort(names)
		sets[m[1]+"/"+m[2]] = names
		if first == nil {
			first = names
		}
	}
	for _, name := range themes {
		for _, mode := range []string{"dark", "light"} {
			got, ok := sets[name+"/"+mode]
			switch {
			case !ok:
				t.Errorf("theme %q has no %s block in app.css", name, mode)
			case !slices.Equal(got, first):
				t.Errorf("theme %s/%s sets %v; every block must set the same variables: %v", name, mode, got, first)
			}
		}
	}
	if len(sets) != 2*len(themes) {
		t.Errorf("app.css has %d theme blocks for %d themes; list every theme in web.go", len(sets), len(themes))
	}
}

func TestCSSVariablesAreDefined(t *testing.T) {
	css := readStatic(t, "app.css")
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`var\((--[a-z0-9-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("app.css uses %s but never defines it", m[1])
		}
	}
}

func TestMermaidIsBundled(t *testing.T) {
	js := readStatic(t, "mermaid.min.js")
	if len(js) < 1<<20 || !strings.Contains(js, "mermaid") {
		t.Fatalf("web/static/mermaid.min.js looks wrong (%d bytes)", len(js))
	}
	if !strings.Contains(readStatic(t, "mermaid.LICENSE"), "MIT") {
		t.Error("mermaid's licence must ship with the bundle")
	}
}

func TestScriptsAreServedFiles(t *testing.T) {
	for _, name := range []string{"theme-boot.js", "app.js"} {
		if js := readStatic(t, name); !strings.Contains(js, "tattva-") {
			t.Errorf("%s doesn't use the tattva-* storage keys", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: staticFiles` and `undefined: themes`.

- [ ] **Step 3: Write `web.go`**

```go
package main

import (
	"embed"
	"io/fs"
)

// webFS holds the workspace's templates and static files, built into the binary.
//
//go:embed web
var webFS embed.FS

// themes are the workspace's colour themes in dropdown order; the first is
// the default. Each has a dark and a light block in web/static/app.css.
var themes = []string{"catppuccin", "tokyo-night", "gruvbox"}

// staticFiles is web/static, served at /static/.
var staticFiles = mustSub(webFS, "web/static")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
```

- [ ] **Step 4: Write `web/static/app.css`**

```css
/* tattva workspace styles.
   Every colour comes from a variable. The theme blocks below set those
   variables; to add a theme, copy a dark and a light block, change the
   colours, and add the theme's name to `themes` in web.go. */

/* === themes === */
:root[data-theme="catppuccin"][data-mode="dark"] {
  color-scheme: dark;
  --bg: #11111b;
  --surface: #1e1e2e;
  --surface-2: #181825;
  --selection: #313244;
  --text: #cdd6f4;
  --subtext: #a6adc8;
  --dim: #6c7086;
  --border: #45475a;
  --border-active: #cba6f7;
  --accent: #cba6f7;
  --link: #89b4fa;
  --done: #a6e3a1;
  --current: #fab387;
  --warning: #f38ba8;
}
:root[data-theme="catppuccin"][data-mode="light"] {
  color-scheme: light;
  --bg: #dce0e8;
  --surface: #eff1f5;
  --surface-2: #e6e9ef;
  --selection: #ccd0da;
  --text: #4c4f69;
  --subtext: #6c6f85;
  --dim: #9ca0b0;
  --border: #bcc0cc;
  --border-active: #8839ef;
  --accent: #8839ef;
  --link: #1e66f5;
  --done: #40a02b;
  --current: #fe640b;
  --warning: #d20f39;
}
:root[data-theme="tokyo-night"][data-mode="dark"] {
  color-scheme: dark;
  --bg: #16161e;
  --surface: #1a1b26;
  --surface-2: #1f2335;
  --selection: #292e42;
  --text: #c0caf5;
  --subtext: #a9b1d6;
  --dim: #565f89;
  --border: #3b4261;
  --border-active: #7aa2f7;
  --accent: #bb9af7;
  --link: #7aa2f7;
  --done: #9ece6a;
  --current: #ff9e64;
  --warning: #f7768e;
}
:root[data-theme="tokyo-night"][data-mode="light"] {
  color-scheme: light;
  --bg: #c4c8da;
  --surface: #e1e2e7;
  --surface-2: #d0d5e3;
  --selection: #b7c1e3;
  --text: #3760bf;
  --subtext: #6172b0;
  --dim: #848cb5;
  --border: #a8aecb;
  --border-active: #2e7de9;
  --accent: #9854f1;
  --link: #2e7de9;
  --done: #587539;
  --current: #b15c00;
  --warning: #f52a65;
}
:root[data-theme="gruvbox"][data-mode="dark"] {
  color-scheme: dark;
  --bg: #1d2021;
  --surface: #282828;
  --surface-2: #32302f;
  --selection: #3c3836;
  --text: #ebdbb2;
  --subtext: #bdae93;
  --dim: #928374;
  --border: #504945;
  --border-active: #fabd2f;
  --accent: #fabd2f;
  --link: #83a598;
  --done: #b8bb26;
  --current: #fe8019;
  --warning: #fb4934;
}
:root[data-theme="gruvbox"][data-mode="light"] {
  color-scheme: light;
  --bg: #ebdbb2;
  --surface: #fbf1c7;
  --surface-2: #f2e5bc;
  --selection: #ebdbb2;
  --text: #3c3836;
  --subtext: #665c54;
  --dim: #928374;
  --border: #d5c4a1;
  --border-active: #8f3f71;
  --accent: #8f3f71;
  --link: #076678;
  --done: #79740e;
  --current: #af3a03;
  --warning: #9d0006;
}
/* === end themes === */

:root {
  --font: ui-monospace, "SF Mono", SFMono-Regular, Menlo, "JetBrains Mono", "Cascadia Mono", Consolas, "Liberation Mono", monospace;
  --gap: 8px;
  --radius: 10px;
  --left-width: 300px;
}

* { box-sizing: border-box; }
html, body { height: 100%; }
body {
  margin: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg);
  color: var(--text);
  font: 14px/1.6 var(--font);
}
a { color: var(--link); }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
::selection { background: var(--selection); }

/* The top bar, like tmux's status line moved to the top. */
.bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  padding: 4px 12px;
  background: var(--surface-2);
  color: var(--subtext);
  border-bottom: 1px solid var(--border);
}
.brand { color: var(--accent); font-weight: bold; }
.bar select, .bar button {
  font: inherit;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0 6px;
  cursor: pointer;
}
.windows { display: flex; flex-wrap: wrap; gap: 10px; }
.windows a { color: var(--subtext); text-decoration: none; padding: 0 4px; border-radius: 3px; }
.windows a:hover { color: var(--text); }
.windows a.active { color: var(--surface); background: var(--accent); }
.stats { margin-left: auto; }

/* Panes: tiled with gaps like Hyprland, titles in the top border like tmux. */
.pane {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px 14px 10px;
}
.pane:hover, .pane:focus-within { border-color: var(--border-active); }
.pane-title {
  position: absolute;
  top: -0.8em;
  left: 12px;
  margin: 0;
  padding: 0 6px;
  font-size: 13px;
  font-weight: normal;
  line-height: 1.6;
  color: var(--accent);
  background: var(--surface);
}
.pane-body { flex: 1; min-height: 0; overflow: auto; }

/* Landing page: one pane per project. */
.tiles {
  flex: 1;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  align-content: start;
  gap: 24px 16px;
  padding: 24px 16px 16px;
  overflow: auto;
}
.card { color: var(--text); text-decoration: none; }
.card p { margin: 2px 0; }
.card-name { font-weight: bold; }
.card-meta, .card-current { color: var(--subtext); }
.card-bar { color: var(--done); }
.card.missing { color: var(--dim); }
.card.missing:hover { border-color: var(--border); }

/* Project page: the steps pane, the divider and the content pane. */
.panes { flex: 1; display: flex; min-height: 0; padding: 20px var(--gap) var(--gap); }
.steps { flex: none; width: var(--left-width); min-width: 200px; max-width: 50vw; }
.content { flex: 1; min-width: 0; }
.divider { flex: none; width: 12px; cursor: col-resize; touch-action: none; }
.divider::after { content: ""; display: block; width: 2px; height: 100%; margin: 0 auto; border-radius: 1px; }
.divider:hover::after, .divider:focus-visible::after { background: var(--border-active); }
.divider:focus-visible { outline: none; }

.nav-item {
  display: block;
  padding: 0 6px;
  border-radius: 4px;
  color: var(--text);
  text-decoration: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
a.nav-item:hover, .nav-item.selected { background: var(--selection); }
.nav-item.selected { color: var(--accent); }
.phase { margin: 12px 0 2px; color: var(--subtext); }
.status-done .mark { color: var(--done); }
.status-in-progress .mark { color: var(--current); }
.status-locked, .unexpanded { color: var(--dim); }
.hint-count { color: var(--dim); font-size: 12px; }

/* Rendered markdown, styled like a terminal pager. */
.md h1, .md h2, .md h3 { color: var(--accent); font-size: 1em; margin: 1.4em 0 0.4em; }
.md h1:first-child { margin-top: 0; }
.md h1::before { content: "# "; color: var(--dim); }
.md h2::before { content: "## "; color: var(--dim); }
.md h3::before { content: "### "; color: var(--dim); }
.md p, .md ul, .md ol { margin: 0.4em 0; }
.md li > p { margin: 0; }
.md code { background: var(--surface-2); padding: 0 4px; border-radius: 3px; }
.md pre { background: var(--surface-2); padding: 10px 12px; border-radius: 6px; overflow: auto; }
.md pre code { background: none; padding: 0; }
.md table { border-collapse: collapse; }
.md th, .md td { border: 1px solid var(--border); padding: 2px 8px; }
.md blockquote { margin: 0.4em 0; padding-left: 12px; border-left: 2px solid var(--border); color: var(--subtext); }
.meta { color: var(--subtext); }
.dim { color: var(--dim); }
.note { color: var(--warning); }
pre.mermaid { background: var(--surface); text-align: center; }
details.checks summary { cursor: pointer; color: var(--subtext); }

/* Buttons look like bracketed terminal text. */
.actions { display: flex; flex-wrap: wrap; gap: 16px; align-items: baseline; margin: 0.4em 0 1em; }
.actions button, .hint button {
  font: inherit;
  color: var(--accent);
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
}
.actions button:hover, .hint button:hover { text-decoration: underline; }
.done-note { color: var(--done); }

@media (max-width: 800px) {
  .panes { flex-direction: column; gap: 24px; }
  .steps { width: auto; max-width: none; max-height: 40vh; }
  .divider { display: none; }
}
```

- [ ] **Step 5: Write `web/static/theme-boot.js`**

```js
// Applies the saved theme, mode and pane width before the page draws, so it
// never flashes the wrong colours. Loaded from <head>; there are no inline scripts.
(function () {
  var root = document.documentElement;
  var known = (root.dataset.themes || "").split(" ");
  try {
    var theme = localStorage.getItem("tattva-theme");
    if (theme && known.indexOf(theme) >= 0) root.dataset.theme = theme;
    var mode = localStorage.getItem("tattva-mode");
    if (mode === "dark" || mode === "light") root.dataset.mode = mode;
    var width = localStorage.getItem("tattva-left-width");
    if (/^\d+px$/.test(width || "")) root.style.setProperty("--left-width", width);
  } catch (e) {
    // Storage can be unavailable (private windows, blocked site data): keep the defaults.
  }
})();
```

- [ ] **Step 6: Write `web/static/app.js`**

```js
// tattva workspace: theme picker, pane divider and diagrams. No framework.
(function () {
  var root = document.documentElement;
  var save = function (key, value) {
    try { localStorage.setItem(key, value); } catch (e) { /* storage unavailable */ }
  };

  // Theme and mode.
  var theme = document.getElementById("theme");
  var mode = document.getElementById("mode");
  var showMode = function () { mode.textContent = "[" + root.dataset.mode + "]"; };
  theme.value = root.dataset.theme;
  showMode();
  theme.addEventListener("change", function () {
    root.dataset.theme = theme.value;
    save("tattva-theme", theme.value);
    drawDiagrams();
  });
  mode.addEventListener("click", function () {
    root.dataset.mode = root.dataset.mode === "dark" ? "light" : "dark";
    save("tattva-mode", root.dataset.mode);
    showMode();
    drawDiagrams();
  });

  // Divider: drag it, or use the arrow keys, to size the steps pane between
  // 200px and half the window.
  var divider = document.getElementById("divider");
  var steps = document.querySelector(".steps");
  if (divider && steps) {
    var setWidth = function (px) {
      var width = Math.round(Math.max(200, Math.min(px, window.innerWidth / 2))) + "px";
      root.style.setProperty("--left-width", width);
      save("tattva-left-width", width);
    };
    divider.addEventListener("pointerdown", function (down) {
      down.preventDefault();
      divider.setPointerCapture(down.pointerId);
      var left = steps.getBoundingClientRect().left;
      var move = function (e) { setWidth(e.clientX - left); };
      var up = function () {
        divider.removeEventListener("pointermove", move);
        divider.removeEventListener("pointerup", up);
      };
      divider.addEventListener("pointermove", move);
      divider.addEventListener("pointerup", up);
    });
    divider.addEventListener("keydown", function (e) {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      setWidth(steps.getBoundingClientRect().width + (e.key === "ArrowRight" ? 20 : -20));
    });
  }

  // Keep the selected step in view.
  var selected = document.querySelector(".steps .selected");
  if (selected) selected.scrollIntoView({ block: "nearest" });

  // Architecture diagrams, coloured from the current theme's variables.
  function drawDiagrams() {
    var blocks = document.querySelectorAll("pre.mermaid");
    if (!blocks.length || !window.mermaid) return;
    var css = getComputedStyle(root);
    var v = function (name) { return css.getPropertyValue(name).trim(); };
    window.mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: "base",
      fontFamily: v("--font"),
      themeVariables: {
        background: v("--surface"),
        primaryColor: v("--surface-2"),
        primaryTextColor: v("--text"),
        primaryBorderColor: v("--accent"),
        secondaryColor: v("--surface-2"),
        tertiaryColor: v("--surface"),
        lineColor: v("--subtext"),
        textColor: v("--text")
      }
    });
    blocks.forEach(function (el) {
      el.removeAttribute("data-processed");
      el.textContent = el.dataset.source;
    });
    window.mermaid.run({ nodes: Array.prototype.slice.call(blocks) }).catch(function () {
      // mermaid draws its own error message in place of a broken diagram
    });
  }
  drawDiagrams();
})();
```

- [ ] **Step 7: Download mermaid 11 and pin its version**

```bash
V=$(curl -fsSL "https://data.jsdelivr.com/v1/packages/npm/mermaid/resolved?specifier=11" | sed -E 's/.*"version":"([^"]+)".*/\1/')
echo "mermaid version: $V"
{ echo "/* mermaid $V (MIT), from https://cdn.jsdelivr.net/npm/mermaid@$V/dist/mermaid.min.js */"; curl -fsSL "https://cdn.jsdelivr.net/npm/mermaid@$V/dist/mermaid.min.js"; } > web/static/mermaid.min.js
curl -fsSL "https://cdn.jsdelivr.net/npm/mermaid@$V/LICENSE" -o web/static/mermaid.LICENSE
ls -l web/static/mermaid.min.js web/static/mermaid.LICENSE && head -c 200 web/static/mermaid.min.js
```
Expected: `$V` is an `11.x.y` version. The bundle is a few MB and starts with the pin comment. The licence file mentions MIT. If the version lookup fails, stop and report; don't guess a URL.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS. If `TestCSSColoursOnlyInThemeBlocks` fails, a colour slipped outside the theme blocks: replace it with a variable. Don't change the test.

- [ ] **Step 9: Commit**

```bash
git add web.go web_test.go web/static
git commit -m "feat: add workspace themes, scripts and bundled mermaid $V"
```
(Put the actual version number in place of `$V` in the message.)

---

### Task 4: `tattva serve`, security and the landing page

**Files:**
- Create: `serve.go`
- Create: `web/templates/base.html`
- Create: `web/templates/home.html`
- Modify: `registry.go` (a shared read-only project loader; `cmdList` uses it)
- Modify: `main.go` (the `serve` command and its usage line)
- Test: `serve_test.go`, `registry_test.go` (append)

**Interfaces:**
- Consumes:
  - `markdown` (Task 2), `hintNote` and `Progress.Hints` (Task 1)
  - `webFS`, `themes` and `staticFiles` (Task 3)
  - `loadRegistry`, `loadSpec`, `Validate`, `readEvents`, `progressOf`, `num`, `lastActivity` and `saveEntry`
  - test helpers `newTestProject` and `cmdNext`
- Produces:
  - `projectView{N int; ID, Name, Slug, Target, Language string; Done, Total, Hints int; Focus, Problem string}`, plus unexported `root string`, `spec *Spec` and `events []Event`
  - `loadProjects() ([]projectView, string)`: sorted by name, read-only; the string is a registry warning
  - `focusOf(s *Spec, pr Progress) string`
  - `cmdServe(ctx context.Context, port int, out io.Writer) error`
  - `openBrowser` (a var) and `newServer(port int) http.Handler`
  - `pageView{Projects []projectView; Current *projectView}`, extended in Task 5, with the method `IsCurrent(id string) bool`
  - `pageFuncs template.FuncMap`, `page(name string) *template.Template`, `show(w, t, v pageView)` and `homePage`
  - `slug(name string) string` and `progressBar(done, total int) string`
  - test helpers `request(t, method, path string, form url.Values) *httptest.ResponseRecorder` and `noBrowser(t)`

- [ ] **Step 1: Write the failing tests**

`serve_test.go`:

```go
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
```

Append to `registry_test.go`:

```go
func TestListSurvivesAnUnreadableProgressFile(t *testing.T) {
	p := newTestProject(t)
	if err := os.Mkdir(p.abs(progressFile), 0o755); err != nil { // a directory can't be read as a file
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdList(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Mini Redis  (can't read its progress: ") {
		t.Fatalf("out = %s", out.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: FAIL to compile with `undefined: newServer`, `undefined: cmdServe` and `undefined: openBrowser`.

- [ ] **Step 3: Add the shared project loader to `registry.go`**

Replace the whole `cmdList` function in `registry.go` with:

```go
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
```

- [ ] **Step 4: Write `serve.go`**

```go
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
```

- [ ] **Step 5: Write the templates**

`web/templates/base.html`:

```html
{{define "base"}}<!doctype html>
<html lang="en" data-themes="{{join (themes) " "}}" data-theme="{{index (themes) 0}}" data-mode="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{template "title" .}} · tattva</title>
<script src="/static/theme-boot.js"></script>
<link rel="stylesheet" href="/static/app.css">
</head>
<body>
<header class="bar">
  <span class="brand">[tattva]</span>
  <label>theme <select id="theme">{{range themes}}<option value="{{.}}">{{.}}</option>{{end}}</select></label>
  <button type="button" id="mode" aria-label="Switch between dark and light mode">[dark]</button>
  <nav class="windows" aria-label="Projects">
    <a href="/"{{if not .Current}} class="active" aria-current="page"{{end}}>0:projects</a>
    {{- range .Projects}}
    <a href="/p/{{.ID}}"{{if $.IsCurrent .ID}} class="active" aria-current="page"{{end}}>{{.N}}:{{.Slug}}{{if $.IsCurrent .ID}}*{{end}}</a>
    {{- end}}
  </nav>
  {{- with .Current}}{{if not .Problem}}
  <span class="stats">{{.Done}}/{{.Total}} ✓{{with hintNote .Hints}} · {{.}}{{end}}</span>
  {{- end}}{{end}}
</header>
{{template "main" .}}
<script src="/static/app.js"></script>
</body>
</html>
{{end}}
```

`web/templates/home.html`:

```html
{{define "title"}}projects{{end}}
{{define "main"}}
<main class="tiles">
{{- range .Projects}}
{{- if .Problem}}
  <section class="pane card missing">
    <h2 class="pane-title">{{.N}}:{{.Slug}}</h2>
    <p class="card-name">{{.Name}}</p>
    <p class="note">{{.Problem}}</p>
  </section>
{{- else}}
  <a class="pane card" href="/p/{{.ID}}">
    <h2 class="pane-title">{{.N}}:{{.Slug}}</h2>
    <p class="card-name">{{.Name}}</p>
    <p class="card-meta">{{.Target}} · {{.Language}}</p>
    <p><span class="card-bar">[{{progressBar .Done .Total}}]</span> {{.Done}}/{{.Total}}{{with hintNote .Hints}} · {{.}}{{end}}</p>
    <p class="card-current">{{.Focus}}</p>
  </a>
{{- end}}
{{- else}}
  <section class="pane card">
    <h2 class="pane-title">0:projects</h2>
    <p>No projects yet. In a new folder, run <code>tattva new "&lt;target&gt;" --lang &lt;lang&gt;</code>.</p>
  </section>
{{- end}}
</main>
{{end}}
```

- [ ] **Step 6: Add the `serve` command to `main.go`**

In `usage`, add this line after the `tattva list` line:
```
  tattva serve [--port <n>]                           read and track in your browser
```
Insert this case in `run`'s `switch`, before `default:`:
```go
	case "serve":
		fs := newFlags(cmd)
		port := fs.Int("port", 4747, "port to listen on")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: tattva serve [--port <n>]")
		}
		return cmdServe(ctx, *port, out)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS, including the existing `TestList*` tests. `cmdList`'s output must be unchanged apart from the new unreadable-progress case.

- [ ] **Step 8: Commit**

```bash
git add serve.go serve_test.go web/templates registry.go registry_test.go main.go
git commit -m "feat: add tattva serve with the landing page"
```

---

### Task 5: Project page: steps pane, overview and step view

**Files:**
- Modify: `render.go` (pull the step view data out into `stepViewOf`)
- Modify: `serve.go` (project page types, handler, routes; `safeURL`)
- Create: `web/templates/project.html`
- Test: `serve_test.go` (append)

**Interfaces:**
- Consumes:
  - `pageView`, `show`, `page`, `pageFuncs`, `loadProjects` and `projectView` (Task 4)
  - `Progress.Hints` and `hintNote` (Task 1)
  - `markdown` (Task 2), and `marks`, `num`, `stepFile`, `contract`, `describeCheck` and `hintLabel`
- Produces:
  - `stepViewOf(s *Spec, i int) stepView`, used by `renderStep` and the workspace
  - `stepPage`, `hintItem`, `overviewPage`, `phaseNav` and `stepNav`
  - `statusNames map[Status]string`
  - `newStepPage(id string, s *Spec, pr Progress, i int) *stepPage` and `phaseNavs(s *Spec, pr Progress, selected int) []phaseNav`
  - `handleProject`, `safeURL(u string) bool` and `projectPage`
  - new `pageView` fields: `Phases []phaseNav; Overview *overviewPage; Step *stepPage; Problem string`

- [ ] **Step 1: Write the failing tests**

Append to `serve_test.go`, and add `"os"` to its imports:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: the new tests FAIL with status 404 for `/p/...`, since there's no project route yet. `TestProjectNotFound` may pass already.

- [ ] **Step 3: Pull `stepViewOf` out of `renderStep` in `render.go`**

Replace the `renderStep` function (from its `// renderStep returns…` comment to its closing brace) with:

```go
// renderStep returns the markdown file for step i, which must be expanded.
func renderStep(s *Spec, i int) []byte { return execute("step.md.tmpl", stepViewOf(s, i)) }

// stepViewOf gathers what both the step's markdown file and the workspace
// show for step i, which must be expanded.
func stepViewOf(s *Spec, i int) stepView {
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
		if hi == 2 && !strings.Contains(h, "```") {
			h = "```text\n" + strings.TrimSpace(h) + "\n```" // unfenced pseudocode renders as one run-on paragraph
		}
		v.Hints = append(v.Hints, hintView{Label: hintLabel(hi), Text: h})
	}
	for j, other := range s.Steps {
		for _, pre := range other.Prerequisites {
			if pre == st.ID {
				v.Unlocks = append(v.Unlocks, num(j)+" · "+other.Title)
			}
		}
	}
	return v
}
```

- [ ] **Step 4: Add the project page to `serve.go`**

1. Add `"net/url"`, `"path"`, `"slices"` and `"strconv"` to the imports.
2. In `newServer`, add after the `GET /{$}` line:
```go
	mux.HandleFunc("GET /p/{id}", handleProject)
	mux.HandleFunc("GET /p/{id}/s/{n}", handleProject)
```
3. Replace the `pageView` struct with:
```go
// pageView is what every page template receives.
type pageView struct {
	Projects []projectView
	Current  *projectView  // the project being shown; nil on the landing page
	Phases   []phaseNav    // project page: the steps pane
	Overview *overviewPage // project page showing the overview
	Step     *stepPage     // project page showing a step
	Problem  string        // project page for a project that can't be shown
}
```
4. Add `"safeURL": safeURL,` to `pageFuncs`.
5. Under `var homePage = page("home.html")`, add `var projectPage = page("project.html")`.
6. Append:

```go
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
```

- [ ] **Step 5: Write `web/templates/project.html`**

```html
{{define "title"}}{{.Current.Slug}}{{end}}
{{define "main"}}
<main class="panes" id="panes">
  <nav class="pane steps" aria-label="Steps">
    <h2 class="pane-title">steps</h2>
    <div class="pane-body">
      <a class="nav-item{{if .Overview}} selected{{end}}" href="/p/{{.Current.ID}}"{{if .Overview}} aria-current="page"{{end}}>▸ overview</a>
      {{- range .Phases}}
      <p class="phase">{{.Title}}</p>
      {{- range .Steps}}
      {{- if .Expanded}}
      <a class="nav-item status-{{.Status}}{{if .Selected}} selected{{end}}" href="/p/{{$.Current.ID}}/s/{{.N}}"{{if .Selected}} aria-current="page"{{end}}><span class="mark">{{.Mark}}</span> {{.Num}} {{.Title}}{{with .Hints}} <span class="hint-count">{{.}}</span>{{end}}</a>
      {{- else}}
      <span class="nav-item status-{{.Status}} unexpanded" title="Not expanded yet: run tattva expand"><span class="mark">{{.Mark}}</span> {{.Num}} {{.Title}}</span>
      {{- end}}
      {{- end}}
      {{- end}}
    </div>
  </nav>
  <div class="divider" id="divider" role="separator" aria-orientation="vertical" aria-label="Resize the steps pane" tabindex="0"></div>
  <article class="pane content">
  {{- if .Problem}}
    <h2 class="pane-title">error</h2>
    <div class="pane-body"><p class="note">{{.Problem}}</p></div>
  {{- else if .Overview}}{{template "overview" .Overview}}
  {{- else}}{{template "step" .}}
  {{- end}}
  </article>
</main>
{{end}}

{{define "references"}}{{if .}}
<h2>references</h2>
<ul>
{{- range .}}
  <li>{{if safeURL .URL}}<a href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{.Title}}</a> ↗{{else}}{{.Title}} <span class="dim">({{.URL}})</span>{{end}}</li>
{{- end}}
</ul>
{{- end}}{{end}}

{{define "overview"}}
<h2 class="pane-title">README.md</h2>
<div class="pane-body md">
<h1>{{.Project.Name}}</h1>
{{markdown .Project.Summary}}
<p class="meta">target: {{.Project.Target}} · language: {{.Project.Language}}</p>
<h2>what this curriculum assumes</h2>
<ul>{{range .Project.Assumes}}<li>{{.}}</li>{{end}}</ul>
<h2>scope</h2>
<p>in scope:</p>
<ul>{{range .Scope.In}}<li>{{.}}</li>{{end}}</ul>
<p>left out:</p>
<ul>{{range .Scope.Out}}<li><strong>{{.Feature}}</strong>: {{.Why}}</li>{{end}}</ul>
<h2>how the real system is built</h2>
{{markdown .Architecture.Real}}
<h2>what you'll build</h2>
<ul>{{range .Architecture.Components}}<li><strong>{{.ID}}</strong>: {{.Responsibility}}{{with .TalksTo}} (talks to {{join . ", "}}){{end}}</li>{{end}}</ul>
<pre class="mermaid" data-source="{{.Architecture.Diagram}}">{{.Architecture.Diagram}}</pre>
<h2>program contract</h2>
{{markdown .Contract}}
<h2>concepts</h2>
<ul>{{range .Concepts}}<li><strong>{{.Name}}</strong> (<code>{{.ID}}</code>): {{.Summary}}</li>{{end}}</ul>
{{template "references" .References}}
</div>
<script src="/static/mermaid.min.js"></script>
{{end}}

{{define "step"}}{{with .Step}}
<h2 class="pane-title">{{.File}}</h2>
<div class="pane-body md">
<h1>{{.Num}} · {{.Step.Title}}</h1>
<p class="meta">phase {{.PhaseNum}}: {{.PhaseTitle}} · concepts: {{join .Step.Concepts ", "}}{{with .After}} · after: {{.}}{{end}}</p>
<form class="actions" method="post">
  {{- if eq .Status "available"}}<button formaction="{{.Base}}/start">[ start step ]</button>{{end}}
  {{- if eq .Status "done"}}<span class="done-note">✓ done</span>{{else}}<button formaction="{{.Base}}/done">[ mark done ]</button>{{end}}
  {{- if and .Missing (ne .Status "done")}}<span class="note">prerequisites {{.Missing}} aren't done yet</span>{{end}}
</form>
<h2>goal</h2>
{{markdown .Step.Goal}}
<h2>why</h2>
{{markdown .Step.Detail.Why}}
<h2>context</h2>
{{markdown .Step.Detail.Context}}
<h2>your task</h2>
{{markdown .Step.Detail.Task}}
{{- with .Step.Detail.Constraints}}
<h2>constraints</h2>
<ul>{{range .}}<li>{{markdown .}}</li>{{end}}</ul>
{{- end}}
<h2>check it</h2>
{{markdown .Step.Detail.ByHand}}
<p><strong>expected outcome:</strong> {{.Step.Detail.ExpectedOutcome}}</p>
{{- with .Checks}}
<details class="checks"><summary>machine checks (run by the verifier)</summary>
<ul>{{range .}}<li>{{markdown .}}</li>{{end}}</ul>
</details>
{{- end}}
<h2 id="hints">hints</h2>
{{- range .Reveal}}
<section class="hint">
<h3>{{.Label}}</h3>
{{- if .Revealed}}
{{markdown .Text}}
{{- else if .Next}}
<form method="post" action="{{$.Step.Base}}/hint"><input type="hidden" name="level" value="{{.Level}}"><button>[ show hint {{.Level}} ]</button></form>
{{- else}}
<p class="dim">opens after the hint above</p>
{{- end}}
</section>
{{- end}}
<h2>common mistakes</h2>
<ul>{{range .Step.Detail.CommonMistakes}}<li>{{markdown .}}</li>{{end}}</ul>
<h2>reflect</h2>
{{markdown .Step.Detail.Reflection}}
{{- with .Unlocks}}
<h2>unlocks</h2>
<ul>{{range .}}<li>{{.}}</li>{{end}}</ul>
{{- end}}
{{template "references" .References}}
</div>
{{end}}{{end}}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS. `TestRenderGolden` still passes unchanged, which proves `stepViewOf` didn't change the markdown output.

- [ ] **Step 7: Commit**

```bash
git add render.go serve.go serve_test.go web/templates/project.html
git commit -m "feat: add the project page with steps, overview and step view"
```

---

### Task 6: Buttons: start, done and hints

**Files:**
- Modify: `serve.go` (`handleAction` and its route)
- Test: `serve_test.go` (append)

**Interfaces:**
- Consumes:
  - `openProject`, `(*Project).progress`, `appendEvent(Event)`, `Progress.Hints` and `loadRegistry`
  - the test helpers `request` and `newTestProject`, plus `readFile` and `registryPath`
- Produces: `handleAction`, routed at `POST /p/{id}/s/{n}/{action}`, which answers `303` back to the step (with `#hints` for hints) or `404`

- [ ] **Step 1: Write the failing tests**

Append to `serve_test.go`, and add `"bytes"` to its imports:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./...`
Expected: the action tests FAIL, with 405 or 404 in place of 303, because there's no POST route yet.

- [ ] **Step 3: Implement `handleAction`**

In `newServer`, add after the project routes:
```go
	mux.HandleFunc("POST /p/{id}/s/{n}/{action}", handleAction)
```
Append to `serve.go`:

```go
// handleAction runs one of a step's buttons (start, done or hint) and sends
// the browser back to the step, so reloading doesn't post again.
func handleAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	reg, _ := loadRegistry()
	i := slices.IndexFunc(reg.Projects, func(e Entry) bool { return e.ID == id })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	p, err := openProject(reg.Projects[i].Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || p.Spec.Project.ID != id || n < 1 || n > len(p.Spec.Steps) || p.Spec.Steps[n-1].Detail == nil {
		http.NotFound(w, r)
		return
	}
	st := p.Spec.Steps[n-1]
	pr, err := p.progress()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	back := fmt.Sprintf("/p/%s/s/%d", id, n)
	switch r.PathValue("action") {
	case "start":
		if pr.Status[st.ID] == Available {
			err = p.appendEvent(Event{Step: st.ID, Event: "started"})
		}
	case "done":
		if pr.Status[st.ID] != Done {
			err = p.appendEvent(Event{Step: st.ID, Event: "completed"})
		}
	case "hint":
		back += "#hints"
		if level, _ := strconv.Atoi(r.FormValue("level")); level == pr.Hints[st.ID]+1 && level <= len(st.Detail.Hints) {
			err = p.appendEvent(Event{Step: st.ID, Event: "hint", Level: level})
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w . && go vet ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add serve.go serve_test.go
git commit -m "feat: start, finish and open hints from the workspace"
```

---

### Task 7: Browser check and README

**Files:**
- Modify: `README.md`
- No other code, unless the browser check finds a defect. A defect gets a failing test first, where one is possible, then the fix.

**Interfaces:**
- Consumes: the built binary and the smoke-test curricula in `~/tattva-smoke` (redis, git, sql-db-v2)
- Produces: a verified workspace and user-facing docs

- [ ] **Step 1: Write `README.md`**

Replace the file's contents with:

````markdown
# tattva

Turn a system you've used, such as Redis, Git or a SQL database, into a build-your-own-x curriculum that you implement yourself. Claude designs the path, with small steps, hints and checks. You write all the code.

## Install

You need Go 1.25+ and [Claude Code](https://claude.com/claude-code), installed and logged in.

```
go install github.com/0x1DKFA/tattva@latest
```

## Use

```
mkdir mini-redis && cd mini-redis
tattva new "Redis" --lang go                          # design the curriculum (a few minutes)
tattva revise "skip persistence, go deeper on RESP"   # optional: reshape the outline
tattva expand                                         # write every step's details
tattva next                                           # start the next step
tattva done                                           # finish it
tattva status                                         # see where you are
tattva list                                           # all your projects
tattva serve                                          # read and track in your browser
```

Everything lives in `curriculum/` inside your project: `spec.json` (the curriculum), `progress.jsonl` (your progress), `README.md` and `steps/*.md`. Commit it with your code and git shows your progress next to the code that made it.

`tattva serve` opens a workspace at http://127.0.0.1:4747: your projects as cards, and each project's steps next to the step you're reading. Hints open one at a time and are recorded, so you can see which steps you finished on your own. Pick a theme and dark or light mode in the top-left corner.
````

- [ ] **Step 2: Build and start the workspace on a copy of the smoke projects**

Use a temporary `HOME` so the check never touches the user's real registry or progress. `serve` doesn't call Claude, so a temporary `HOME` is safe.

```bash
cd /Users/rohitkk074/Documents/projects/tattva && go build -o tattva .
CHECK=$(mktemp -d) && mkdir -p "$CHECK/home" "$CHECK/projects"
cp -R ~/tattva-smoke/redis ~/tattva-smoke/git ~/tattva-smoke/sql-db-v2 "$CHECK/projects/"
for d in redis git sql-db-v2; do (cd "$CHECK/projects/$d" && HOME="$CHECK/home" /Users/rohitkk074/Documents/projects/tattva/tattva status > /dev/null); done
HOME="$CHECK/home" /Users/rohitkk074/Documents/projects/tattva/tattva list
```
Expected: `list` shows Mini Git, Mini Redis and Mini SQLite, all at 0%. Then start the server in the background (Bash tool `run_in_background: true`):
```bash
HOME="$CHECK/home" /Users/rohitkk074/Documents/projects/tattva/tattva serve --port 4848
```

- [ ] **Step 3: Check the pages in a real browser**

Use the Chrome DevTools MCP tools; load them with ToolSearch (`chrome-devtools`) if they're deferred. If no browser tools are available, do what you can with `curl -H 'Host: 127.0.0.1:4848'`, and report which checks were manual-only. Save screenshots in the session scratchpad.

1. **Landing page:** open `http://127.0.0.1:4848/`. Expect three cards with `0/18`, `0/15` and `0/22`, the top bar with `[tattva]`, the theme dropdown, `[dark]` and the window list. Take a screenshot.
2. **Overview:** click the Mini Redis card. Expect the overview, with `document.querySelector("pre.mermaid svg") !== null`: the diagram rendered. Take a screenshot.
3. **A step:** click step 01 in the steps pane.
   - Click `[ show hint 1 ]`. Expect hint 1's text and `[ show hint 2 ]`, and `1 hint` next to step 01 in the steps pane.
   - Click `[ mark done ]`. Expect `✓ done` and step 01 marked `✓`.
   - Run `HOME="$CHECK/home" tattva status` in `$CHECK/projects/redis`. Expect `✓ 01 … (1 hint)`.
4. **Theme:**
   - Select `tokyo-night` in the dropdown. Expect `document.documentElement.dataset.theme === "tokyo-night"` and a redrawn diagram on the overview.
   - Click `[dark]` so it shows `[light]`. Take a screenshot.
   - Reload. Expect the theme and mode to have survived.
5. **Divider:** focus the divider and press ArrowRight 5 times. Expect the steps pane about 100px wider.
   - Then drag it, or press the key many times, past half the window. Expect the width to stop at `innerWidth / 2`.
   - Press ArrowLeft many times. Expect it to stop at 200px.
   - Reload. Expect the width to be remembered.
6. **Console:** list the console messages. There should be no errors, and in particular no `Content-Security-Policy` violations. If mermaid is blocked by the policy, loosen the policy by the smallest possible change. Update `TestServerSetsSecurityHeaders` and the spec's Security section to match, and explain why in the commit message.

- [ ] **Step 4: Clean up**

Stop the background server, then `rm -rf "$CHECK"`. Check that the user's real `~/tattva-smoke` projects have no new `progress.jsonl` files: `ls ~/tattva-smoke/*/curriculum/progress.jsonl` should report none.

- [ ] **Step 5: Run the full suite and commit**

Run: `gofmt -l . && go vet ./... && go test -race ./...`
Expected: no gofmt output, and PASS.

```bash
git add README.md
git commit -m "docs: describe install, the CLI and the workspace"
```
If the browser check led to code fixes, commit each fix separately with its test, before the README commit.
