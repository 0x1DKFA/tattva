# Tattva: Workspace Design (sub-project 2)

**Date:** 2026-09-27
**Status:** Draft
**Parent spec:** [docs/specs.md](../../specs.md), the product vision. `§N` refers to its sections.
**Builds on:** [2026-09-24-curriculum-engine-design.md](2026-09-24-curriculum-engine-design.md) (sub-project 1). The CLI, the Learning Spec, the progress log and the registry described there are unchanged unless this document says otherwise.

## Goal

A local web UI for reading curricula and tracking progress, started with `tattva serve`. It works over the same files the CLI uses (`curriculum/spec.json`, `curriculum/progress.jsonl`, `~/.tattva/projects.json`), so the browser and the terminal never disagree.

**Read and track only.** Creating, revising and expanding curricula stays in the CLI. The tutor ("Need help?") and the verifier ("Run verification") from §29 are sub-projects 3 and 4.

## Decisions

These were settled while brainstorming:
- **Scope:** reading and tracking, with no generation from the browser.
- **Hint tracking:** opening a hint in the workspace is recorded as a progress event. This is the first evidence of independence (§33, §34). Hints in the step markdown files stay untracked.
- **How pages are built:** Go `html/template` pages served by `tattva serve`, with markdown rendered by goldmark (the project's first third-party module) and diagrams drawn by a copy of mermaid.js built into the binary. Buttons are plain form posts. There's no JavaScript build step.
- **Layout:** a landing page of project cards, then a project page with a steps pane on the left and a content pane on the right, split by a draggable divider.
- **Look:** a terminal theme. Panes are tiled with gaps and rounded borders like Hyprland, pane titles sit in the border like tmux, and a tmux-style status bar runs along the top. Catppuccin Mocha is the default. A theme picker and a dark/light switch sit in the top-left corner. Every colour comes from a CSS variable, so adding a theme is easy.

## Usage

```
$ tattva serve              # http://127.0.0.1:4747/, opens your browser
$ tattva serve --port 5000
```

`serve` prints the address and opens it in the default browser: `open` on macOS, `xdg-open` elsewhere. If opening fails, the printed address still works. Ctrl-C stops the server. If the port is taken, the command exits with an error that suggests `--port`.

## Pages

### Top bar (every page)

```
[tattva] theme: [catppuccin ▾] [dark]   0:projects  1:mini-redis*  2:mini-git  3:mini-sqlite     7/18 ✓ · 2 hints
```

- **Theme controls:** a theme dropdown and a `[dark]`/`[light]` toggle.
- **Project list:** it works like tmux's window list. `0:projects` is the landing page, and each registered project is `N:<slug>`, numbered in name order. The slug is the project name in lowercase with dashes, such as `mini-redis`. The current project is highlighted and marked `*`.
- **Progress:** on a project page, the right end shows done/total and the number of hints used.

### Landing page (`/`)

One pane per registered project, tiled in a grid:

```
╭─ 1:mini-redis ─────────╮
│ Mini Redis             │
│ Redis · go             │
│ [███████░░░░░] 7/18    │
│ → 08 Key expiry        │
╰────────────────────────╯
```

- **What a card shows:** the name, the target and language, a 12-cell text progress bar with done/total (plus hints used, if any), and what the project is up to: `→ NN Title`, `all done` or `no step in progress`.
- **Problem projects:** a project whose folder is missing, or whose `spec.json` can't be read or breaks the rules, shows as a dimmed card with the reason. It isn't a link.
- **No projects:** the page explains how to start one with `tattva new`.

### Project page (`/p/<id>` for the overview, `/p/<id>/s/<n>` for step n)

```
╭─ steps ────────────────────╮  ╭─ 08-key-expiry.md ──────────────────────────╮
│ ▸ overview                 │  │ # 08 · Key expiry                           │
│ setup                      │  │ phase 3: keys · concepts: ttl · after: 07   │
│   ✓ 01 create run.sh       │  │ [ start step ]  [ mark done ]               │
│ protocol                   │  │ ## goal ...                                 │
│   → 08 key expiry  2 hints │  │ ## hints                                    │
│   ○ 09 lists               │  │ [ show hint 1 ]                             │
│   · 10 blocking pops       │  │                                             │
╰────────────────────────────╯  ╰─────────────────────────────────────────────╯
```

- **Left pane (`steps`):**
  - **Overview** comes first, then the phases with their steps.
  - Steps are marked `✓` done, `→` in progress, `○` available, `·` locked, with the hint count once any hint has been used.
  - A step that isn't expanded yet is listed dimmed, isn't a link, and has a tooltip saying to run `tattva expand`.
  - The selected item is highlighted and scrolled into view.
- **Divider:** drag it to set the left pane's width, from 200px up to half the window. With keyboard focus, the arrow keys move it 20px at a time. The browser remembers the width. Below 800px wide, the panes stack and the divider is hidden.
- **Right pane:**
  - **Overview:** the pane is titled `README.md`. It shows:
    - the curriculum's name and summary, target and language
    - assumptions, what's in and out of scope
    - how the real system is built, and what you'll build
    - the architecture diagram
    - the program contract, the concept glossary and the references
  - **Step:** the pane is titled with the step's file name, such as `08-key-expiry.md`. It shows:
    - the step heading and a line with phase, concepts and prerequisites
    - the buttons
    - goal, why, context, your task, constraints
    - check it: the by-hand instructions, the expected outcome, and the machine checks in a folded section
    - hints, common mistakes, reflect, unlocks
    - the curriculum's references, as links
  - **Problem:** an `error` pane with the reason.
- **Buttons:**
  - `[ start step ]` appears only on an available step.
  - `[ mark done ]` appears on any step that isn't done, as in the CLI. If prerequisites aren't done, a note lists them. A done step shows `✓ done`.
- **Hints:**
  - Hints open one at a time, in order. `[ show hint 1 ]` records the event and reveals hint 1. Then `[ show hint 2 ]` appears, then hint 3.
  - Hints not yet reachable say they open after the hint above.
  - Opened hints stay open on later visits.
  - The pseudocode hint is shown in a code block, the same rule the markdown renderer follows.

### Look

- **Panes:**
  - Rounded borders with gaps between them.
  - The pane under the mouse, or holding keyboard focus, gets the active border colour; the others use the normal border colour.
  - Pane titles sit in the top border.
- **Text:**
  - Monospace everywhere, from fonts already installed (no web fonts, so it works offline).
  - Markdown looks like a terminal viewer: headings in the accent colour prefixed with `#`, `##` or `###`, code in darker boxes, underlined links.
- **Controls:** buttons are bracketed text such as `[ mark done ]`.
- **Architecture diagram:** mermaid's `base` theme, fed the current theme's variables, redrawn when the theme or mode changes.

## Themes

- **Variables:** the stylesheet uses these variables for every colour:
  - `--bg`: behind the panes
  - `--surface`: pane background
  - `--surface-2`: code blocks and the top bar
  - `--selection`
  - `--text`, `--subtext`, `--dim`
  - `--border`, `--border-active`
  - `--accent`, `--link`
  - `--done`, `--current`, `--warning`

  No colour is written anywhere else in the CSS.
- **Theme blocks:** a theme is a pair of blocks, `:root[data-theme="<name>"][data-mode="dark"]` and `…[data-mode="light"]`. Each sets all of the variables. The blocks sit between `/* === themes === */` markers at the top of `web/static/app.css`.
- **Initial themes:**
  - `catppuccin`: Mocha (dark) and Latte (light). The default.
  - `tokyo-night`: Night and Day.
  - `gruvbox`: dark and light.
- **Adding a theme:** add its two blocks and its name to the `themes` list in `web.go`. The dropdown is built from that list. Tests enforce all of this:
  - no colour appears outside the theme blocks
  - every theme in the list has a dark and a light block
  - every block sets the same variables
  - every variable the CSS uses is defined
- **Remembering the choice:** the browser stores the theme, mode and pane width in `localStorage`. A small script in `<head>` applies them before the page draws, so it never flashes the wrong colours. A stored theme that no longer exists falls back to the default. If storage is unavailable, the defaults are used.

## Data and behaviour

- **Always current:** every request re-reads the registry, `spec.json` and `progress.jsonl`, so anything the CLI changes shows up on the next page load. There's no live refresh.
- **Viewing never writes:** GET requests never write files. Pages use read-only loading, not the CLI's project opening, which updates the registry.
- **Step content:** built from `spec.json`, not from the step markdown files.
- **Buttons:** each is a POST to `/p/<id>/s/<n>/<action>`, answered with a redirect back to the step (a `303` redirect back, so reloading doesn't resubmit). They go through the same project code as the CLI, so file hashes stay correct.
  - `start` adds `started`, only when the step is available.
  - `done` adds `completed`, unless the step is already done.
  - `hint` with form field `level=N` adds `{"event": "hint", "level": N}`, only when N is the next hint to open. Anything else does nothing.
  - An unknown project, a step that isn't expanded, or an unknown action returns 404.
- **Progress log:** events gain an optional `level` field, used by `hint`. Status is still worked out only from `started` and `completed`. The number of hints used on a step is the highest level opened.
- **CLI changes:**
  - `tattva status` shows `(2 hints)` next to steps where hints were used.
  - `tattva list` and the landing page share one read-only loader. That also fixes the deferred minor where `list` stopped entirely when one project's progress file was unreadable.

## Security

The workspace only listens on this machine, but a web page open in the same browser can still send requests to `127.0.0.1`.
- **Localhost only:** the server listens on `127.0.0.1` only.
- **Host check:** requests whose `Host` isn't `127.0.0.1:<port>` or `localhost:<port>` get 403. This stops DNS rebinding.
- **No cross-site posts:** Go 1.25's `http.CrossOriginProtection` rejects posts from other sites, so another page can't mark steps done or open hints.
- **Browser headers:** `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`, plus `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. `'unsafe-inline'` styles are needed for the SVG mermaid draws. No script is inline.
- **Claude's text is untrusted:**
  - Markdown is rendered by goldmark with raw HTML off and dangerous link targets dropped.
  - `html/template` escapes everything else.
  - References become links only if they're `http` or `https`.
  - Mermaid runs with `securityLevel: "strict"`.
- **No login:** it's single-user and local.

## Code layout

```
web.go                   the web/ files built into the binary, the themes list
serve.go                 tattva serve: server, routes, security, page data
markdown.go              markdown to HTML with goldmark
web/templates/base.html  page frame and top bar
web/templates/home.html  landing page
web/templates/project.html  steps pane, overview, step view
web/static/app.css       themes and layout
web/static/theme-boot.js applies the stored theme before the page draws
web/static/app.js        theme picker, divider, diagrams
web/static/mermaid.min.js, mermaid.LICENSE   bundled mermaid 11 (MIT)
```

Changes to existing files:
- `progress.go`: hint events and the hint count; `status` shows it.
- `registry.go`: the shared read-only project loader; `list` uses it.
- `render.go`: the step view data is extracted into a function both renderers share.
- `main.go`: the `serve` command.

## Testing

`go test ./...` stays offline and never calls Claude or opens a browser.
- **Progress:** hint levels are counted, status ignores hint events, and `status` prints the hint count.
- **Markdown:** raw HTML and `javascript:` links from Claude never reach the page, and normal markdown renders.
- **Themes:** the four CSS rules listed under Themes.
- **Server:** other hosts get 403, cross-site posts get 403, security headers are set, static files are served with the right types, a busy port gives an error suggesting `--port`, and `serve` stops when cancelled.
- **Pages:**
  - The landing page shows cards with progress, problem cards and the empty state.
  - The overview shows the diagram, the scope and the references.
  - The step view shows every section, the buttons that match its status, and hints revealed according to the log.
  - Claude's HTML and `javascript:` links are escaped.
  - Unexpanded and unknown steps get 404.
  - A broken spec shows the error pane.
- **Actions:**
  - `start` and `done` add events only when allowed.
  - Hints can only be opened in order, and a repeat adds nothing.
  - Every action redirects back to the step.
  - Viewing pages leaves the registry, spec and progress files byte-for-byte unchanged.
- **Browser check** (manual or with Chrome DevTools, not part of `go test`), using the three smoke-test curricula:
  - the landing page, project, overview diagram and a step
  - opening hints and marking a step done, then checking `tattva status` agrees
  - switching theme and mode, and the choice surviving a reload
  - dragging the divider to its limits
  - no errors in the browser console, including content-security-policy violations

## Success criteria

1. `tattva serve` shows every registered project as a card with the right done/total.
2. The project page shows the overview with its diagram, and each expanded step with folded machine checks and step-by-step hints.
3. Starting, finishing and opening hints in the browser update `progress.jsonl`, and `tattva status` shows the same progress and hint counts.
4. The theme picker switches between the three themes in both modes, and the choice survives a reload. The CSS tests prove every colour comes from a variable.
5. The divider resizes the steps pane between 200px and half the window, and the width is remembered.
6. `go test ./...` passes offline, and viewing pages never writes a file.

## Out of scope

- generating curricula from the browser
- tutor and verifier actions
- goals and the concept graph
- keyboard navigation between steps
- live refresh
- login or remote access
- editing curricula in the browser
- undoing a completed step

## Risks

1. **Mermaid under the content-security policy:** mermaid may need more than `'unsafe-inline'` styles. The browser check watches the console, and the policy is loosened only by the minimum needed.
2. **Bundle size:** mermaid adds about 3 MB to the repository and the binary. That's the price of offline diagrams.
3. **Other host names:** addresses like `http://[::1]:4747` get 403. Use `127.0.0.1` or `localhost`.
4. **Mermaid and goldmark versions:** both are pinned in the repository (`go.mod`, and the vendored mermaid file), so upgrades happen deliberately.
