"""Render curriculum/README.md and the step files from the spec."""

import json
import re
from dataclasses import dataclass, field

from jinja2 import Environment, PackageLoader

from .spec import Check, Checkpoint, Program, Spec, Step

_env = Environment(loader=PackageLoader("tattva", "templates"), autoescape=False,
                   keep_trailing_newline=True, trim_blocks=True)


@dataclass
class StepView:
    num: str
    step: Step
    phase_num: int
    phase_title: str
    after: str
    checks: list[str] = field(default_factory=list)
    hints: list[tuple[str, str]] = field(default_factory=list)
    unlocks: list[str] = field(default_factory=list)


def num(i: int) -> str:
    return "%02d" % (i + 1)


def prereq_nums(spec: Spec, step: Step) -> str:
    return ", ".join(num(i) for p in step.prerequisites if (i := spec.step_index(p)) >= 0)


def hint_label(i: int) -> str:
    labels = ["nudge", "specific", "pseudocode"]
    return f"Hint {i + 1} ({labels[i]})" if i < len(labels) else f"Hint {i + 1}"


def checkpoint_line(cp: Checkpoint) -> str:
    line = f"{cp.text} · ~{cp.judge.minutes} min"
    if cp.by_hand:
        line += " · by hand"
    if cp.flagged:
        line += f" · ⚑ flagged: {cp.flag_reason}"
    return line


def contract(p: Program) -> str:
    if p.kind == "server":
        return (
            "Create an executable `run.sh` at the project root that builds your server if needed and starts it. "
            "Checks call `run.sh` with no arguments, by its absolute path, from a fresh scratch directory, so find your sources relative to `run.sh` itself, "
            f"but start the server in the directory `run.sh` was called from: checks wait for port {p.port}, talk to it over TCP, and read the files it writes there."
        )
    return (
        "Create an executable `run.sh` at the project root that builds your program if needed and runs it, passing its arguments through. "
        "Checks call `run.sh` by its absolute path from a scratch directory, so find your sources relative to `run.sh` itself, "
        "but run the program in the directory `run.sh` was called from: checks read its output and the files it writes there."
    )


def payload(s: str) -> str:
    """Check text with its escapes visible, cut short when long."""
    if len(s.encode()) <= 120:
        return f"`{json.dumps(s, ensure_ascii=False)}`"
    cut = s.encode()[:120].decode(errors="ignore")
    return f"`{json.dumps(cut, ensure_ascii=False)}`… ({len(s.encode())} bytes)"


def expectation(c: Check) -> str:
    if c.match == "regex":
        return f"`{c.expect}` (regex)"
    return f"{payload(c.expect)} ({c.match})"


def shell_quote(s: str) -> str:
    if s and re.fullmatch(r"[A-Za-z0-9_@%+=:,./-]+", s):
        return s
    return "'" + s.replace("'", "'\\''") + "'"


def describe_check(c: Check) -> str:
    out = f"**{c.name}**: "
    if c.do == "exec":
        cmd = " ".join(["./run.sh", *map(shell_quote, c.args)])
        out += f"run `{cmd}`"
        if c.input:
            out += f" with stdin {payload(c.input)}"
        out += f", expect exit code {c.exit_code}"
        if c.expect:
            out += f" and stdout {expectation(c)}"
    elif c.do == "tcp":
        out += f"send {payload(c.input)}"
        if c.expect:
            out += f", expect reply {expectation(c)}"
    elif c.do == "write":
        out += f"write {payload(c.input)} to `{c.path}`"
    elif c.do == "file":
        out += f"`{c.path}` exists"
        if c.expect:
            out += f" with content {expectation(c)}"
    return out


def step_view(spec: Spec, i: int) -> StepView:
    st = spec.steps[i]
    v = StepView(num(i), st, 0, "", prereq_nums(spec, st))
    for pi, ph in enumerate(spec.phases):
        if ph.id == st.phase:
            v.phase_num, v.phase_title = pi + 1, ph.title
    v.checks = [describe_check(c) for c in st.detail.checks]
    for hi, h in enumerate(st.detail.hints):
        if hi == 2 and "```" not in h:
            h = "```text\n" + h.strip() + "\n```"  # unfenced pseudocode renders as one run-on paragraph
        v.hints.append((hint_label(hi), h))
    v.unlocks = [f"{num(j)} · {o.title}" for j, o in enumerate(spec.steps) if st.id in o.prerequisites]
    return v


def render_readme(spec: Spec) -> bytes:
    phases = []
    for pi, ph in enumerate(spec.phases):
        steps = [
            {"num": num(i), "title": st.title, "goal": st.goal, "after": prereq_nums(spec, st),
             "checkpoints": [checkpoint_line(c) for c in st.checkpoints]}
            for i, st in enumerate(spec.steps) if st.phase == ph.id
        ]
        phases.append({"num": pi + 1, "title": ph.title, "purpose": ph.purpose, "steps": steps})
    return _env.get_template("readme.md.j2").render(
        spec=spec, contract=contract(spec.program), phases=phases).encode()


def render_step(spec: Spec, i: int) -> bytes:
    v = step_view(spec, i)
    return _env.get_template("step.md.j2").render(
        v=v, d=v.step.detail, cps=[checkpoint_line(c) for c in v.step.checkpoints]).encode()
