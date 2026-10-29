"""Progress tracking (curriculum/progress.jsonl) and the next/done/status/list commands."""
import json
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import IntEnum
from pathlib import Path
from typing import TextIO

from . import TattvaError
from .project import Project, hash_of, load_registry, save_entry
from .render import num
from .spec import PROGRESS_FILE, SPEC_FILE, OldSpec, Spec, decode_spec, step_file, validate


@dataclass
class Event:
    at: datetime | None
    step: str
    event: str
    level: int = 0  # which hint, for "hint" events

    def to_json(self) -> str:
        d = {"at": self.at.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"), "step": self.step,
             "event": self.event}
        if self.level:
            d["level"] = self.level
        return json.dumps(d, separators=(",", ":"), ensure_ascii=False)


def now() -> datetime:
    return datetime.now(timezone.utc).replace(microsecond=0)


def read_events(path: Path) -> tuple[list[Event], list[str]]:
    """Lines that aren't valid events, such as a half-written last line after a crash, are skipped with a warning."""
    try:
        text = Path(path).read_text()
    except FileNotFoundError:
        return [], []
    events, warns = [], []
    for i, line in enumerate(text.split("\n"), 1):
        if not line.strip():
            continue
        try:
            d = json.loads(line)
            events.append(Event(datetime.fromisoformat(d["at"]), d["step"], d["event"], d.get("level", 0)))
        except (ValueError, KeyError, TypeError):
            warns.append(f"{PROGRESS_FILE} line {i} isn't valid JSON; ignored")
    return events, warns


class Status(IntEnum):
    LOCKED = 0
    AVAILABLE = 1
    IN_PROGRESS = 2
    DONE = 3


MARKS = {Status.DONE: "✓", Status.IN_PROGRESS: "→", Status.AVAILABLE: "○", Status.LOCKED: "·"}


@dataclass
class Progress:
    status: dict[str, Status] = field(default_factory=dict)
    current: str = ""  # the most recently started step still in progress, or ""
    done: int = 0
    hints: dict[str, int] = field(default_factory=dict)  # the highest hint level opened, per step
    warns: list[str] = field(default_factory=list)


def progress_of(spec: Spec, events: list[Event]) -> Progress:
    """Event types it doesn't know are ignored, so later versions can add their own."""
    started: dict[str, datetime] = {}
    completed: set[str] = set()
    pr = Progress()
    unknown = 0
    for ev in events:
        if spec.step_index(ev.step) < 0:
            unknown += 1
        elif ev.event == "started":
            if ev.step not in started or ev.at > started[ev.step]:
                started[ev.step] = ev.at
        elif ev.event == "completed":
            completed.add(ev.step)
        elif ev.event == "hint":
            pr.hints[ev.step] = max(pr.hints.get(ev.step, 0), ev.level)
    if unknown:
        pr.warns.append(f"{unknown} progress events refer to steps no longer in the curriculum; ignored")
    latest = None
    for st in spec.steps:
        if st.id in completed:
            pr.status[st.id] = Status.DONE
            pr.done += 1
        elif st.id in started:
            pr.status[st.id] = Status.IN_PROGRESS
            if not pr.current or started[st.id] >= latest:
                latest, pr.current = started[st.id], st.id
        else:
            locked = any(pre not in completed for pre in st.prerequisites)
            pr.status[st.id] = Status.LOCKED if locked else Status.AVAILABLE
    return pr


def append_event(p: Project, ev: Event) -> None:
    """Stamps ev with the current time, appends it to the log and records the new hash."""
    ev.at = now()
    path = p.abs(PROGRESS_FILE)
    with open(path, "a") as f:
        f.write(ev.to_json() + "\n")
    p.entry.files[PROGRESS_FILE] = hash_of(path.read_bytes())
    save_entry(p.entry)


def project_progress(p: Project) -> Progress:
    """Works out every step's status; warnings go to the project's notices."""
    events, warns = read_events(p.abs(PROGRESS_FILE))
    pr = progress_of(p.spec, events)
    p.notices += warns + pr.warns
    return pr


def describe_step(p: Project, i: int) -> str:
    st = p.spec.steps[i]
    return f"{num(i)} · {st.title} → {p.abs(step_file(i, st.id))}"


def cmd_next(p: Project, out: TextIO) -> None:
    pr = project_progress(p)
    if pr.current:
        print("In progress:", describe_step(p, p.spec.step_index(pr.current)), file=out)
        return
    for i, st in enumerate(p.spec.steps):
        if pr.status[st.id] == Status.AVAILABLE:
            append_event(p, Event(None, st.id, "started"))
            print("Started:", describe_step(p, i), file=out)
            return
    print(f"All {len(p.spec.steps)} steps are done.", file=out)


def cmd_done(p: Project, arg: str, out: TextIO) -> None:
    """Completes the current step, or the one arg names: a step number if it's all digits, otherwise a step id."""
    pr = project_progress(p)
    total = len(p.spec.steps)
    if not arg and not pr.current:
        raise TattvaError("no step is in progress; pass a step number or id, such as `tattva done 3`")
    if not arg:
        i = p.spec.step_index(pr.current)
    elif arg.isascii() and arg.isdigit():
        i = int(arg) - 1 if 1 <= int(arg) <= total else -1
    else:
        i = p.spec.step_index(arg)
    if i < 0:
        raise TattvaError(f'no step "{arg}" in this curriculum (steps are 1-{total})')
    st = p.spec.steps[i]
    if pr.status[st.id] == Status.DONE:
        print(f"Step {num(i)} · {st.title} is already done.", file=out)
        return
    missing = [num(p.spec.step_index(pre)) for pre in st.prerequisites if pr.status[pre] != Status.DONE]
    if missing:
        print(f"Note: prerequisite steps {', '.join(missing)} aren't done yet.", file=out)
    append_event(p, Event(None, st.id, "completed"))
    print(f"Done: {num(i)} · {st.title} ({pr.done + 1}/{total} steps, {(pr.done + 1) * 100 // total}%). "
          "Run `tattva next` for the next step.", file=out)


def cmd_status(p: Project, out: TextIO) -> None:
    pr = project_progress(p)
    s = p.spec
    print(f"{s.project.name}: {s.project.target} in {s.project.language} · "
          f"{pr.done}/{len(s.steps)} steps done ({pr.done * 100 // len(s.steps)}%)", file=out)
    for pi, ph in enumerate(s.phases, 1):
        print(f"\nPhase {pi} · {ph.title}", file=out)
        for i, st in enumerate(s.steps):
            if st.phase == ph.id:
                h = hint_note(pr.hints.get(st.id, 0))
                print(f"  {MARKS[pr.status[st.id]]} {num(i)} {st.title}{f' ({h})' if h else ''}", file=out)
    warns = validate(s)[1]
    if warns:
        print(file=out)
        for w in warns:
            print("warning:", w, file=out)
    for rel in p.edited:
        print("edited outside tattva (not overwritten):", rel, file=out)


def hint_note(n: int) -> str:
    return "" if n == 0 else "1 hint" if n == 1 else f"{n} hints"


def slug(name: str) -> str:
    out, dash = "", False
    for c in name.lower():
        if not c.isalnum():
            dash = True
            continue
        if dash and out:
            out += "-"
        out += c
        dash = False
    return out


@dataclass
class ProjectView:
    """One registered project as `tattva list` and the workspace show it. Loading it never writes anything."""
    n: int  # position in name order, from 1
    id: str
    name: str
    slug: str
    root: str
    target: str = ""
    language: str = ""
    done: int = 0
    total: int = 0
    hints: int = 0
    focus: str = ""  # "→ 08 Key expiry", "all done" or "no step in progress"
    problem: str = ""  # why the project can't be shown, or ""
    spec: Spec | None = None
    events: list[Event] = field(default_factory=list)

    def load(self) -> str:
        """Fills self from its project folder; returns why it couldn't, or ""."""
        root = Path(self.root)
        try:
            s = decode_spec((root / SPEC_FILE).read_bytes())
        except FileNotFoundError:
            return "missing: " + self.root
        except OldSpec as e:
            return str(e)
        except (OSError, ValueError) as e:
            return "can't read its spec: " + str(e)
        if validate(s)[0]:
            return f"its spec breaks the curriculum rules; run `tattva status` in {self.root} for details"
        try:
            events, _ = read_events(root / PROGRESS_FILE)
        except OSError as e:
            return "can't read its progress: " + str(e)
        pr = progress_of(s, events)
        self.name, self.slug = s.project.name, slug(s.project.name)
        self.target, self.language = s.project.target, s.project.language
        self.done, self.total = pr.done, len(s.steps)
        self.hints = sum(pr.hints.values())
        self.focus = focus_of(s, pr)
        self.spec, self.events = s, events
        return ""


def load_projects() -> tuple[list[ProjectView], str]:
    """Every registered project, sorted by name, without writing anything; plus a warning about the registry itself."""
    entries, warn = load_registry()
    views = []
    for i, e in enumerate(sorted(entries, key=lambda e: e.name), 1):
        v = ProjectView(i, e.id, e.name, slug(e.name), e.path)
        v.problem = v.load()
        views.append(v)
    return views, warn


def focus_of(spec: Spec, pr: Progress) -> str:
    i = spec.step_index(pr.current)
    if i >= 0:
        return f"→ {num(i)} {spec.steps[i].title}"
    return "all done" if pr.done == len(spec.steps) else "no step in progress"


def cmd_list(out: TextIO) -> None:
    projects, warn = load_projects()
    if warn:
        print("note:", warn, file=out)
    if not projects:
        print('No projects yet. Start one with `tattva new "<target>" --lang <lang>`.', file=out)
        return
    for v in projects:
        if v.problem:
            print(f"{v.name}  ({v.problem})", file=out)
        else:
            print(f"{v.name}  {v.done * 100 // v.total}%  {v.focus}  last activity "
                  f"{last_activity(v.root, v.events)}  {v.root}", file=out)


def last_activity(root: str, events: list[Event]) -> str:
    """The time of the last progress event, or of the spec's last change if there are none."""
    if events:
        t = max(ev.at for ev in events)
    else:
        try:
            t = datetime.fromtimestamp((Path(root) / SPEC_FILE).stat().st_mtime, timezone.utc)
        except OSError:
            return ""
    return t.astimezone().strftime("%Y-%m-%d %H:%M")
