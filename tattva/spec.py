"""Curriculum spec (schema v2): models, JSON schema and validation."""

import json
import re
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

SPEC_FILE = "curriculum/spec.json"
PROGRESS_FILE = "curriculum/progress.jsonl"
README_FILE = "curriculum/README.md"
RESEARCH_FILE = "curriculum/research.md"
STEPS_DIR = "curriculum/steps"
RUN_DIR = "curriculum/.run"
FAILED_DIR = "curriculum/.failed"

SCHEMA_VERSION = 2
MAX_NEW_CONCEPTS = 2
MAX_GOAL_WORDS = 30
MAX_CHECKPOINTS = 3
CHECKPOINT_MINUTES = 20
MAX_SPLITS = 3
MAX_STEPS = 99
KEBAB = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")


class Strict(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)


class ProjectInfo(Strict):
    id: str
    name: str
    target: str
    language: str
    summary: str
    assumes: list[str]


class Excluded(Strict):
    feature: str
    why: str


class Scope(Strict):
    in_: list[str] = Field(alias="in")
    out: list[Excluded]


class Component(Strict):
    id: str
    responsibility: str
    talks_to: list[str]


class Architecture(Strict):
    real: str
    components: list[Component]
    diagram: str


class Program(Strict):
    kind: Literal["server", "cli"]
    port: int


class Concept(Strict):
    id: str
    name: str
    summary: str


class Reference(Strict):
    title: str
    url: str


class Phase(Strict):
    id: str
    title: str
    purpose: str


class Feature(Strict):
    id: str
    phase: str
    title: str
    description: str


class Verdict(Strict):
    fits: bool
    minutes: int
    reason: str


class Checkpoint(Strict):
    text: str
    feature: str
    by_hand: bool
    judge: Verdict
    splits: int
    parent: str
    flagged: bool
    flag_reason: str


class Check(Strict):
    name: str
    do: Literal["exec", "tcp", "write", "file"]
    args: list[str]
    input: str
    path: str
    expect: str
    match: Literal["exact", "contains", "regex"]
    exit_code: int


class Detail(Strict):
    why: str
    context: str
    task: str
    constraints: list[str]
    hints: list[str]
    checks: list[Check]
    by_hand: str
    expected_outcome: str
    common_mistakes: list[str]
    reflection: str


class Step(Strict):
    id: str
    phase: str
    feature: str
    title: str
    goal: str
    checkpoints: list[Checkpoint]
    concepts: list[str]
    prerequisites: list[str]
    detail: Detail


class Spec(Strict):
    schema_version: int
    project: ProjectInfo
    scope: Scope
    architecture: Architecture
    program: Program
    concepts: list[Concept]
    references: list[Reference]
    phases: list[Phase]
    features: list[Feature]
    steps: list[Step]

    def step_index(self, id: str) -> int:
        return next((i for i, s in enumerate(self.steps) if s.id == id), -1)


class OldSpec(ValueError):
    pass


def decode_spec(data: bytes) -> Spec:
    raw = json.loads(data)
    version = raw.get("schema_version") if isinstance(raw, dict) else None
    if version != SCHEMA_VERSION:
        raise OldSpec(
            f"made by an older tattva (schema_version {version}); "
            "start it again with `tattva new` in a new folder"
        )
    try:
        return Spec.model_validate(raw)
    except ValidationError as e:
        raise ValueError(str(e)) from e


def load_spec(path: Path) -> Spec:
    try:
        return decode_spec(Path(path).read_bytes())
    except OldSpec as e:
        raise OldSpec(f"{path}: {e}") from e
    except ValueError as e:
        raise ValueError(f"{path}: {e}") from e


def encode_spec(spec: Spec) -> bytes:
    data = spec.model_dump(by_alias=True)
    return (json.dumps(data, indent=2, ensure_ascii=False) + "\n").encode()


def step_file(i: int, step_id: str) -> str:
    return "%s/%02d-%s.md" % (STEPS_DIR, i + 1, step_id)


def id_errors(kind: str, ids: list[str]) -> tuple[set[str], list[str]]:
    errs: list[str] = []
    seen: set[str] = set()
    for i in ids:
        if not KEBAB.match(i):
            errs.append(f"{kind} id {i!r} is not kebab-case")
        if i in seen:
            errs.append(f"duplicate {kind} id {i!r}")
        seen.add(i)
    return seen, errs


def program_errors(p: Program) -> list[str]:
    if p.kind == "server" and not 1024 <= p.port <= 65535:
        return [f"server port {p.port} must be between 1024 and 65535"]
    if p.kind == "cli" and p.port != 0:
        return [f"cli programs have port 0, got {p.port}"]
    return []


def validate(spec: Spec) -> tuple[list[str], list[str]]:
    errs: list[str] = []
    warns: list[str] = []

    if spec.schema_version != SCHEMA_VERSION:
        errs.append(f"schema_version is {spec.schema_version}, want {SCHEMA_VERSION}")

    def id_set(kind: str, ids: list[str]) -> set[str]:
        found, e = id_errors(kind, ids)
        errs.extend(e)
        return found

    phase_ids = [p.id for p in spec.phases]
    concept_ids = [c.id for c in spec.concepts]
    phases = id_set("phase", phase_ids)
    id_set("step", [s.id for s in spec.steps])
    concepts = id_set("concept", concept_ids)
    components = id_set("component", [c.id for c in spec.architecture.components])
    features = id_set("feature", [f.id for f in spec.features])
    feature_phase = {f.id: f.phase for f in spec.features}
    for f in spec.features:
        if f.phase not in phases:
            errs.append(f"feature {f.id!r}: phase {f.phase!r} is not declared")

    if not spec.phases:
        errs.append("there are no phases")
    if len(spec.steps) > MAX_STEPS:
        errs.append(f"there are {len(spec.steps)} steps, the maximum is {MAX_STEPS}")

    # steps grouped by phase, phases in declared order, none empty
    order: list[str] = []
    for st in spec.steps:
        if st.phase not in phases:
            errs.append(f"step {st.id!r}: phase {st.phase!r} is not declared")
        if not order or order[-1] != st.phase:
            order.append(st.phase)
    if order != phase_ids:
        errs.append(
            "steps must be grouped by phase in declared order, with no empty phase: "
            f"steps give [{' '.join(order)}], phases are [{' '.join(phase_ids)}]"
        )

    earlier: set[str] = set()
    seen: set[str] = set()
    for st in spec.steps:
        for pre in st.prerequisites:
            if pre not in earlier:
                errs.append(f"step {st.id!r}: prerequisite {pre!r} is not an earlier step")
        earlier.add(st.id)

        if st.feature not in features:
            errs.append(f"step {st.id!r}: feature {st.feature!r} is not declared")
        elif feature_phase[st.feature] != st.phase:
            errs.append(
                f"step {st.id!r}: phase {st.phase!r} doesn't match "
                f"feature {st.feature!r}'s phase {feature_phase[st.feature]!r}"
            )
        if not 1 <= len(st.checkpoints) <= MAX_CHECKPOINTS:
            errs.append(
                f"step {st.id!r} has {len(st.checkpoints)} checkpoints, want 1 to {MAX_CHECKPOINTS}"
            )
        for cp in st.checkpoints:
            if cp.feature != st.feature:
                errs.append(
                    f"step {st.id!r}: checkpoint feature {cp.feature!r} "
                    f"is not the step's feature {st.feature!r}"
                )

        if not st.concepts:
            errs.append(f"step {st.id!r} has no concepts")
        fresh = []
        for c in st.concepts:
            if c not in concepts:
                errs.append(f"step {st.id!r}: concept {c!r} is not declared")
            if c not in seen:
                fresh.append(c)
                seen.add(c)
        if len(fresh) > MAX_NEW_CONCEPTS:
            errs.append(
                f"step {st.id!r} introduces {len(fresh)} new concepts ({', '.join(fresh)}); "
                f"the maximum is {MAX_NEW_CONCEPTS}, so split the step"
            )
    for c in concept_ids:
        if c not in seen:
            errs.append(f"concept {c!r} is never used by a step")

    for c in spec.architecture.components:
        for other in c.talks_to:
            if other not in components:
                errs.append(f"component {c.id!r} talks to undeclared component {other!r}")

    p = spec.program
    errs += program_errors(p)

    for st in spec.steps:
        e, w = detail_errors(p.kind, st.detail)
        errs += [f"step {st.id!r}: {m}" for m in e]
        warns += [f"step {st.id!r}: {m}" for m in w]
    return errs, warns


def detail_errors(kind: str, d: Detail) -> tuple[list[str], list[str]]:
    errs: list[str] = []
    warns: list[str] = []
    for name in ("why", "context", "task", "by_hand", "expected_outcome", "reflection"):
        if not getattr(d, name).strip():
            errs.append(f"{name} is empty")
    if len(d.hints) != 3:
        errs.append(f"has {len(d.hints)} hints, want exactly 3")
    for i, h in enumerate(d.hints, 1):
        if not h.strip():
            errs.append(f"hint {i} is empty")
    if not d.common_mistakes:
        errs.append("has no common mistakes")
    if not d.checks:
        warns.append("has no checks")
    for i, c in enumerate(d.checks, 1):
        errs += [f"check {i} ({c.name}): {m}" for m in check_errors(kind, c)]
    return errs, warns


def check_errors(kind: str, c: Check) -> list[str]:
    errs: list[str] = []
    if not c.name.strip():
        errs.append("name is empty")
    if c.do == "exec":
        if kind != "cli":
            errs.append("exec checks are only for cli programs")
        if c.path:
            errs.append("exec checks don't use path")
    elif c.do == "tcp":
        if kind != "server":
            errs.append("tcp checks are only for server programs")
        if not c.input:
            errs.append("tcp checks need input")
        if c.args or c.path or c.exit_code:
            errs.append("tcp checks only use input, expect and match")
    elif c.do == "write":
        if c.args or c.expect or c.exit_code:
            errs.append("write checks only use path and input")
    elif c.do == "file":
        if c.args or c.input or c.exit_code:
            errs.append("file checks only use path, expect and match")
    for name, value in (("input", c.input), ("expect", c.expect)):
        if not is_text(value):
            errs.append(
                f"{name} contains binary bytes; checks are text only, so create binary files by "
                "running the program earlier in the step, or drop the check and explain it in by_hand"
            )
    if c.do in ("write", "file") and not safe_rel_path(c.path):
        errs.append(f"path {c.path!r} must be relative with no .. segment")
    if c.match == "regex":
        try:
            re.compile(c.expect)
        except re.error as e:
            errs.append(f"regex doesn't compile: {e}")
    return errs


def is_text(s: str) -> bool:
    return not any((ord(ch) < 0x20 and ch not in "\t\n\r") or ch == "\x7f" for ch in s)


def safe_rel_path(p: str) -> bool:
    if not p or p.startswith("/") or Path(p).is_absolute():
        return False
    return ".." not in p.replace("\\", "/").split("/")


def json_schema(model: type[BaseModel]) -> dict:
    """Strict, self-contained JSON schema: refs inlined, every property required."""
    schema = model.model_json_schema(by_alias=True)
    defs = schema.pop("$defs", {})

    def walk(node):
        if isinstance(node, list):
            return [walk(n) for n in node]
        if not isinstance(node, dict):
            return node
        if "$ref" in node:
            return walk(defs[node["$ref"].rsplit("/", 1)[-1]])
        out = {}
        for k, v in node.items():
            if k in ("title", "default", "description"):
                continue
            # property names are data, not keywords
            out[k] = {n: walk(s) for n, s in v.items()} if k == "properties" else walk(v)
        if out.get("type") == "object" and "properties" in out:
            out["additionalProperties"] = False
            out["required"] = list(out["properties"])
        return out

    return walk(schema)
