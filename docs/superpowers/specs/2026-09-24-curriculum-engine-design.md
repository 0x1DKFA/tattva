# Tattva: Curriculum Engine Design (sub-project 1)

**Date:** 2026-09-24
**Status:** Draft, awaiting review
**Parent spec:** [docs/specs.md](../../specs.md), the product vision (v0.1). In this document, `§N` always refers to a section of the parent spec. Sections of this document are referred to by name.

## Goal

A user names a system they have used but couldn't build (Redis, Git, Postgres…). Tattva turns it into a curriculum in the style of build-your-own-x: small, ordered steps, each with enough detail for the user to implement it themselves. The result is a miniature version built for learning, not a clone. Claude designs the curriculum; the user writes all the code.

This spec covers the first of four sub-projects: **generate a curriculum and work through it from the terminal.**

| # | Sub-project | Status |
|---|---|---|
| 1 | Curriculum engine + terminal progress (this spec) | designing |
| 2 | Workspace: a local web UI over the same files | later |
| 3 | Tutor: AI actions on each step (hint ladder, explain, review my code), with assistance tracking | later |
| 4 | Verifier: runs step checks against the user's program, limited to commands the user allows | later |

Goals, the concept graph, git-history evidence and mastery scoring (§18–21, §28, §33) come after these four.

## Assumptions

- A single user on their own machine; no accounts.
- Go 1.25, standard library only. The Claude Code CLI (`claude`, version 2.1.281 at the time of writing) is the only AI backend, run as a subprocess.
- Input is a free-text target plus the language the user will build in. Analysing an existing repository (§6 Types A and B) is out of scope.
- The program the curriculum builds is either a **server** (reached over TCP) or a **CLI** (run with arguments and stdin). Targets that fit neither are out of scope for v1.

## Usage

Tattva runs in the folder the user builds in, like `git init`. All state is files, and every command can be re-run.

```
$ mkdir mini-redis && cd mini-redis
$ tattva new "Redis" --lang go                          # design call → outline
$ tattva revise "skip persistence, go deeper on RESP"   # optional, repeatable
$ tattva expand                                         # step details + step files; re-run resumes
$ tattva next                                           # start the next unlocked step
$ tattva done                                           # complete the current step
$ tattva status                                         # curriculum view with progress
$ tattva list                                           # all projects on this machine
```

The target is passed to Claude word for word, so it can include a focus or the learner's background: `tattva new "Postgres, focus on storage and MVCC; I've never written a parser" --lang go`.

Commands that work on a project look for `curriculum/spec.json` in the current directory and then in its parents, the way git finds `.git`. The **project root** is the directory that contains `curriculum/`.

### Commands

| Command | Behaviour |
|---|---|
| `new "<target>" --lang <lang> [--model <m>]` | Fails if `curriculum/spec.json` already exists in the current directory. Runs the design call, writes `spec.json` and `README.md`, and registers the project. Prints a summary (phases, number of steps, warnings, time, cost) and suggests reviewing `README.md`. |
| `revise "<feedback>" [--force] [--model <m>]` | Rewrites the outline from the current one plus the feedback. Refuses if any step is expanded, unless `--force` is given. `--force` drops all step details and deletes every step file, including edited ones. Keeps `project.id` and `progress.jsonl`. |
| `expand [--model <m>]` | Runs one expand call for each phase that has unexpanded steps, up to 3 at a time. Saves each phase as soon as it finishes and writes its step files. Lists any phases that failed and exits with a non-zero status. Re-running retries only what is missing. If every step is already expanded, it says so and does nothing. |
| `next` | If a step is in progress, prints it. Otherwise, marks the first available step (in build order) as started and prints its number, title and file path. If that step isn't expanded yet, it tells the user to run `expand` and does not start it. When every step is done, it says so. |
| `done [step]` | Marks the current step completed, or the given one. `step` is a number (the step's position in build order) if it is all digits, otherwise a step id. Completing a step whose prerequisites aren't done is allowed, with a note. Completing an already completed step does nothing. |
| `status` | Shows phases and steps marked `✓` done, `→` in progress, `○` available or `·` locked, plus the percentage complete, change-detection notices and validation warnings. |
| `list` | Shows every registered project: name, path, percentage complete, current step, and last activity (the time of the last progress event, or `spec.json`'s modification time if there are none). Marks projects whose folder is missing. |

`--lang` is required on `new`. `--model` is passed to `claude` unchanged; without it, Claude Code's default model is used. There are no other flags.

### Project layout

```
mini-redis/                  ← the user's project; their code lives here too
  run.sh                     ← written by the user in step 1 (the program contract)
  curriculum/
    spec.json                ← the Learning Spec, the source of truth
    progress.jsonl           ← progress events (created on the first event)
    README.md                ← generated: scope, architecture, program contract, step list
    steps/
      01-create-run-script.md
      02-accept-a-connection.md
      …
    .failed/                 ← outputs that failed validation twice (only created if that happens)
```

Step files are named `NN-<step id>.md`. `NN` is the step's position in build order, starting at 1 and padded to two digits. Only expanded steps have a file.

## Learning Spec (`curriculum/spec.json`)

This is the contract between Claude and tattva described in §24. The design and revise calls produce the **outline**: everything except `detail`, `project.id`, `project.target`, `project.language` and `schema_version`. Expand calls produce `detail`. Tattva sets the rest itself.

```jsonc
{
  "schema_version": 1,
  "project": {
    "id": "<uuid v4, set by tattva>",
    "name": "Mini Redis",
    "target": "Redis",              // the user's input, set by tattva
    "language": "go",               // --lang, set by tattva
    "summary": "…",
    "assumes": ["comfortable with Go", "no networking background needed"]
  },
  "scope": {
    "in": ["TCP server", "RESP", "GET/SET/DEL", "expiry"],
    "out": [{ "feature": "persistence", "why": "file-I/O detour; good follow-up" }]
  },
  "architecture": {
    "real": "<markdown: how the production system is built>",
    "components": [{ "id": "parser", "responsibility": "…", "talks_to": ["store"] }],
    "diagram": "<Mermaid flowchart source for the simplified architecture>"
  },
  "program": { "kind": "server", "port": 6379 },
  "concepts": [{ "id": "framing", "name": "Message framing", "summary": "…" }],
  "references": [{ "title": "RESP protocol spec", "url": "https://…" }],
  "phases": [{ "id": "networking", "title": "Networking", "purpose": "…" }],
  "steps": [{
    "id": "respond-to-ping",
    "phase": "networking",
    "title": "Respond to PING",
    "goal": "…",
    "concepts": ["framing"],
    "prerequisites": ["accept-a-connection"],
    "detail": null
  }]
}
```

- `architecture.real`, next to `components` and `diagram`, separates the real architecture from the simplified one the user builds (§43, decision 3).
- `project.assumes` lists what the curriculum expects the learner to already know. This covers the knowledge prerequisites in §14. The user corrects it with `revise`.
- `prerequisites` holds step ids only. "Unlocks" is worked out from them, not stored.
- `references` are collected while the design call researches the target. Expand calls may fetch them again.
- `detail` is `null` until the step is expanded.

### Step detail

```jsonc
"detail": {
  "why": "…",
  "context": "…",                   // what exists so far and how this step fits the architecture
  "task": "…",                      // markdown
  "constraints": ["standard library only"],
  "hints": ["<nudge>", "<specific>", "<pseudocode>"],
  "checks": [ /* Check */ ],
  "by_hand": "…",                   // markdown: commands to run yourself and what you should see
  "expected_outcome": "…",
  "common_mistakes": ["…"],
  "reflection": "…"                 // one question
}
```

The three hints are levels 2–4 of the assistance ladder in §15: a conceptual nudge, a specific hint, then pseudocode. Levels 5–6 (partial and full implementation) belong to the tutor and are never generated in advance.

### Checks

```jsonc
{
  "name": "replies PONG",
  "do": "tcp",                      // exec | tcp | write | file
  "args": [],
  "input": "*1\r\n$4\r\nPING\r\n",
  "path": "",
  "expect": "+PONG\r\n",
  "match": "exact",                 // exact | contains | regex
  "exit_code": 0
}
```

Every field is always present. Fields a check doesn't use are left empty (`""`, `[]`, `0`). `match` is ignored when `expect` is empty or not used.

| `do` | Meaning | Fields used |
|---|---|---|
| `exec` | Runs `run.sh` with `args`, sending `input` on stdin. Checks that the exit status equals `exit_code` and, if `expect` isn't empty, compares stdout to `expect`. CLI programs only. | `args`, `input`, `expect`, `match`, `exit_code` |
| `tcp` | Opens a new connection to `127.0.0.1:<port>`, sends `input`, and reads until the reply satisfies `expect` or a timeout hits. Server programs only. | `input`, `expect`, `match` |
| `write` | Writes `input` to `path` in the scratch directory, to set up a fixture. Checks nothing. | `path`, `input` |
| `file` | Checks that `path` exists in the scratch directory and, if `expect` isn't empty, compares its content to `expect`. | `path`, `expect`, `match` |

`regex` uses Go's RE2 syntax and can match anywhere in the text unless the pattern is anchored.

**Execution contract.** Generated checks follow this contract. The verifier (sub-project 4) implements it, including timeouts and asking the user for approval.
- The user's `run.sh` sits at the project root. It builds the program if needed, runs it, and passes its arguments through. Step 1 of every curriculum creates it.
- Each step's checks run in order, in a fresh empty scratch directory that is `run.sh`'s working directory. `run.sh` is called by its absolute path, so a mini-git's `init` never touches the user's repository.
- For servers, the verifier starts `run.sh` with no arguments, waits until the port accepts connections, runs the step's checks, then stops it. State carries across one step's checks (SET then GET works) but not across steps.
- For CLI programs, each `exec` check runs `run.sh` once. The scratch directory carries across one step's checks.

### Validity rules

Tattva checks these rules after every Claude call and every time it loads `spec.json`. If Claude's output breaks a rule, tattva makes one repair call (see Generation pipeline). If a hand edit breaks a rule, the command stops and shows the errors. Warnings are printed but don't stop anything.

Errors in the outline:
1. Every id (phases, steps, concepts, components) is kebab-case (`^[a-z0-9]+(-[a-z0-9]+)*$`) and unique among ids of its kind.
2. There is at least one phase, every phase has at least one step, and there are at most 99 steps.
3. Every `step.phase` names a declared phase. A phase's steps sit next to each other in the list, and phases appear in the order they are declared.
4. Every prerequisite names a step that comes earlier in the list. This rules out circular dependencies and makes list order the build order.
5. Every step has at least one concept, every concept a step uses is declared, and every declared concept is used.
6. A step introduces at most 2 concepts that no earlier step uses. This is the "one conceptual leap" rule from §13, and it enforces stage 6 of §23 in code.
7. Every entry in `components[].talks_to` names a declared component.
8. `program.kind` is either `server` with `port` between 1024 and 65535, or `cli` with `port` 0.
9. `schema_version` is 1.

Errors in expanded steps:
10. `why`, `context`, `task`, `by_hand`, `expected_outcome` and `reflection` are not empty. There are exactly 3 non-empty hints and at least one common mistake.
11. Checks make sense for their type: `exec` only for CLI programs; `tcp` only for servers, and only with non-empty `input`; `write` and `file` need a relative `path` with no `..` segment; unused fields are empty; `regex` patterns compile; `input` and `expect` are plain text, with no control characters other than tab, newline and carriage return (a hand-written binary fixture goes back to Claude for repair).
12. An expand call returns exactly the step ids it was asked to fill.

Warnings:
- An expanded step has no checks. Some behaviour, such as handling several clients at once, can't be checked from outside.

Known limits: check input and output are text only, so binary protocols will need a hex field later. A check can't open two connections at the same time.

## Generation pipeline

```
new     → design call (web research) → validate → [one repair] → spec.json + README.md
revise  → revise call (outline + feedback) → validate → [one repair] → spec.json + README.md
expand  → one call per phase with unexpanded steps, 3 at a time → validate each → [one repair] → merge → step files
```

| Call | Prompt (sent on stdin) | Tools | Output schema |
|---|---|---|---|
| design | target, language | WebSearch, WebFetch | outline |
| revise | target, language, current outline, feedback | WebSearch, WebFetch | outline |
| expand | language, full outline, phase id, the step ids to fill | WebSearch, WebFetch | an object with `steps`, each entry being `{id, detail}` |
| repair | the original request, the output that failed, the rule errors | none | same as the call that failed |

If a call's output still breaks the rules after its one repair, the output and its errors are saved to `curriculum/.failed/<call>-<timestamp>.json` and the command stops. For `expand`, only that phase fails.

### Claude adapter

A single Go function runs one call. There is no interface layer. §25 asks that the product not be tied to Claude, and the Learning Spec contract already takes care of that. A second AI backend would be a second function.

```
claude -p
  --output-format stream-json --verbose
  --json-schema <schema>
  --system-prompt <built-in prompt for this call>
  --tools WebSearch,WebFetch            # "" for repair
  --allowedTools WebSearch,WebFetch
  --safe-mode
  --no-session-persistence
  [--model <m>]
```

- The prompt goes to stdin. The process runs in a fresh temporary directory, so Claude can't see or change the user's files.
- `--safe-mode` skips the user's CLAUDE.md, hooks, plugins and MCP servers. Otherwise they would get mixed into generation; this machine has global SessionStart hooks. `--no-session-persistence` keeps generation runs out of the user's session history.
- Each call has a 20-minute timeout. Ctrl-C, or `kill` (SIGTERM), cancels every running call and reports it as interrupted; a second signal exits at once.
- If `claude` exits with a non-zero status or returns an error result, tattva shows Claude's message. Tattva doesn't retry, because Claude Code already retries API errors.
- The result's `total_cost_usd` is printed for each call and totalled for each command.
- The output is read line by line as Claude works. Its searches and the pages it reads show in the terminal as they happen. `new`, `revise` and `expand` each write a log to `~/.tattva/logs/<time>-<command>-<pid>.log`, and the first line of output says which file. The log records every call with its model and outcome, what Claude said, searched for and read, tool errors, Claude's stderr, rule checks and repairs, cost, and how the command ended. Failing to create the log doesn't stop the command. Tattva never deletes old logs.

### Schemas

Two schemas are sent to Claude: the outline, and the details for one phase. Both are generated from the Go types by a small reflection helper, so they can't drift from the code. Every property is required and every object sets `additionalProperties: false`. Enums cover `program.kind`, `check.do` and `check.match`. Rules a schema can't express, such as counts, references and ordering, are covered by the validity rules.

### Prompt rules

The two system prompts, `prompts/design.md` and `prompts/expand.md`, are built into the binary. They carry the teaching approach.

Design prompt (also used by revise):
- Choose the smallest subset that still teaches the core ideas: as many concepts as possible for as little implementation as possible (§7). Say what is out of scope and why.
- Describe the real architecture, then the simplified one the learner will build.
- Step 1 always has the learner create `run.sh` and a minimal program for it to run. Every later step adds behaviour you can see by running `run.sh`, so it can be checked from outside.
- One conceptual leap per step, about 30–90 minutes of work. No more than 2 new concepts per step.
- A concept is an idea worth a glossary entry, like "framing" or "B-tree page split", not a function call. Concept ids stay generic rather than project-specific, so a future concept graph can link them across projects.
- Research the real system and its protocols with the web tools, and record the sources in `references`.
- Standard library only, unless the target really needs something else.
- State the assumptions about the learner in `project.assumes`.

Expand prompt:
- Fill in `detail` for exactly the steps requested.
- Describe behaviour, never internal function names or structure; the learner designs the internals. This is also what keeps phases expanded in parallel consistent with each other: none of them prescribes internals another could contradict, and checks only test what can be observed from outside.
- Hints go from a nudge, to a specific hint, to pseudocode. Never solution code.
- Checks must match the referenced specs byte for byte. Fetch the references when unsure.
- `by_hand` uses common tools where possible (redis-cli, nc, curl, git).
- Constraints and hints are written for the chosen language.

## Progress

Progress is stored in `curriculum/progress.jsonl`. Tattva only appends to it, one event per line:

```
{"at":"2026-09-24T10:02:11Z","step":"respond-to-ping","event":"started"}
{"at":"2026-09-24T10:49:40Z","step":"respond-to-ping","event":"completed"}
```

- This sub-project writes two event types: `started` and `completed`. Later sub-projects add new types to the same file (`hint`, `check_passed`, `check_failed`, `reflected`, …), which provides the evidence described in §28, §33 and §34. Event types tattva doesn't recognise are ignored when working out status.
- A step's status is worked out from the log:
  - **done:** it has a `completed` event.
  - **in progress:** it has a `started` event but no `completed` event.
  - **available:** it has neither, and all its prerequisites are done.
  - **locked:** anything else.

  The percentage complete is done steps divided by all steps.
- The *current* step is the in-progress step that was started most recently.
- Events for step ids that aren't in the spec (possible after `revise --force`) are ignored with a warning. So is a half-written last line.
- The file lives inside the project, so it moves with it. If it is committed alongside the code, git history shows each step completing next to the code that completed it.

This replaces SQLite for progress (§26). SQLite can come back later as an index rebuilt from these files, if cross-project features need one.

## Registry and change detection

The project folder holds what belongs to the user: the curriculum and their progress. `~/.tattva/projects.json` holds tattva's own bookkeeping:

```json
{
  "projects": [{
    "id": "<uuid>",
    "path": "/Users/…/mini-redis",
    "name": "Mini Redis",
    "files": {
      "curriculum/spec.json": "<sha256>",
      "curriculum/steps/01-create-run-script.md": "<sha256>"
    }
  }]
}
```

- **Registration:** `new` registers the project. Every command run inside a project (all except `list`) updates its entry by id, so a moved folder is picked up automatically. An existing entry with the same path but a different id is replaced. Each registry write takes a lock on `~/.tattva/projects.lock`, re-reads the file and changes only the current project's entry, so runs in different projects at the same time keep every entry.
- **Hashes:** after every write to `spec.json`, `progress.jsonl`, `README.md` or a step file, tattva stores that file's SHA-256 hash.
- **Checking:** every command run inside a project compares the current files with the stored hashes:
  - **`spec.json` changed:** it is validated. If it breaks a rule, the command stops and shows the errors. If it's valid, tattva accepts it as the user's edit, reports it once and updates the hash.
  - **`progress.jsonl` changed:** accepted, reported once, hash updated.
  - **A generated markdown file changed:** `status` reports it, and tattva never overwrites it while it differs from its stored hash. To get the generated version back, the user deletes the file.
- **Rendering** runs at the end of every command inside a project:
  - Missing generated files are written.
  - Files tattva wrote and nobody has changed are rewritten if their content should change.
  - Edited files are left alone and reported.
  - Unchanged files for steps that no longer exist, or are no longer expanded, are deleted.
  - `revise --force` deletes every step file, edited or not.
- **No stored hash** (a new machine or a lost registry): a file counts as unchanged only if it matches what tattva would generate right now.
- **Unreadable registry:** treated as empty, with a warning.
- **Purpose:** this catches accidents, like Claude Code editing curriculum files while it helps the user build, and makes edits visible. It is deliberately not tamper-proofing (§34).

## Rendering

Files are generated with `text/template`, with the templates built into the binary.
- **`README.md`:** name, summary, target and language, assumptions, what's in and out of scope, the real architecture, the simplified components with the Mermaid diagram (in a ```` ```mermaid ```` block), the program contract, a glossary of concepts, references, and the phases with their numbered steps (title, goal, and prerequisites by number).
- **Step file:** a `# NN · Title` heading and a line with the phase, concepts and prerequisites (by number). Then these sections:
  - Goal
  - Why
  - Context
  - Your task
  - Constraints
  - Check it: the by-hand instructions, then the machine checks in a folded `<details>` block (command arguments shell-quoted, payloads over 120 bytes shortened, regexes shown as written)
  - Hints: three separate folded `<details>` blocks; the pseudocode hint goes in a text code block when it isn't fenced already
  - Common mistakes
  - Reflect
  - Unlocks: worked out from the other steps' prerequisites
- **Header comment:** every generated file starts with a comment saying it is generated from `spec.json`, and that edits are detected and won't be overwritten.

## Failure handling

- `spec.json`, the registry and the generated files are written safely: to a temporary file first, then renamed into place. `progress.jsonl` is only appended to.
- `expand` merges and saves each phase as it finishes, under a single mutex. A crash or Ctrl-C keeps the phases already finished, and re-running retries only what's missing. When one phase fails, the others continue. The summary lists the failures and the exit status is non-zero.
- Running two tattva commands on the same project at the same time isn't supported; the last write wins. The code marks this as a known limit.

## Code layout

One Go module, package `main`, standard library only, built as a single `tattva` binary with `go build` or `go install`.

```
main.go       command dispatch and flags: new, revise, expand, status, next, done, list
spec.go       Learning Spec types, Validate(), schema generation
claude.go     the function that runs claude -p
runlog.go     the per-command log in ~/.tattva/logs
generate.go   design / revise / expand flow, repair, merging phases
render.go     README and step markdown (templates built in)
progress.go   appending to progress.jsonl, working out status
registry.go   ~/.tattva/projects.json, file hashes
prompts/      design.md, expand.md (built in)
```

## Testing

`go test ./...` runs offline and never calls Claude.
- **Validation:** one valid spec, plus one broken copy for each rule.
- **Rendering:** `README.md` and one step file are compared against saved expected output.
- **Adapter:** a fake `claude` script placed first on `PATH` records its arguments and stdin. It replies with canned stream-json lines: a success with searches, a page read and a tool error, an error result, malformed output, or a hang (to test the timeout). The success case also checks what reaches the log and the terminal.
- **Generation flow, using the fake:** phases merge; `expand` resumes only the missing phases; finished phases survive when one phase fails; repair runs at most once.
- **Progress:** working out status, what `next` picks, edge cases for `done`, and skipping a half-written last line.
- **Change detection:** an edited step file isn't overwritten; a `spec.json` edit that breaks a rule stops the command; a valid edit is accepted.

A manual smoke run generates the three curricula named in the success criteria. It spends tokens, so it isn't part of `go test`.

## Success criteria

1. Curricula for Redis, Git and a small SQL database, all in Go, pass validation with at most one repair per call.
2. Targets to measure: the design call takes about 5 minutes or less, and `expand` about 15 minutes or less for a curriculum the size of Redis. If not, adjust how many phases expand at once.
3. Reading the three outlines, the user finds the order sensible, each step a single conceptual leap, and the scope a real miniature version.
4. The user works through the first ~5 steps of one curriculum using only the step files. `status`, `next` and `done` track them correctly, and the checks look believable.
5. `go test ./...` passes offline.

## Out of scope

- The web workspace
- Running checks
- The tutor, and revealing solutions
- Analysing existing repositories
- Goals, the concept graph, and SQLite
- Starter code and reference solutions
- Expanding a phase only when the learner reaches it
- Binary data in checks
- Running two commands on one project at the same time

## Risks and things to verify first

The implementation plan starts with a short test to settle items 1 and 2.
1. Using `--safe-mode` together with `-p` while logged in with a Claude subscription hasn't been tested. Fallback: `--bare` with an API key.
2. It isn't confirmed which field of the `--output-format json` result holds the structured output.
3. Generated checks may be wrong (wrong bytes or formats), and nothing runs them until the verifier exists. For now the references and the byte-for-byte rule reduce this risk. Later, checks can be tested against a reference implementation.
4. The threshold in rule 6 depends on how Claude tags concepts. Tune it after the smoke runs.
5. Parallel expansion might still produce context text that assumes something other phases' details didn't set up. Watch for this in the smoke runs.

## Deviations from docs/specs.md

- §26: progress lives in files inside each project plus a registry, not in SQLite.
- §25: the Claude adapter is a function, not an interface. The Learning Spec is what keeps the product independent of Claude.
- §23: the eight stages become one design call plus one expand call per phase. Stage 6 becomes validity rule 6 plus the repair call.
- §14: "Prerequisites" is split into step prerequisites (`prerequisites`) and learner knowledge (`project.assumes`).
- §15: levels 2–4 are generated in advance as three fixed hints. Levels 5–6 are left to the tutor.
