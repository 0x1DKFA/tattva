# Tattva: Python + LangGraph rewrite

**Date:** 2026-10-28
**Status:** Approved in conversation; written up for planning
**Replaces:** the Go implementation at commit `a1f5df5`, which is the behaviour reference for everything this document says is "unchanged".
**Earlier designs:** [curriculum engine](2026-09-24-curriculum-engine-design.md), [workspace](2026-09-27-workspace-design.md).

## Why

Tattva turns a system the user has used (Redis, Git, a raycaster…) into a build-your-own curriculum the user implements themselves. Two pain points drive this rewrite:

1. **Steps come out too big.** Two rounds of prompt rules (probes, 1–3 checkpoints, a 30-word goal cap, ≤2 new concepts) helped, but the same model that writes the outline is the only thing that ever judges its size, and it keeps producing 2–4 hour steps.
2. **The user can't review the breakdown.** They are learning a system they don't know yet, so a manual review of the outline (today's `new` → read README → `revise` → `expand` loop) adds nothing. The review has to be done by an AI agent.

The fix is a generation pipeline where a separate judge agent estimates every checkpoint's size, and any checkpoint that is too big is split again, before steps are formed. The user wants that pipeline built with **LangGraph**, and wants the whole project in **Python**: learning LangGraph is an explicit goal, not a side effect.

## Goals

- `tattva new` produces a complete curriculum (outline, step details, markdown) in one unattended run.
- Every checkpoint has been judged by a fresh agent to fit **20 minutes** for the learner profile below, or is visibly flagged.
- Every step is **1–3 checkpoints, at most 60 minutes**.
- A run that dies (timeout, crash, laptop lid closed, Ctrl-C) resumes from where it stopped when `tattva new` is run again.
- While it runs, the user can see at a glance that it is making progress and not stuck.
- Everything else tattva does today (progress tracking, project list, web workspace) keeps working, in Python.

## Non-goals

- Opening curricula made by the Go version. The new format starts fresh.
- `tattva revise` and `tattva expand` as separate commands. Both are removed.
- Calling model APIs with API keys. Model calls keep going through the `claude` and `codex` CLIs and the user's existing logins.
- A judge that builds or runs code. It only estimates.
- A different model per node, cost caps, or budget limits.
- The tutor and the verifier (sub-projects 3 and 4). Nothing runs a step's checks against the learner's code.

## Anti-goals

- A judge that is the same conversation as the agent whose work it judges.
- A loop that can run forever or silently drop a behaviour while splitting.
- A run where the user can't tell for minutes at a time whether anything is happening.
- Hiding the graph behind clever abstractions. The nodes, edges and state should be easy to read, since the user is learning LangGraph from this code.

## The learner profile

One profile, used everywhere a prompt pictures the learner (the design's `project.assumes`, the judge, the step builder, expand):

> An **intermediate** programmer in the chosen language, **new to this system's internals**, working with **no AI help**. Time estimates include the reading and studying they need to do, not just typing.

This replaces today's "competent programmer" assumption, so the writer and the judge picture the same person.

## Usage

```
$ mkdir mini-redis && cd mini-redis
$ tattva new "Redis" --lang go [--provider claude|codex] [--model <m>]   # whole curriculum, unattended
$ tattva new "Redis" --lang go        # after a crash or Ctrl-C: resumes the unfinished run
$ tattva next | done [step] | status | list
$ tattva serve [--port <n>]
```

| Command | Behaviour |
|---|---|
| `new "<target>" --lang <lang> [--provider] [--model]` | Runs the generation graph below from start to finish. If this folder has an unfinished run, it resumes it instead (see Resume). If this folder already has a finished curriculum, it refuses, as today. `--provider` defaults to `claude`. `--model` is passed to the CLI unchanged and applies to every node. |
| `next`, `done [step]`, `status`, `list` | Unchanged behaviour (Go: `progress.go`, `registry.go`). |
| `serve [--port <n>]` | Unchanged behaviour and look (Go: `serve.go`, `web/`): themes, light/dark, mermaid diagrams, start/done/hint buttons, hint events, local-only Host check. It renders the new format. |

## The generation graph

Each box marked *model* is a **fresh** `claude -p` or `codex exec` call. It sees only what the graph's state hands it, never an earlier call's conversation.

```
1. research   (model, web)   → research brief
2. design     (model)        → outline: scope, architecture, program contract, concepts, phases, features
3. breakdown  (model, one call per feature, in parallel) → checkpoints
4. judge      (model, one call per checkpoint, in parallel) → fits? minutes, reason
      fits                      → settled
      too big, split count < 3  → 5. split (model) → 5b. coverage check (model)
                                     covered          → children go back to 4
                                     not covered      → split once more with the problem, then check again;
                                                        still not covered → parent kept unsplit, flagged
      too big, split count = 3  → settled, flagged
6. build steps (model)        → steps of 1–3 checkpoints, ≤ 60 min
7. expand     (model, web, one call per step, in parallel) → step details and checks
8. write      (Python)        → spec.json, README.md, steps/NN-<id>.md, registry entry
```

### 1. Research

Web search and fetch only. Finds out how the real system works: architecture, wire protocol or file formats, the exact commands and outputs its users see, with primary sources (official docs, specs, RFCs, source code). It decides nothing about the curriculum. Output: a markdown **research brief** plus the list of references. The brief is saved as `curriculum/research.md` and handed to every later model node.

### 2. Design

A fresh agent reads the brief and decides: scope in/out, `architecture` (real + components + mermaid diagram), the program contract (`server` + port or `cli`), concepts, phases, and **features**. A feature is one behaviour of the miniature (e.g. "PING and ECHO over RESP", "key expiry"), belongs to one phase, and is listed in build order. The design does **not** create steps. Today's `prompts/design.md` rules carry over where they apply: the miniature's scope, the program contract and the limits of checks, probes, `scope.in` pinning everything other phases depend on, concepts, standard library only.

### 3. Breakdown

One call per feature. Turns the feature into ordered checkpoints. A **checkpoint** is one input and the observable result it produces, checkable from outside through `run.sh` (a command and its output, a request and its reply, a probe's output). Checkpoints that can only be seen by hand (a window, timing, concurrent clients) are allowed but marked as such.

### 4. Judge

One fresh call per checkpoint. It gets the research brief, the outline, the checkpoint's feature, the checkpoint, the language and the learner profile, and answers one question: **can this learner do this checkpoint in 20 minutes?** It returns `minutes` (its estimate) and `reason` (one or two sentences, e.g. "parsing RESP arrays and dispatching commands are two new ideas"); `fits` is computed as `minutes <= 20`, so the two can't disagree. It never sees the agent that wrote the checkpoint, and never builds anything.

The judge also sees the ordered list of every checkpoint before this one (earlier features and earlier in this feature) and **assumes all of them are already built**. Splitting a checkpoint never changes what comes before a later one, because the children cover the same behaviour as their parent, so judge calls stay independent and run in parallel.

### 5. Split

When the judge says a checkpoint is too big, a split call gets that checkpoint, the judge's reason, the feature, the outline and the brief, and returns two or more smaller checkpoints that **together cover exactly the original's behaviour**; nothing is dropped. The children replace the parent, in its position, and go back to the judge.

- Each checkpoint carries a **split count**. Children get their parent's count + 1. A checkpoint can be split at most **3** times along its lineage.
- A checkpoint judged too big whose split count is already 3 is **kept and flagged**. The run carries on. Flags show in the terminal summary, the run log, `spec.json` and the step's markdown.

### 5b. Coverage check

Every split is reviewed by a **fresh** agent before its children reach the judge. It gets the parent checkpoint, the children, the feature and the brief, and answers: do the children together cover exactly the parent's behaviour? It returns `covered` (bool) and `problem` (what was dropped or added, empty when covered).

- Covered: the children replace the parent and go to the judge.
- Not covered: the split is redone once, with the problem passed to the splitter. This redo doesn't count toward the limit of 3. The redo is checked again.
- Still not covered: the parent stays unsplit and is **flagged** ("couldn't be split without losing behaviour"), with the judge's last verdict. The run carries on.

Each child records its parent's text, so the lineage is visible in `spec.json`.

### 6. Build steps

A fresh agent gets the outline and every settled checkpoint (with its verdict), and groups them into steps:

- 1–3 checkpoints per step, all from **one feature**, kept in order. A flagged checkpoint is a step on its own.
- Each step gets an id, title, phase (its feature's phase), a goal (one sentence, ≤ 30 words, naming its one observable result), concepts (≤ 2 new ones per step, as today), and prerequisites (earlier steps it directly builds on).
- Step 1 is always "create `run.sh` and a minimal program", as today.

Rule checks in Python, with one repair call on failure (as today): every settled checkpoint is used exactly once; 1–3 per step; steps grouped by phase in declared order; prerequisites point to earlier steps; concept rules; goal word cap.

### 7. Expand

One call per step, with the brief, the outline, all steps' goals and checkpoints, and the step itself. Returns the step detail: why, context, task (with short abstract pseudocode where exact checks depend on an algorithm), constraints, three escalating hints, checks, by_hand, expected outcome, common mistakes, reflection. Web fetch stays on so checks can be byte-exact. Today's `prompts/expand.md` carries over: behaviour not structure, rely only on goals and `scope.in` across steps, text-only checks, every check that tests the step fails for a program that skipped it, each checkpoint gets at least one check unless it's by-hand only, probes tested with `exact` on hand-computable inputs. Rule checks in Python with one repair call, as today (Go: `detailErrors`, `checkErrors`).

### 8. Write

Plain Python. Assembles `spec.json`, renders `README.md` and one markdown file per step, records file hashes in the registry, and prints the summary (time, cost, steps, flagged checkpoints, repairs). Hand-edited generated files are detected and not overwritten, as today (Go: `Project.render`).

## Model calls

Unchanged from today (Go: `claude.go`, `codex.go`), ported to Python `subprocess`:

- `claude -p --output-format stream-json --verbose --json-schema … --system-prompt … --tools <web or none> --safe-mode --no-session-persistence`, in an empty temp dir, prompt on stdin.
- `codex exec --ignore-user-config --json --ephemeral --skip-git-repo-check --sandbox read-only --ask-for-approval never --output-schema … --output-last-message …` (plus `--search` for web), system prompt prepended to the request.
- Structured JSON output only, validated against a JSON schema generated from the Python types.
- **45-minute** limit per call. A clear error if the CLI isn't on `PATH`.
- Claude reports cost; Codex doesn't (shown as "cost not reported").
- Rule-breaking output gets **one repair call** with the original request, the answer and the broken rules. Output that still breaks rules is saved to `curriculum/.failed/` and fails that node.
- **At most 4 model calls run at once** across the whole graph.

## Resume

LangGraph's SQLite checkpointer saves the graph state after every node, in the project folder (e.g. `curriculum/.run/`). The thread id is the project id.

- `tattva new` in a folder with an unfinished run resumes it. If the target, language or provider differ from the saved run, it refuses and says so (it doesn't mix two runs).
- Parallel calls that finished before a crash are not repeated on resume; only the unfinished ones run again.
- A call that fails (call error, still-invalid output) doesn't stop its siblings. The run finishes all the work that doesn't depend on the failed item (the rest of a fan-out, as Go's `cmdExpand` does today), then stops before the first node that needs it, listing what failed and saying `tattva new` will retry it. Resume reruns only the failed items.
- Ctrl-C or SIGTERM stops the run and kills the running CLI subprocesses, so nothing keeps spending. A second Ctrl-C exits at once.
- On macOS the run holds an idle-sleep assertion (`caffeinate -i -w <pid>`), so an idle Mac doesn't sleep mid-run. Resume covers lid-close and crashes.

## Progress display

Before the run starts (fresh or resumed), `tattva new` prints the graph itself in the terminal, drawn from the compiled LangGraph graph (`get_graph().draw_ascii()`), so the nodes, edges and the judge/split loop shown are always the real ones.

During the run: option B from the conversation, a scrolling log above and a live status panel pinned at the bottom, redrawn in place (using `rich`).

```
07:15 judge  "SET with EX stores a TTL" too big: two new ideas
07:15 judge  "GET of a missing key" fits
07:40 split  "SET with EX…" → 2 checkpoints (split 1 of 3)
──────────────────────────────────────────────────────
 tattva new "redis" · 09:40 elapsed · $4.12 so far
 ✓ research  ✓ design  ✓ breakdown  ▶ judge  · steps · expand · write
 checkpoints  29 fit · 6 splitting · 1 flagged · 5 waiting
 running      judge "AOF append on write"          41s · last output 2s ago
              judge "RDB header magic"             12s · last output 5s ago
              split "EXPIRE on missing key" (2/3)  1m 03s · last output 9s ago
```

- The log shows node results and, from inside each call, its searches and pages read (as today).
- The panel shows the stage, checkpoint counts, every running call with its age and **seconds since its last output**, elapsed time and cost so far.
- When output isn't a terminal, it falls back to plain log lines plus a heartbeat line every minute when nothing else printed.
- Everything also goes to `~/.tattva/logs/<time>-new-<pid>.log`, as today. Logs are never deleted.

## The new format (`curriculum/spec.json`)

Starts fresh at `schema_version: 2`. Same broad shape as today (Go: `spec.go`) plus the new pieces:

- `features`: `{id, phase, title, description}`, in build order.
- Each step's `checkpoints` become objects: `{text, feature, by_hand, judge: {fits, minutes, reason}, splits, parent, flagged, flag_reason}`. `parent` is the text of the checkpoint it was split from (empty for originals); `flag_reason` says why it was flagged (still too big after 3 splits, or couldn't be split without losing behaviour).
- `references` come from research; the brief itself lives in `curriculum/research.md`.
- `project`, `scope`, `architecture`, `program`, `concepts`, `phases`, `steps` (with `detail` and `checks`) keep today's meaning. Check kinds (`exec`, `tcp`, `write`, `file`) and match modes (`exact`, `contains`, `regex`) are unchanged; regexes use Python `re` syntax.

The step markdown shows each checkpoint with its judge estimate and marks flagged ones. `progress.jsonl` (`started`, `completed`, `hint` events) and the registry (`~/.tattva/projects.json`, with a file lock) keep today's format and behaviour.

## Build and tooling

- Python ≥ 3.12, one package, `pyproject.toml`, installed as the `tattva` command.
- Dependencies: `langgraph`, `langgraph-checkpoint-sqlite`, `rich`, `grandalf` (needed by LangGraph's ASCII drawing), `jinja2` (web templates and markdown templates), `markdown-it-py` (GFM rendering in the workspace). Standard library for everything else: `argparse`, `subprocess`, `http.server`, `json`, `fcntl`.
- The Go code, `go.mod`, `go.sum` and Go tests are deleted. `web/static/*` (CSS, JS, mermaid) is reused as-is; `web/templates/*` and `templates/*.tmpl` are ported to Jinja2.
- Tests: `pytest`. Model calls are faked in tests (as the Go tests swap `claude`), so tests never call a real CLI.

## How we'll know it worked

- Graph tests with a fake provider: a checkpoint judged too big three times in a row is split three times and then flagged; children replace their parent in order; a split that fails coverage twice leaves the parent unsplit and flagged; resume after a simulated crash doesn't repeat finished calls.
- Ported tests for the unchanged commands pass.
- The user regenerates a Redis curriculum and reviews it themselves.

## Left to the builder

- The exact LangGraph wiring (state schema and reducers, how the judge/split loop fans out with `Send`, where the concurrency limit lives), as long as the graph stays readable and matches the flow above.
- Prompt wording for the new nodes, built from today's `prompts/design.md` and `prompts/expand.md`.
- Module layout and file names.
- How the panel is laid out, within the content above.

## Calls made without the user (check these while reading)

- Expand runs one call per step (as shown in the approved diagram), not one per phase as today. This costs more calls; cost is visible in the panel.
- A step's checkpoints all come from one feature; a flagged checkpoint is a step on its own.
- `schema_version` is 2; regexes in checks use Python `re` syntax.
- 4 concurrent model calls; one repair call per node; no automatic retries beyond that (resume covers failures).
- Run state lives in `curriculum/.run/` and is kept after the run finishes.
- The judge assumes every earlier checkpoint is built, and returns minutes rather than a yes/no; `fits` is `minutes <= 20`.
- A failed call lets independent work finish before the run stops; resume retries only what failed.
- A split that fails the coverage check is redone once (not counted toward the 3); if the redo fails too, the parent stays unsplit and flagged.
