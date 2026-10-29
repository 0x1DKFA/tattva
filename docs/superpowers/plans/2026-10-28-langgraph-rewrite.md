# Tattva Python + LangGraph Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Go tattva with a Python package whose `tattva new` runs a LangGraph graph (research → design → breakdown → judge/split/coverage loop → build steps → expand → write) unattended and resumably, while `next`, `done`, `status`, `list` and `serve` keep today's behaviour.

**Architecture:** One package, `tattva/`. Model calls shell out to the `claude` / `codex` CLIs exactly as the Go code does. The graph lives in `tattva/graph.py`; every model node is a fresh CLI call that gets only what its LangGraph state hands it. A hub node `route` sends each unsettled checkpoint to `judge`, `split` or `coverage` with `Send` until all are settled. A SQLite checkpointer in `curriculum/.run/` makes `tattva new` resumable. Live progress is streamed with LangGraph's custom stream mode into a `rich` panel.

**Tech Stack:** Python ≥ 3.12, langgraph 1.2, langgraph-checkpoint-sqlite, pydantic 2 (already required by langgraph), rich, grandalf, jinja2, markdown-it-py, pytest.

**Spec:** `docs/superpowers/specs/2026-10-28-langgraph-rewrite-design.md`. The Go code at commit `a1f5df5` (still in the repo until Task 14) is the behaviour reference wherever a task says "port".

## Global Constraints

- Python ≥ 3.12, one package, `pyproject.toml`, installed as the `tattva` command.
- Dependencies: `langgraph`, `langgraph-checkpoint-sqlite`, `pydantic`, `rich`, `grandalf`, `jinja2`, `markdown-it-py`; `pytest` for dev. Standard library for everything else (`argparse`, `subprocess`, `http.server`, `json`, `fcntl`).
- Model calls only through the `claude` and `codex` CLIs; never API keys. `--provider claude|codex`, default `claude`. `--model` applies to every node.
- 45-minute limit per call. At most 4 model calls at once. One repair call per node. A failed call is retried once (not timeouts, not Ctrl-C).
- A checkpoint fits when `minutes <= 20`. A step has 1–3 checkpoints, at most 60 minutes. A checkpoint is split at most 3 times along its lineage. A split that fails coverage is redone once (not counted toward the 3); failing again flags the parent unsplit.
- Learner profile, used verbatim in every request that pictures the learner: `An intermediate programmer in <language>, new to this system's internals, working with no AI help. Time estimates include the reading and studying they need to do, not just typing.`
- `curriculum/spec.json` has `schema_version: 2`. Regexes in checks use Python `re` syntax.
- Registry `~/.tattva/projects.json` with lock `~/.tattva/projects.lock`; logs `~/.tattva/logs/<time>-new-<pid>.log`, never deleted.
- Tests never call a real CLI or touch the real home directory.
- Commits: no Claude attribution, no `Co-Authored-By` line.

## Review Focus

1. Model-written markdown containing raw HTML (`<script>`) or `javascript:` links, shown in the workspace: must be escaped / not linked. Test in Task 13.
2. Ctrl-C or `kill` during a run: every running `claude`/`codex` child is killed and the message says re-running resumes. Tests in Tasks 6 and 12.
3. Re-running `tattva new` with a different target, `--lang` or `--provider` while an unfinished run exists: refuse, never mix runs. Test in Task 12.
4. Old Go projects (`schema_version: 1`) still in the shared registry: `list`, `status` and `serve` report "made by an older tattva" instead of crashing. Tests in Tasks 5 and 13.
5. Codex reports no cost (`None`): panel and summary say "cost not reported" instead of crashing on arithmetic. Test in Task 11.

---

### Task 1: Package skeleton and CLI dispatch

**Files:**
- Create: `pyproject.toml`, `tattva/__init__.py`, `tattva/cli.py`, `tests/conftest.py`, `tests/test_cli.py`
- Modify: `.gitignore` (add `.venv/`, `*.egg-info/`, `.pytest_cache/`)

**Interfaces:**
- Produces: `tattva.TattvaError(Exception)` (a message for the user, no traceback); `tattva.cli.run(cwd: Path, argv: list[str], out: TextIO) -> None` (raises `TattvaError`); `tattva.cli.main() -> None` (prints `tattva: <message>` to stderr and exits 1 on `TattvaError`); `tattva.cli.USAGE: str`.
- Produces (tests): fixture `home` (autouse) in `tests/conftest.py` that sets `HOME` to `tmp_path / "home"` with `monkeypatch.setenv`.

- [ ] **Step 1: Write `pyproject.toml`** — project `tattva`, version `0.2.0`, `requires-python = ">=3.12"`, the dependencies above (`langgraph>=1.2`, `langgraph-checkpoint-sqlite>=3.1`, `pydantic>=2`, `rich>=15`, `grandalf>=0.8`, `jinja2>=3.1`, `markdown-it-py>=4`), optional `dev = ["pytest>=9"]`, script `tattva = "tattva.cli:main"`, build backend `hatchling` with `[tool.hatch.build.targets.wheel] packages = ["tattva"]`.

- [ ] **Step 2: Create the venv and install**

Run: `python3 -m venv .venv && .venv/bin/pip install -q -e '.[dev]'`
Expected: exits 0.

- [ ] **Step 3: Write the failing tests**

```python
def test_help_prints_usage(tmp_path):
    out = io.StringIO()
    run(tmp_path, ["help"], out)
    assert out.getvalue() == USAGE
    assert 'tattva new "<target>" --lang <lang>' in USAGE
    assert "revise" not in USAGE and "expand" not in USAGE

def test_no_command_prints_usage_and_errors(tmp_path):
    out = io.StringIO()
    with pytest.raises(TattvaError, match="no command given"):
        run(tmp_path, [], out)
    assert out.getvalue() == USAGE

def test_unknown_command(tmp_path):
    with pytest.raises(TattvaError, match=r"unknown command \"frob\" \(run `tattva help`\)"):
        run(tmp_path, ["frob"], io.StringIO())
```

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_cli.py -v` — Expected: FAIL (import error).

- [ ] **Step 5: Implement `run` and `main`** with `argparse` subparsers. Subclass `ArgumentParser` so `error(message)` raises `TattvaError(message)` instead of exiting. `USAGE` is exactly:

```
tattva turns a system you've used into a curriculum you build yourself.

Usage:
  tattva new "<target>" --lang <lang> [--provider <p>] [--model <m>]   write a curriculum here (re-run to resume)
  tattva next                                         start the next step
  tattva done [step]                                  complete a step
  tattva status                                       show progress
  tattva list                                         list your projects
  tattva serve [--port <n>]                           read and track in your browser
```

Only `help`/`-h`/`--help` is wired now; later tasks add their own subcommands here.

- [ ] **Step 6: Run** `.venv/bin/pytest tests/test_cli.py -v` — Expected: 3 passed.

- [ ] **Step 7: Commit** — `git add pyproject.toml .gitignore tattva tests && git commit -m "feat: Python package skeleton and CLI dispatch"`

---

### Task 2: Spec v2 models, JSON schema and validation

**Files:**
- Create: `tattva/spec.py`, `tests/test_spec.py`
- Modify: `tests/conftest.py` (add `sample_spec`)

**Interfaces:**
- Produces constants: `SPEC_FILE = "curriculum/spec.json"`, `PROGRESS_FILE = "curriculum/progress.jsonl"`, `README_FILE = "curriculum/README.md"`, `RESEARCH_FILE = "curriculum/research.md"`, `STEPS_DIR = "curriculum/steps"`, `RUN_DIR = "curriculum/.run"`, `FAILED_DIR = "curriculum/.failed"`, `SCHEMA_VERSION = 2`, `MAX_NEW_CONCEPTS = 2`, `MAX_GOAL_WORDS = 30`, `MAX_CHECKPOINTS = 3`, `CHECKPOINT_MINUTES = 20`, `MAX_SPLITS = 3`, `MAX_STEPS = 99`, `KEBAB = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")`.
- Produces pydantic models (base `Strict` with `ConfigDict(extra="forbid", populate_by_name=True)`): `ProjectInfo(id, name, target, language, summary, assumes: list[str])`, `Excluded(feature, why)`, `Scope(in_: list[str] = Field(alias="in"), out: list[Excluded])`, `Component(id, responsibility, talks_to: list[str])`, `Architecture(real, components, diagram)`, `Program(kind: Literal["server","cli"], port: int)`, `Concept(id, name, summary)`, `Reference(title, url)`, `Phase(id, title, purpose)`, `Feature(id, phase, title, description)`, `Verdict(fits: bool, minutes: int, reason: str)`, `Checkpoint(text, feature, by_hand: bool, judge: Verdict, splits: int, parent: str, flagged: bool, flag_reason: str)`, `Check(name, do: Literal["exec","tcp","write","file"], args: list[str], input, path, expect, match: Literal["exact","contains","regex"], exit_code: int)`, `Detail(why, context, task, constraints: list[str], hints: list[str], checks: list[Check], by_hand, expected_outcome, common_mistakes: list[str], reflection)`, `Step(id, phase, feature, title, goal, checkpoints: list[Checkpoint], concepts: list[str], prerequisites: list[str], detail: Detail)`, `Spec(schema_version: int, project, scope, architecture, program, concepts, references, phases, features, steps)` with method `step_index(id: str) -> int` (−1 if absent). Every step always has `detail`: there is no unexpanded state any more.
- Produces functions: `class OldSpec(ValueError)`; `decode_spec(data: bytes) -> Spec` (raises `OldSpec("made by an older tattva (schema_version N); start it again with `tattva new` in a new folder")` when `schema_version != 2`, checked before model validation; other problems raise `ValueError` with pydantic's message); `load_spec(path: Path) -> Spec`; `encode_spec(spec: Spec) -> bytes` (`model_dump(by_alias=True)`, `json.dumps(indent=2, ensure_ascii=False)` + `"\n"`); `step_file(i: int, step_id: str) -> str` (`"curriculum/steps/%02d-%s.md" % (i + 1, step_id)`); `validate(spec) -> tuple[list[str], list[str]]` (errors, warnings); `detail_errors(kind: str, d: Detail) -> tuple[list[str], list[str]]`; `check_errors(kind: str, c: Check) -> list[str]`; `is_text(s) -> bool`; `safe_rel_path(p) -> bool`; `json_schema(model: type[BaseModel]) -> dict`.
- Produces (tests): fixture `sample_spec` in conftest returning a fresh valid `Spec`: server on port 6379, phases `basics`, `storage`; features `skeleton` (basics), `ping` (basics), `get-set` (storage); steps `create-run-script`, `respond-to-ping`, `get-and-set` (third has prerequisite `respond-to-ping`), each with 1–2 checkpoints (one flagged with `flag_reason="still too big after 3 splits"`), concepts `tcp-streams`, `resp-framing`, full `detail` with 3 hints, one `tcp` check each.

- [ ] **Step 1: Write the failing tests.** Port the cases from `spec_test.go` (`Validate`, `detailErrors`, `checkErrors`, `isText`, `safeRelPath`) to pytest, adapted to v2. Add:

```python
def test_sample_spec_is_valid(sample_spec):
    assert validate(sample_spec) == ([], [])

def test_round_trip(sample_spec):
    assert decode_spec(encode_spec(sample_spec)) == sample_spec
    assert b'"in": [' in encode_spec(sample_spec)

def test_old_schema_is_reported_as_older_tattva():
    with pytest.raises(OldSpec, match="made by an older tattva"):
        decode_spec(b'{"schema_version": 1, "project": {}}')

def test_unknown_field_rejected(sample_spec):
    d = json.loads(encode_spec(sample_spec)); d["steps"][0]["extra"] = 1
    with pytest.raises(ValueError):
        decode_spec(json.dumps(d).encode())

def test_step_needs_one_to_three_checkpoints(sample_spec):
    st = sample_spec.steps[1]
    st.checkpoints = st.checkpoints[:1] * 4
    assert any("has 4 checkpoints" in e for e in validate(sample_spec)[0])

def test_step_phase_must_match_its_feature(sample_spec):
    sample_spec.steps[2].phase = "basics"
    assert any("feature" in e and "phase" in e for e in validate(sample_spec)[0])

def test_regex_uses_python_syntax(sample_spec):
    c = sample_spec.steps[1].detail.checks[0]
    c.match, c.expect = "regex", "(?P<x>a"
    assert any("regex doesn't compile" in e for e in check_errors("server", c))

def test_schema_is_inline_and_strict():
    s = json_schema(Spec)
    text = json.dumps(s)
    assert "$ref" not in text and "$defs" not in text and '"title"' not in text
    assert "in" in s["properties"]["scope"]["properties"]
    assert s["additionalProperties"] is False
    assert set(s["required"]) == set(s["properties"])
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_spec.py -v` — Expected: FAIL (import error).

- [ ] **Step 3: Implement `tattva/spec.py`.** `validate` ports every rule of Go `Validate` (rules 1–11) with these v2 changes: rule 9 wants `schema_version == 2`; feature ids are kebab-case and unique; every feature's phase is declared; every step's feature is declared, its `phase` equals its feature's phase, and each of its checkpoints has `feature == step.feature`; every step has 1–3 checkpoints (`"step %r has %d checkpoints, want 1 to 3"`); detail rules apply to every step. `check_errors` compiles regexes with `re.compile`. `json_schema` takes `model.model_json_schema(by_alias=True)`, inlines every `$ref` from `$defs`, drops `title`, `default` and `description` keys, and sets `additionalProperties: false` and `required` = all property names on every object.

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_spec.py -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: spec v2 models, schema and validation"`

---

### Task 3: Markdown rendering (README and step files)

**Files:**
- Create: `tattva/render.py`, `tattva/templates/readme.md.j2`, `tattva/templates/step.md.j2`, `tests/test_render.py`

**Interfaces:**
- Consumes: `Spec`, `Step`, `Checkpoint`, `Check`, `Program`, `step_file` (Task 2).
- Produces: `render_readme(spec) -> bytes`; `render_step(spec, i: int) -> bytes`; `@dataclass StepView(num: str, step: Step, phase_num: int, phase_title: str, after: str, checks: list[str], hints: list[tuple[str, str]], unlocks: list[str])`; `step_view(spec, i) -> StepView`; `contract(program) -> str`; `describe_check(c) -> str`; `checkpoint_line(cp: Checkpoint) -> str`; `num(i) -> str`; `prereq_nums(spec, step) -> str`; `hint_label(i) -> str`; `shell_quote(s) -> str`.

- [ ] **Step 1: Write the failing tests.** Port `render_test.go` (contract text, `describe_check` for each `do`, `shell_quote`, hint-3 fencing, unlocks). Add:

```python
def test_checkpoint_line():
    cp = Checkpoint(text="PING replies +PONG", feature="ping", by_hand=False,
                    judge=Verdict(fits=True, minutes=10, reason="small"), splits=0, parent="", flagged=False, flag_reason="")
    assert checkpoint_line(cp) == "PING replies +PONG · ~10 min"
    cp.by_hand = True
    assert checkpoint_line(cp) == "PING replies +PONG · ~10 min · by hand"
    cp.by_hand, cp.flagged, cp.flag_reason = False, True, "still too big after 3 splits"
    assert checkpoint_line(cp) == "PING replies +PONG · ~10 min · ⚑ flagged: still too big after 3 splits"

def test_step_file_shows_checkpoints_with_estimates(sample_spec):
    md = render_step(sample_spec, 1).decode()
    assert md.startswith("<!-- Generated by tattva from spec.json.")
    assert "## Checkpoints\n\n- " in md and " min" in md
    assert "not expanded" not in render_readme(sample_spec).decode()
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_render.py -v` — Expected: FAIL.

- [ ] **Step 3: Implement.** Port `render.go` and `templates/*.tmpl` to Jinja2 (`Environment(loader=PackageLoader("tattva", "templates"), autoescape=False, keep_trailing_newline=True, trim_blocks=True)`), keeping the output byte-for-byte the same as the Go templates except: checkpoints render through `checkpoint_line`, and the README drops the "(not expanded yet)" marker. `payload(s)` quotes with `json.dumps(s, ensure_ascii=False)` (Go used `%q`). `contract` text is copied exactly from `render.go`.

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_render.py -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: render README and step markdown"`

---

### Task 4: Registry and project files

**Files:**
- Create: `tattva/project.py`, `tests/test_project.py`

**Interfaces:**
- Consumes: `decode_spec`, `encode_spec`, `validate`, `OldSpec`, file constants (Task 2); `render_readme`, `render_step`, `step_file` (Tasks 2–3).
- Produces: `registry_path() -> Path` (`Path.home() / ".tattva" / "projects.json"`); `@dataclass Entry(id: str, path: str, name: str, files: dict[str, str])`; `load_registry() -> tuple[list[Entry], str]` (entries, warning); `save_entry(e: Entry) -> None`; `lock_registry()` (context manager, `fcntl.flock` on `projects.lock`); `write_file_atomic(path: Path, data: bytes) -> None`; `hash_of(data: bytes) -> str` (sha256 hex); `find_root(cwd: Path) -> Path`; `class Project` with `root: Path`, `spec: Spec`, `entry: Entry`, `notices: list[str]`, `edited: list[str]`, classmethod `Project.create(root: Path, spec: Spec) -> Project` (registers it), methods `abs(rel) -> Path`, `write(rel, data: bytes)`, `save_spec()`, `generated() -> dict[str, bytes]`, `render()`, `adopt()`, `changed(rel, data) -> bool`, `untouched(rel, cur) -> bool`, `print_notices(out)`; `open_project(cwd: Path) -> Project` (raises `TattvaError`); `with_project(cwd, out, fn: Callable[[Project], None]) -> None`.

- [ ] **Step 1: Write the failing tests.** Port `registry_test.go` (atomic write, lock, entries kept across projects, same id or path replaced, unreadable registry warning, `find_root` walking up, edited spec accepted with notice, invalid spec rejected, edited step file not overwritten and listed in `edited`, untouched stale step file deleted, `adopt`). Add:

```python
def test_old_project_is_reported_not_crashed(tmp_path):
    (tmp_path / "curriculum").mkdir()
    (tmp_path / "curriculum/spec.json").write_text('{"schema_version": 1}')
    with pytest.raises(TattvaError, match="made by an older tattva"):
        open_project(tmp_path)

def test_registry_format_matches_go(tmp_path, sample_spec):
    Project.create(tmp_path, sample_spec)
    data = json.loads(registry_path().read_text())
    assert set(data["projects"][0]) == {"id", "path", "name", "files"}
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_project.py -v` — Expected: FAIL.

- [ ] **Step 3: Implement** by porting `registry.go` (minus `removeSteps`, which only `revise --force` used). `write_file_atomic` uses `tempfile.NamedTemporaryFile(dir=..., prefix=".tattva-", delete=False)`, `chmod 0o644`, `os.replace`.

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_project.py -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: project registry and generated-file tracking"`

---

### Task 5: Progress and the next/done/status/list commands

**Files:**
- Create: `tattva/progress.py`, `tests/test_progress.py`
- Modify: `tattva/cli.py` (add `next`, `done [step]`, `status`, `list`)

**Interfaces:**
- Consumes: `Project`, `open_project`, `with_project`, `load_registry` (Task 4); `num`, `step_file` (Tasks 2–3).
- Produces: `@dataclass Event(at: datetime, step: str, event: str, level: int = 0)` serialised exactly as Go does (`{"at":"2026-10-28T10:00:00Z","step":"x","event":"started"}`, `"level"` only when non-zero); `read_events(path) -> tuple[list[Event], list[str]]`; `class Status(IntEnum): LOCKED, AVAILABLE, IN_PROGRESS, DONE`; `@dataclass Progress(status: dict[str, Status], current: str, done: int, hints: dict[str, int], warns: list[str])`; `progress_of(spec, events) -> Progress`; `append_event(p: Project, ev: Event) -> None`; `now() -> datetime` (UTC, seconds; tests monkeypatch it); `cmd_next(p, out)`, `cmd_done(p, arg: str, out)`, `cmd_status(p, out)`, `cmd_list(out)`; `@dataclass ProjectView(n, id, name, slug, target, language, done, total, hints, focus, problem, root, spec, events)`; `load_projects() -> tuple[list[ProjectView], str]`; `focus_of(spec, pr) -> str`; `hint_note(n) -> str`; `slug(name) -> str`; `MARKS: dict[Status, str]` (`✓ → ○ ·`).

- [ ] **Step 1: Write the failing tests.** Port `progress_test.go` and the `list` cases of `registry_test.go`, dropping every "not expanded" case. Add:

```python
def test_list_reports_old_projects(tmp_path, capsys):
    old = tmp_path / "old"; (old / "curriculum").mkdir(parents=True)
    (old / "curriculum/spec.json").write_text('{"schema_version": 1}')
    save_entry(Entry(id="x", path=str(old), name="Old Redis", files={}))
    out = io.StringIO(); cmd_list(out)
    assert "Old Redis  (made by an older tattva" in out.getvalue()

def test_cli_done_by_number(tmp_path, sample_spec):
    Project.create(tmp_path, sample_spec).save_spec()
    out = io.StringIO()
    run(tmp_path, ["done", "1"], out)
    assert out.getvalue().startswith("Done: 01 · ")
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_progress.py -v` — Expected: FAIL.

- [ ] **Step 3: Implement** by porting `progress.go`, `cmdList`/`loadProjects`/`focusOf`/`lastActivity` from `registry.go`, and `slug` from `serve.go`. `load_projects` sets `problem` to the `OldSpec` message when `decode_spec` raises it. Wire the four commands in `cli.py` through `with_project` (except `list`), with the same usage errors as `main.go`.

- [ ] **Step 4: Run** `.venv/bin/pytest -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: progress tracking and next/done/status/list"`

---

### Task 6: Provider calls (claude and codex subprocesses)

**Files:**
- Create: `tattva/providers.py`, `tests/test_providers.py`
- Modify: `tests/conftest.py` (add `fake_cli` fixture)

**Interfaces:**
- Produces:

```python
CALL_TIMEOUT = 45 * 60  # seconds

@dataclass
class Call:
    label: str                 # "judge 1.2": names the call in the log and the panel
    system: str
    prompt: str
    schema: dict
    provider: str = "claude"
    model: str = ""
    web: bool = False
    title: str = ""            # short subject for the panel, such as the checkpoint text
    on_event: Callable[[dict], None] = lambda ev: None

@dataclass
class Result:
    output: dict
    cost: float | None         # None: the provider doesn't report cost (codex)

class CallFailed(Exception): ...     # retried once by the graph
class CallTimedOut(Exception): ...   # not retried
class Interrupted(Exception): ...    # not retried

def run_call(call: Call) -> Result
def kill_children() -> None          # kills every running CLI child; their run_call raises Interrupted
def check_provider(name: str) -> None  # TattvaError("unknown provider \"x\" (choose claude or codex)")
```

- Events passed to `on_event`, all dicts with `"label"`: `{"type": "call_start", "title", "provider", "model"}`; `{"type": "activity", "text", "show": bool}` (`show=True` for `searching: <query>` and `reading: <url>` lines and codex agent messages, `False` for log-only lines); `{"type": "call_end", "cost": float | None, "error": str}`.
- Produces (tests): fixture `fake_cli(name: str, mode: str) -> Path` that writes the Go tests' `fakeClaudeScript` / codex fake to a temp bin dir, prepends it to `PATH`, sets `FAKE_DIR`/`FAKE_MODE`, and returns the record dir.

- [ ] **Step 1: Write the failing tests.** Port every test in `claude_test.go` and `codex_test.go` (args recorded exactly, prompt on stdin, empty temp cwd, structured output and cost, `is_error` result, garbage output, no structured output, timeout with `CALL_TIMEOUT` monkeypatched to 1, missing CLI message). Map Go's error returns to `CallFailed`, timeouts to `CallTimedOut`. Add:

```python
def test_kill_children_interrupts_running_call(fake_cli):
    fake_cli("claude", "hang")
    err = []
    t = threading.Thread(target=lambda: err.append(pytest.raises(Interrupted, run_call, Call("x", "s", "p", {}))))
    t.start(); time.sleep(0.3); kill_children(); t.join(5)
    assert not t.is_alive() and err

def test_events_show_searches(fake_cli):
    fake_cli("claude", "ok")
    evs = []
    run_call(Call("design", "s", "p", {}, on_event=evs.append))
    assert {"type": "activity", "label": "design", "text": "searching: parquet spec", "show": True} in evs
    assert evs[0]["type"] == "call_start" and evs[-1] == {"type": "call_end", "label": "design", "cost": 0.25, "error": ""}
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_providers.py -v` — Expected: FAIL.

- [ ] **Step 3: Implement** by porting `claude.go` and `codex.go`: identical CLI arguments, `subprocess.Popen` in a `tempfile.TemporaryDirectory(prefix="tattva-call-")`, prompt written to stdin then closed, stdout read line by line in the calling thread, stderr collected by a reader thread, `threading.Timer(CALL_TIMEOUT, ...)` killing the child on timeout. A module-level set of live `Popen`s, guarded by a lock, backs `kill_children`; a call whose child was killed by `kill_children` raises `Interrupted`.

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_providers.py -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: claude and codex CLI calls"`

---

### Task 7: generate() with one repair call

**Files:**
- Create: `tattva/generate.py`, `tests/test_generate.py`

**Interfaces:**
- Consumes: `Call`, `Result`, `CallFailed`, `run_call` (Task 6), `FAILED_DIR` (Task 2).
- Produces: `REPAIR_PROMPT: str`; `class InvalidOutput(CallFailed)` with `output: dict`, `errors: list[str]`, `saved: Path | None`; `generate(call: Call, check: Callable[[dict], list[str]], failed_dir: Path | None) -> tuple[dict, float | None, list[str]]` returning (accepted output, total cost, rules the first draft broke); `add_cost(a: float | None, b: float | None) -> float | None` (`None` if either is `None`).
- `generate` must call `run_call` as a module attribute lookup (`providers.run_call(...)`), so graph tests can monkeypatch `tattva.providers.run_call`.

- [ ] **Step 1: Write the failing tests.** Port `generate_test.go`'s repair tests: a passing draft makes one call; a failing draft makes a repair call whose prompt contains the broken rules, the original request and the answer, with label `"<label> repair"`; a repair that still fails raises `InvalidOutput` and writes `failed_dir/<label with spaces as dashes>-<UTC YYYYmmdd-HHMMSS>.json` holding `{"errors": [...], "output": {...}}`; costs add up; `None` cost stays `None`.

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_generate.py -v` — Expected: FAIL.

- [ ] **Step 3: Implement.** `REPAIR_PROMPT` is exactly:

```
You repair JSON answers that failed validation. You receive the original request, the answer that was given, and the rules the answer breaks. Return the corrected answer, matching the same schema, and change only what is needed to satisfy the rules. Never drop a behaviour, a checkpoint or a step to make the answer fit; split or move things instead.
```

The repair request is `"The answer below breaks these rules:\n- " + "\n- ".join(errs) + "\n\nOriginal request:\n" + call.prompt + "\n\nAnswer:\n" + json.dumps(output, indent=2)`.

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_generate.py -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: generate with one repair call"`

---

### Task 8: Node outputs and their rules

**Files:**
- Create: `tattva/outputs.py`, `tests/test_outputs.py`

**Interfaces:**
- Consumes: models, constants, `KEBAB` from Task 2.
- Produces models (`Strict` base from Task 2): `ResearchOut(brief: str, references: list[Reference])`; `ProjectDraft(name, summary, assumes: list[str])`; `DesignOut(project: ProjectDraft, scope: Scope, architecture: Architecture, program: Program, concepts: list[Concept], phases: list[Phase], features: list[Feature])`; `DraftCheckpoint(text: str, by_hand: bool)`; `Drafts(checkpoints: list[DraftCheckpoint])` (breakdown and split both return this); `JudgeOut(minutes: int, reason: str)`; `CoverageOut(covered: bool, problem: str)`; `StepDraft(id, title, goal, checkpoints: list[str], concepts: list[str], prerequisites: list[str])`; `StepsOut(steps: list[StepDraft])`.
- Produces rule functions, each returning a list of error strings: `research_errors(out: dict)`; `design_errors(out: dict)`; `drafts_errors(out: dict, at_least: int)`; `judge_errors(out: dict)`; `coverage_errors(out: dict)`; `steps_errors(out: dict, settled: list[dict], outline: dict)` where `settled` is the ordered list of settled checkpoint records (each with `"feature"`, `"flagged"`) and the agent refers to them as `c1…cN`; `detail_rules(kind: str)` returning `Callable[[dict], list[str]]` built on `detail_errors`. Each function first parses `out` with its model and returns `["output doesn't match the schema: <pydantic message>"]` if that fails.

- [ ] **Step 1: Write the failing tests.**

```python
def test_design_needs_features_in_every_phase(design_out):
    design_out["features"] = [f for f in design_out["features"] if f["phase"] != "storage"]
    assert any("phase \"storage\" has no features" in e for e in design_errors(design_out))

def test_design_features_grouped_by_phase(design_out):
    design_out["features"].reverse()
    assert any("grouped by phase" in e for e in design_errors(design_out))

def test_steps_must_read_c1_to_cn_in_order(settled, outline):
    out = {"steps": [step("a", ["c2"]), step("b", ["c1", "c3"])]}
    assert any("must list c1 to c3 in order" in e for e in steps_errors(out, settled, outline))

def test_steps_at_most_three_checkpoints(settled4, outline):
    out = {"steps": [step("a", ["c1", "c2", "c3", "c4"])]}
    assert any("has 4 checkpoints; the maximum is 3" in e for e in steps_errors(out, settled4, outline))

def test_steps_one_feature_each(settled, outline):       # c1 is feature skeleton, c2 feature ping
    out = {"steps": [step("a", ["c1", "c2"]), step("b", ["c3"])]}
    assert any("mixes checkpoints from features" in e for e in steps_errors(out, settled, outline))

def test_flagged_checkpoint_is_its_own_step(settled_with_flag, outline):  # c2 flagged
    out = {"steps": [step("a", ["c1"]), step("b", ["c2", "c3"])]}
    assert any("flagged checkpoint c2 must be a step on its own" in e for e in steps_errors(out, settled_with_flag, outline))

def test_steps_goal_and_concepts(settled, outline):
    long_goal = " ".join(["word"] * 31)
    out = {"steps": [step("a", ["c1"], goal=long_goal, concepts=["x", "y", "z"]), step("b", ["c2"]), step("c", ["c3"])]}
    errs = steps_errors(out, settled, outline)
    assert any("goal has 31 words" in e for e in errs)
    assert any("introduces 3 new concepts" in e for e in errs)

def test_split_needs_two(drafts1):
    assert drafts_errors(drafts1, at_least=2) == ["returned 1 checkpoints, want at least 2"]

def test_coverage_needs_a_problem_when_not_covered():
    assert coverage_errors({"covered": False, "problem": ""}) == ["problem is empty although covered is false"]
```

(Write small fixtures `design_out`, `outline`, `settled`, `settled4`, `settled_with_flag`, `drafts1` and helper `step(id, ids, goal="does x", concepts=None)` at the top of the test file.)

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_outputs.py -v` — Expected: FAIL.

- [ ] **Step 3: Implement.** `design_errors`: kebab/unique ids for phases, features, concepts and components; at least one phase; each feature's phase declared; every phase has a feature; features grouped by phase in declared order; `talks_to` ids declared; Go rule 8 for `program`. `steps_errors`: step ids kebab/unique; at most `MAX_STEPS` steps; reading every step's `checkpoints` in order gives exactly `c1…cN` (one rule covers missing, duplicate, unknown and out-of-order ids: `"steps must list c1 to cN in order, each exactly once; they list …"`); 1–`MAX_CHECKPOINTS` per step; one feature per step; a flagged checkpoint alone; goal ≤ `MAX_GOAL_WORDS` words; concepts declared in the outline, at least one per step, ≤ `MAX_NEW_CONCEPTS` new per step, every outline concept used; prerequisites name earlier steps. `judge_errors`: `minutes >= 1`, reason non-empty. `research_errors`: brief and references non-empty.

- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_outputs.py -v` — Expected: all pass.

- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: node output models and rules"`

---

### Task 9: Prompts for the eight model nodes

**Files:**
- Create: `tattva/prompts/research.md`, `design.md`, `breakdown.md`, `judge.md`, `split.md`, `coverage.md`, `steps.md`, `expand.md`; `tests/test_prompts.py`

**Interfaces:**
- Produces: prompt files read with `importlib.resources.files("tattva") / "prompts" / f"{name}.md"`. They are system prompts; the learner profile, language, brief and outline arrive in the request (Task 10), so no prompt hard-codes a learner.

Each prompt must contain the points below. Reuse today's `prompts/design.md` and `prompts/expand.md` wording wherever a point says "keep".

- **research.md** — You research, you don't design. Use WebSearch/WebFetch, primary sources first (official docs, specs, RFCs, source). Return `brief` (markdown) covering: architecture; wire protocol or file formats with exact bytes; the exact commands and outputs users see, copied byte for byte; default ports; error replies and edge cases. Return every source relied on in `references`.
- **design.md** — Keep from today: "Choose the miniature" (minus the 90-minute sentence; say steps are sized later by other agents, so more features means more steps), "The program contract" (all of it, including check limits and probes), "Concepts", "Output". Replace "The learner" with: describe the learner from the request's `Learner:` line in `project.assumes`. Input is the research brief; there is no web access. New "Features" section: each feature is one behaviour the miniature adds, listed in build order, each in one phase, phases in declared order with their features together; the first feature is the skeleton (`run.sh` and a minimal program: a server that accepts a TCP connection on the port, or a CLI that exits cleanly). Don't write steps or checkpoints. Drop "Steps" and "Revisions".
- **breakdown.md** — Turn one feature into ordered checkpoints. A checkpoint is one input and the observable result it shows through `run.sh`: an exact command or request and its exact output, worked out by hand from the brief, within the check limits (keep today's check-limits paragraph). `by_hand: true` only for windows, timing or concurrent clients. Aim for checkpoints of about 20 minutes for the learner; prefer more, smaller ones. Everything in earlier features is already built; don't repeat it. Follow `scope.in` exactly and use the probes the outline names.
- **judge.md** — You estimate size, never build or fix. Picture the learner in the request. Every checkpoint listed under "Already built" is done. Count reading and studying time for every idea the learner hasn't met, plus building and debugging. Return `minutes` (a whole number, realistic for this learner, not optimistic) and `reason` (one or two sentences naming what drives the time: new ideas, formats, algorithms, edge cases). Two new ideas in one checkpoint rarely fit 20 minutes. Judge size only, never wording or quality.
- **split.md** — Split one checkpoint into two or more smaller checkpoints that together cover exactly its behaviour: nothing dropped, nothing added. Same checkpoint rules as breakdown. Cut where the judge's reason says the time goes. Order them so each builds on the ones before. If a coverage problem is given, fix exactly that.
- **coverage.md** — Compare a parent checkpoint with its proposed children. `covered: true` only if every input, output, behaviour and edge case of the parent appears in at least one child and the children ask for nothing the parent doesn't. Otherwise `covered: false` and `problem` names exactly what is missing or added. Ignore wording and size.
- **steps.md** — Group the settled checkpoints `c1…cN` into steps without reordering: reading the steps in order must give `c1…cN`. 1–3 checkpoints per step, all from one feature, at most 60 minutes by their estimates; a flagged checkpoint is a step on its own; a step is one behaviour. Each step: kebab-case `id`, `title`, `goal` (one sentence, at most 30 words, naming the command or request and what it now outputs), `concepts` (from the outline, at least one, at most 2 new per step, every outline concept used somewhere), `prerequisites` (only earlier steps it directly builds on). The first step is the skeleton.
- **expand.md** — Keep today's `prompts/expand.md`, changed so that: it fills one step, not a phase ("Other steps are being written at the same time as yours"); the research brief is read first and references are re-fetched only for exact bytes; every checkpoint of the step gets at least one check using its exact input and output, unless it is by hand; regexes use Python `re` syntax (not Go RE2); the learner is the one in the request.

- [ ] **Step 1: Write the failing test**

```python
NAMES = ["research", "design", "breakdown", "judge", "split", "coverage", "steps", "expand"]

def test_prompts_exist_and_carry_their_key_rules():
    p = {n: (files("tattva") / "prompts" / f"{n}.md").read_text() for n in NAMES}
    assert all(len(t) > 300 for t in p.values())
    assert "20 minutes" in p["judge"] and "Already built" in p["judge"]
    assert "nothing dropped" in p["split"]
    assert "c1" in p["steps"] and "60 minutes" in p["steps"]
    assert "Python" in p["expand"] and "RE2" not in p["expand"]
    assert "WebSearch" in p["research"]
    assert "competent programmer" not in "".join(p.values())
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_prompts.py -v` — Expected: FAIL.
- [ ] **Step 3: Write the eight prompt files.**
- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_prompts.py -v` — Expected: PASS.
- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: prompts for the generation graph"`

---

### Task 10: The generation graph

**Files:**
- Create: `tattva/graph.py`, `tests/test_graph.py`

**Interfaces:**
- Consumes: `Call`, `CallFailed` (Task 6); `generate`, `add_cost` (Task 7); output models and rules (Task 8); prompts (Task 9); `Spec` models, `validate`, constants (Task 2); `Project`, `write_file_atomic` (Task 4).
- Produces:

```python
LEARNER = ("An intermediate programmer in {lang}, new to this system's internals, working with no AI help. "
           "Time estimates include the reading and studying they need to do, not just typing.")
MAX_CONCURRENCY = 4
STAGES = ["research", "design", "breakdown", "judge", "steps", "expand", "write"]  # judge covers judge/split/coverage

def merge(a: dict, b: dict) -> dict          # reducer: {**a, **b}

class State(TypedDict, total=False):
    target: str; lang: str; provider: str; model: str; project_id: str
    brief: str; references: list[dict]
    outline: dict                            # DesignOut as a dict
    cps: Annotated[dict[str, dict], merge]   # checkpoint id -> record
    steps: list[dict]                        # StepDraft dicts, checkpoint ids mapped back to real ids
    details: Annotated[dict[str, dict], merge]   # step id -> Detail dict
    cost: Annotated[float | None, add_cost]
    repairs: Annotated[list[str], operator.add]

def cp_key(cp_id: str) -> tuple[int, ...]    # "1.2.1" -> (1, 2, 1)
def active(cps: dict) -> list[dict]          # records whose status != "replaced", sorted by cp_key
def dispatch(state: State) -> list[Send] | str
def assemble_spec(state: State) -> Spec
def build_graph(checkpointer=None) -> CompiledStateGraph
```

- **Checkpoint record** (`cps` value): `{"id", "feature", "text", "by_hand", "splits", "parent", "status", "minutes", "reason", "proposed", "problem", "redo", "flag_reason"}`. Ids: breakdown of feature number *f* (1-based, outline order) gives `"f.1"`, `"f.2"`, …; children of `"x"` are `"x.1"`, `"x.2"`, …, so `cp_key` order is build order and children sit where their parent was. Children get `splits = parent.splits + 1` and `parent = parent.text`.
- **Statuses and who moves them:**

| status | set by | `dispatch` sends to |
|---|---|---|
| `new` | breakdown, coverage (children) | `judge` |
| `too_big` | judge (`minutes > 20` and `splits < 3`), coverage (first failure: `redo=True`, `problem` set) | `split` |
| `reviewing` | split (`proposed` = child drafts) | `coverage` |
| `fits` | judge (`minutes <= 20`) | — |
| `flagged` | judge (`minutes > 20` and `splits == 3`, `flag_reason="still too big after 3 splits"`), coverage (second failure, `flag_reason="couldn't be split without losing behaviour: " + problem`) | — |
| `replaced` | coverage (covered) | — |

  `dispatch` returns `"build_steps"` when no record is `new`, `too_big` or `reviewing`.
- **Wiring:** `START → research → design`; `design` → conditional edge `fan_breakdown` (one `Send("breakdown", …)` per feature) → `breakdown → route`; `route` → conditional `dispatch` with path map `["judge", "split", "coverage", "build_steps"]`; `judge`, `split`, `coverage → route`; `build_steps` → conditional `fan_expand` (one `Send("expand", …)` per step) → `expand → write → END`. `route` is a no-op node (`lambda s: {}`); it exists so parallel tasks join before the next round. Every model node gets `retry_policy=RetryPolicy(max_attempts=2, retry_on=CallFailed)`.
- **Send payloads:** every payload carries `lang`, `provider`, `model`, `brief`, `outline`; plus `feature` and `fi` (breakdown), `cp` and `earlier: list[str]` (texts of all `active` records before it; judge), `cp` (split, coverage), `step` and `steps` (expand).
- **Each model node:** builds a `Call` (label `"<node> <id>"`, `title` the checkpoint text or step title, `on_event=get_stream_writer()`, `schema=json_schema(<output model>)`, `web=True` only for research and expand) and calls `generate(call, <rule fn>, failed_dir=Path(config["configurable"]["root"]) / FAILED_DIR)`. Its request contains, as labelled plain-text sections: `Language:`, `Learner:` (`LEARNER.format(lang=...)`), `Research brief:`, `Outline:` (indented JSON), plus — breakdown: the feature and the titles of earlier features; judge: the feature, `Already built:` (bullet list of `earlier`) and `Checkpoint to judge:`; split: the feature, the checkpoint, `Judge's reason:` and, on a redo, `Coverage problem to fix:`; coverage: the feature, `Parent:` and `Children:` (no learner); build_steps: every settled record as `c<n> [<feature>] ~<minutes> min[ FLAGGED] <text>`; expand: all steps' id, title, goal and checkpoint texts, then `Step to write: <id>`. Research's request is just `Target:` and `Language:`. After `generate`, each node emits `{"type": "result", "stage": <stage>, "text": <one line>}` through the stream writer (e.g. `"SET with EX…" too big (~45 min): two new ideas`, `"SET with EX…" → 2 checkpoints (split 1 of 3)`, `covered` / `not covered: <problem>`), and returns its state update including `cost` and, when the first draft was repaired, `repairs` (`["<label>: <rule>", …]`).
- **Produces (tests):** fixture `fake` in `tests/conftest.py` that monkeypatches `tattva.providers.run_call` with a scripted provider and is reused by Task 12. Defaults: research → brief starting `"# Redis"` and one reference; design → server on port 6379, phases `basics`/`storage`, features `f1` (basics) and `f2` (storage), one concept per phase; breakdown of feature *n* → `["feature fn checkpoint 1", "feature fn checkpoint 2"]`; judge → 45 minutes when the checkpoint text starts with `BIG`, else 10; split → two children, the first keeping the parent's text plus `" a"` (so a `BIG` prefix survives), the second `"small " + text`; coverage → covered; build_steps → one step per settled checkpoint, ids `step-1…`, each with its phase's concept; expand → a valid `Detail` with one `tcp` check. Knobs: `fake.breakdown: dict[str, list[str]]` (feature number → texts), `fake.judge_big_once: bool` (`BIG` is too big only when `splits == 0`), `fake.covered: list[bool]` (consumed per coverage call, then True), `fake.fail: dict[str, int]` (raise `CallFailed` for the first *n* calls with that exact label), `fake.raise_on: dict[str, type[BaseException]]`. Inspection: `fake.labels: list[str]` (every call in order), `fake.count(prefix) -> int`, `fake.prompt(label, attempt=1) -> str`, `fake.cp_text(cp_id) -> str`, `fake.step_id(n) -> str`. Helper `run_graph(tmp_path) -> dict` invokes the graph with target `Redis`, lang `go`, provider `claude` and returns the final state.
- **build_steps** maps `c<n>` back to real ids before storing `steps`. Nodes that need the project root (every model node, for `failed_dir`, and `write`) take a second parameter `config: RunnableConfig`. **write** calls `assemble_spec` (fits = `minutes <= CHECKPOINT_MINUTES`; phase from each step's feature; references from research; `schema_version = 2`), raises `RuntimeError("bug: assembled spec breaks the rules: …")` if `validate` returns errors, then `Project.create(root, spec)`, `save_spec()`, `render()`, and `write(RESEARCH_FILE, brief)`.

- [ ] **Step 1: Write the `fake` fixture and the failing tests.** `run_graph` calls `build_graph().invoke(init, {"configurable": {"root": str(tmp_path)}, "max_concurrency": 4})`.

```python
def test_happy_path_writes_a_valid_curriculum(tmp_path, fake):
    run_graph(tmp_path)
    spec = load_spec(tmp_path / "curriculum/spec.json")
    assert validate(spec)[0] == [] and spec.schema_version == 2
    assert (tmp_path / "curriculum/research.md").read_text().startswith("# ")
    assert len(list((tmp_path / "curriculum/steps").glob("*.md"))) == len(spec.steps)

def test_too_big_three_times_is_flagged(tmp_path, fake):
    fake.breakdown = {"1": ["BIG parse", "small"]}
    state = run_graph(tmp_path)
    flagged = [c for c in state["cps"].values() if c["status"] == "flagged"]
    assert [c["id"] for c in flagged] == ["1.1.1.1.1"] and flagged[0]["splits"] == 3
    assert flagged[0]["flag_reason"] == "still too big after 3 splits"
    assert fake.count("split ") == 3

def test_children_replace_parent_in_order(tmp_path, fake):
    fake.breakdown = {"1": ["first", "BIG middle", "last"]}
    fake.judge_big_once = True                 # children of "BIG middle" fit
    texts = [c["text"] for c in active(run_graph(tmp_path)["cps"]) if c["feature"] == "f1"]
    assert texts[0] == "first" and texts[-1] == "last" and len(texts) == 4

def test_coverage_failing_twice_flags_parent_unsplit(tmp_path, fake):
    fake.breakdown = {"1": ["BIG parse"]}; fake.covered = [False, False]
    cps = run_graph(tmp_path)["cps"]
    assert cps["1.1"]["status"] == "flagged" and "1.1.1" not in cps
    assert cps["1.1"]["flag_reason"].startswith("couldn't be split without losing behaviour")
    assert fake.count("split ") == 2

def test_coverage_redo_does_not_count_as_a_split(tmp_path, fake):
    fake.breakdown = {"1": ["BIG parse"]}; fake.covered = [False, True]; fake.judge_big_once = True
    cps = run_graph(tmp_path)["cps"]
    assert cps["1.1.1"]["splits"] == 1 and "Coverage problem to fix:" in fake.prompt("split 1.1", attempt=2)

def test_judge_sees_earlier_checkpoints(tmp_path, fake):
    run_graph(tmp_path)
    assert "Already built:" in fake.prompt("judge 1.2") and fake.cp_text("1.1") in fake.prompt("judge 1.2")
    assert LEARNER.format(lang="go") in fake.prompt("judge 1.2")

def test_failed_call_is_retried_once(tmp_path, fake):
    fake.fail = {"judge 1.1": 1}
    run_graph(tmp_path)
    assert fake.count("judge 1.1") == 2

def test_failing_twice_stops_the_run(tmp_path, fake):
    fake.fail = {"judge 1.1": 2}
    with pytest.raises(CallFailed):
        run_graph(tmp_path)
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_graph.py -v` — Expected: FAIL.
- [ ] **Step 3: Implement `tattva/graph.py`** as specified above. Keep one function per node, named after the node, in graph order, so the file reads top to bottom like the diagram.
- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_graph.py -v` — Expected: all pass.
- [ ] **Step 5: Check the drawing** — Run: `.venv/bin/python -c "from tattva.graph import build_graph; print(build_graph().get_graph().draw_ascii())"` — Expected: boxes for research, design, breakdown, route, judge, split, coverage, build_steps, expand, write.
- [ ] **Step 6: Commit** — `git add tattva tests && git commit -m "feat: LangGraph generation graph with judge, split and coverage loop"`

---

### Task 11: Run log and live progress display

**Files:**
- Create: `tattva/display.py`, `tests/test_display.py`

**Interfaces:**
- Consumes: event dicts (Task 6 and Task 10's `result` events); `STAGES` (Task 10); `registry_path` (Task 4).
- Produces:

```python
def clip(s: str, n: int) -> str     # one line, at most n chars, "…" when cut (port of Go clip)
class RunLog:
    def __init__(self, command: str, out: TextIO)   # ~/.tattva/logs/<%Y-%m-%dT%H-%M-%S>-<command>-<pid>.log; prints "  log: <path>"
    path: Path | None
    def write(self, msg: str) -> None               # "%Y-%m-%d %H:%M:%S.mmm <msg>"
    def close(self, err: BaseException | None) -> None   # "failed: <err>" or "done"
class Display:
    def __init__(self, out: TextIO, log: RunLog, target: str, provider: str, live: bool | None = None, clock=time.monotonic)
    def __enter__(self) -> "Display"; def __exit__(self, *exc) -> None
    def handle(self, mode: str, chunk) -> None      # pairs from graph.stream(stream_mode=["custom", "values"])
    def panel_lines(self, now: float) -> list[str]  # the pinned panel as plain text
    def tick(self, now: float) -> None              # plain mode: heartbeat when 60 s passed without a printed line
    def summary(self, state: dict) -> str
```

- **Live mode** (`live=None` means `out.isatty()`): a `rich.live.Live` whose renderable calls `panel_lines(clock())` on each refresh (`refresh_per_second=2`); log lines print above it with `live.console.print`. **Plain mode:** log lines are printed directly and a daemon thread calls `tick(clock())` every second.
- **Printed lines:** `"<mm:ss> <stage:<9> <text>"` for `result` events and for `activity` events with `show=True`. Every event goes to the run log.
- **Panel** (exact shape, from the spec): a rule line; `" tattva new \"<target>\" · <elapsed> elapsed · $<cost> so far"` (or `· cost not reported` for codex); the stage row (`✓` done, `▶` current, `·` later; the current stage is the stage of the latest `call_start`, with judge/split/coverage mapping to `judge`); `" checkpoints  <n> fit · <n> splitting · <n> flagged · <n> waiting"` (from the latest `values` chunk's `cps`: `fits`; `too_big`+`reviewing`; `flagged`; `new`); then one line per running call, `<label first word> "<clip(title, 30)>"  <age> · last output <quiet> ago`. Elapsed is `mm:ss` (`h:mm:ss` from one hour); ages are `41s` / `1m 03s`.
- **Heartbeat** (plain mode): `"<mm:ss> … still working: judge ×4, last output 12s ago"`.
- **Summary:** `"Finished in <elapsed>: <n> steps, <n> flagged checkpoints, <n> repairs, $<cost>"` (`cost not reported` for codex), then each flagged checkpoint's text and reason.

- [ ] **Step 1: Write the failing tests** (`FakeClock(t)` is a callable returning its `t` attribute)

```python
def test_panel_shows_running_calls_and_quiet_time(tmp_path):
    clock = FakeClock(0)
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), 'redis', "claude", live=False, clock=clock)
    d.handle("custom", {"type": "call_start", "label": "judge 1.2", "title": "AOF append on write", "provider": "claude", "model": ""})
    clock.t = 30; d.handle("custom", {"type": "activity", "label": "judge 1.2", "text": "thinking", "show": False})
    lines = d.panel_lines(41)
    assert any('judge "AOF append on write"' in l and "41s · last output 11s ago" in l for l in lines)
    assert any("▶ judge" in l for l in lines)

def test_checkpoint_counts_from_values():
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "claude", live=False)
    d.handle("values", {"cps": {"1": {"status": "fits"}, "2": {"status": "too_big"}, "3": {"status": "flagged"}, "4": {"status": "new"}, "5": {"status": "replaced"}}})
    assert any(l.strip() == "checkpoints  1 fit · 1 splitting · 1 flagged · 1 waiting" for l in d.panel_lines(0))

def test_plain_mode_heartbeat():
    out = io.StringIO(); clock = FakeClock(0)
    d = Display(out, RunLog("new", io.StringIO()), "redis", "claude", live=False, clock=clock)
    d.handle("custom", {"type": "call_start", "label": "judge 1.1", "title": "x", "provider": "claude", "model": ""})
    d.tick(59); assert "still working" not in out.getvalue()
    d.tick(61); assert "… still working: judge ×1, last output 1m 01s ago" in out.getvalue()

def test_codex_cost_not_reported():
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "codex", live=False)
    d.handle("custom", {"type": "call_end", "label": "design", "cost": None, "error": ""})
    assert "cost not reported" in d.panel_lines(0)[1]
    assert "cost not reported" in d.summary({"steps": [], "cps": {}, "repairs": []})

def test_run_log_written(home):
    log = RunLog("new", io.StringIO()); log.write("hello"); log.close(None)
    text = log.path.read_text()
    assert "hello" in text and text.rstrip().endswith("done")
    assert log.path.parent == Path.home() / ".tattva" / "logs"
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_display.py -v` — Expected: FAIL.
- [ ] **Step 3: Implement `tattva/display.py`.**
- [ ] **Step 4: Run** `.venv/bin/pytest tests/test_display.py -v` — Expected: all pass.
- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: run log and live progress panel"`

---

### Task 12: `tattva new` with resume, signals and sleep prevention

**Files:**
- Create: `tattva/run.py`, `tests/test_run.py`
- Modify: `tattva/cli.py` (add `new "<target>" --lang <lang> [--provider claude|codex] [--model <m>]`)

**Interfaces:**
- Consumes: `build_graph`, `MAX_CONCURRENCY` (Task 10); `Display`, `RunLog` (Task 11); `kill_children`, `check_provider`, `CallFailed`, `CallTimedOut`, `Interrupted` (Task 6); `SPEC_FILE`, `RUN_DIR` (Task 2).
- Produces: `cmd_new(cwd: Path, target: str, lang: str, provider: str, model: str, out: TextIO) -> None`.

Behaviour, in order:
1. `cwd/SPEC_FILE` exists → `TattvaError("curriculum/spec.json already exists here")`.
2. Open `SqliteSaver(sqlite3.connect(cwd/RUN_DIR/"graph.sqlite", check_same_thread=False))`; thread id `"new"`; `snap = app.get_state(cfg)`.
3. `snap.values` present and its `target`/`lang`/`provider` differ from the arguments → `TattvaError('this folder has an unfinished run of tattva new "<t>" --lang <l> --provider <p>; run that to resume it, or delete curriculum/.run to start over')`. Present and `snap.next` empty → `TattvaError("curriculum/.run holds a finished run but curriculum/spec.json is missing; delete curriculum/.run to start over")`.
4. Print `app.get_graph().draw_ascii()`, then `Resuming the unfinished run…` (input `None`) or `Starting…` (input `{"target", "lang", "provider", "model", "project_id": str(uuid4())}`).
5. On macOS, if `shutil.which("caffeinate")`, start `caffeinate -i -w <os.getpid()>`.
6. Install a SIGTERM handler that raises `KeyboardInterrupt`. Stream `app.stream(input, {"configurable": {"thread_id": "new", "root": str(cwd)}, "max_concurrency": MAX_CONCURRENCY}, stream_mode=["custom", "values"])` into `Display.handle`.
7. `KeyboardInterrupt` → restore default SIGINT, `kill_children()`, `TattvaError("interrupted; run the same `tattva new` again to resume")`. `CallFailed`, `CallTimedOut`, `Interrupted` → `TattvaError(f"{err}\nThe run is saved; run the same `tattva new` again to resume.")`. The run log is closed with the error either way.
8. Success: print `Display.summary(final values)` and `Run \`tattva next\` to start the first step.`

- [ ] **Step 1: Write the failing tests** (reuse Task 10's `fake`; if LangGraph doesn't surface a `KeyboardInterrupt` raised inside a node, raise it from a monkeypatched `Display.handle` instead)

```python
def test_new_writes_curriculum_and_prints_graph(tmp_path, fake):
    out = io.StringIO()
    cmd_new(tmp_path, "Redis", "go", "claude", "", out)
    assert "build_steps" in out.getvalue() and "Starting…" in out.getvalue()
    assert (tmp_path / "curriculum/spec.json").exists()

def test_resume_does_not_repeat_finished_calls(tmp_path, fake):
    fake.fail = {"expand " + fake.step_id(2): 2}
    with pytest.raises(TattvaError, match="again to resume"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    first = list(fake.labels); fake.labels.clear(); fake.fail = {}
    out = io.StringIO()
    cmd_new(tmp_path, "Redis", "go", "claude", "", out)
    assert "Resuming the unfinished run…" in out.getvalue()
    assert not any(l.startswith(("research", "design", "breakdown", "judge")) for l in fake.labels)
    assert ("expand " + fake.step_id(2)) in fake.labels

def test_refuses_to_mix_runs(tmp_path, fake):
    fake.fail = {"design": 2}
    with pytest.raises(TattvaError):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    with pytest.raises(TattvaError, match='unfinished run of tattva new "Redis" --lang go --provider claude'):
        cmd_new(tmp_path, "Redis", "rust", "claude", "", io.StringIO())

def test_refuses_when_curriculum_exists(tmp_path, fake):
    cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    with pytest.raises(TattvaError, match="already exists here"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())

def test_interrupt_kills_children_and_says_resume(tmp_path, fake, monkeypatch):
    killed = []
    monkeypatch.setattr("tattva.run.kill_children", lambda: killed.append(True))
    fake.raise_on = {"design": KeyboardInterrupt}
    with pytest.raises(TattvaError, match="interrupted; run the same `tattva new` again to resume"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    assert killed

def test_cli_new_checks_provider(tmp_path):
    with pytest.raises(TattvaError, match='unknown provider "gpt"'):
        run(tmp_path, ["new", "Redis", "--lang", "go", "--provider", "gpt"], io.StringIO())
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_run.py -v` — Expected: FAIL.
- [ ] **Step 3: Implement `tattva/run.py` and wire `new` in `cli.py`** (`--lang` required; usage error `usage: tattva new "<target>" --lang <lang> [--provider <p>] [--model <m>]`).
- [ ] **Step 4: Run** `.venv/bin/pytest -v` — Expected: all pass.
- [ ] **Step 5: Commit** — `git add tattva tests && git commit -m "feat: tattva new runs the graph with resume"`

---

### Task 13: Web workspace

**Files:**
- Create: `tattva/serve.py`, `tattva/web/templates/base.html`, `home.html`, `project.html`, `tests/test_serve.py`
- Move: `git mv web/static tattva/web/static`
- Modify: `tattva/cli.py` (add `serve [--port <n>]`, default 4747)

**Interfaces:**
- Consumes: `load_projects`, `ProjectView`, `progress_of`, `append_event`, `Event`, `Status`, `MARKS`, `hint_note` (Task 5); `step_view`, `contract`, `checkpoint_line`, `num`, `step_file` (Task 3); `open_project` (Task 4).
- Produces: `THEMES = ["catppuccin", "tokyo-night", "gruvbox"]`; `markdown(s: str) -> Markup` (`MarkdownIt("commonmark", {"html": False}).enable(["table", "strikethrough"])`); `make_server(port: int) -> ThreadingHTTPServer` (binds `127.0.0.1:port`; port 0 allowed for tests, and the Host check uses the bound port); `cmd_serve(port: int, out: TextIO) -> None` (prints `tattva workspace: http://127.0.0.1:<port>/ (Ctrl-C to stop)`, opens the browser with `webbrowser.open`, which tests monkeypatch).

- [ ] **Step 1: Write the failing tests.** Fixtures: `server` (a `make_server(0)` running in a thread), `project` (a created `sample_spec` project), `old_project_entry` (a registry entry pointing at a `schema_version: 1` spec), and helpers `get(server, path) -> str` / `post(server, path, headers=None, form=None) -> int` over `http.client` with a matching `Host` header. Port `serve_test.go` and `web_test.go` (routes, 404s, start/done/hint POSTs append events and redirect 303 to the step / `#hints`, only the next hint can open, Host check 403, security headers), dropping the unexpanded-step cases. Add:

```python
def test_model_html_is_escaped_and_js_links_dropped():
    html = str(markdown('<script>alert(1)</script> [x](javascript:alert(1))'))
    assert "<script>" not in html and 'href="javascript:' not in html

def test_cross_site_post_rejected(server, project):
    status = post(server, f"/p/{project.spec.project.id}/s/1/start", headers={"Sec-Fetch-Site": "cross-site"})
    assert status == 403

def test_step_page_shows_checkpoint_estimates(server, project):
    assert " min" in get(server, f"/p/{project.spec.project.id}/s/2")

def test_old_project_shows_problem(server, old_project_entry):
    assert "made by an older tattva" in get(server, f"/p/{old_project_entry.id}")
```

- [ ] **Step 2: Run** `.venv/bin/pytest tests/test_serve.py -v` — Expected: FAIL.
- [ ] **Step 3: Implement.** Port `serve.go`, `web.go` and `web/templates/*.html` to Jinja2 (`autoescape=True`; `markdown` returns `Markup`). Same routes (`GET /`, `GET /p/<id>`, `GET /p/<id>/s/<n>`, `POST /p/<id>/s/<n>/<start|done|hint>`, `GET /static/<file>` from `tattva/web/static`, no directory listing), same headers (CSP, `X-Content-Type-Options`, `Referrer-Policy`) and Host check. Cross-origin protection for POSTs (Go's `CrossOriginProtection`): reject with 403 when `Sec-Fetch-Site` is present and not `same-origin` or `none`, or when it is absent and `Origin` is present with a host other than the request's `Host`. Checkpoints render through `checkpoint_line`. Remove the "Not expanded yet" branch.
- [ ] **Step 4: Run** `.venv/bin/pytest -v` — Expected: all pass.
- [ ] **Step 5: Commit** — `git add -A tattva tests web && git commit -m "feat: web workspace in Python"`

---

### Task 14: Remove the Go code and update the README

**Files:**
- Delete: `*.go`, `go.mod`, `go.sum`, `prompts/`, `templates/`, `web/` (the rest of it), `testdata/`
- Modify: `README.md`, `.gitignore` (drop the `/tattva` binary line)

- [ ] **Step 1: Delete** — `git rm -rq '*.go' go.mod go.sum prompts templates web testdata`
- [ ] **Step 2: Rewrite `README.md`**: what tattva does; install (`python3 -m venv .venv && .venv/bin/pip install -e .`, then put `.venv/bin/tattva` on `PATH`); the commands from `USAGE`; that `tattva new` runs unattended, shows the graph and a live panel, and resumes when re-run; requirements (`claude` or `codex` CLI logged in).
- [ ] **Step 3: Verify** — Run: `.venv/bin/pytest -q` — Expected: all pass. Run: `.venv/bin/tattva help` — Expected: the `USAGE` text. Run: `grep -rn "tattva revise\|tattva expand" tattva README.md` — Expected: no output.
- [ ] **Step 4: Commit** — `git add -A && git commit -m "chore: remove the Go implementation"`
