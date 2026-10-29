"""What each model node returns, and the rules each answer is checked against."""

from typing import Callable

from pydantic import ValidationError

from .spec import (MAX_CHECKPOINTS, MAX_GOAL_WORDS, MAX_NEW_CONCEPTS, MAX_STEPS, Architecture,
                   Concept, Detail, Feature, Phase, Program, Reference, Scope, Strict, detail_errors,
                   id_errors, program_errors)


class ResearchOut(Strict):
    brief: str
    references: list[Reference]


class ProjectDraft(Strict):
    name: str
    summary: str
    assumes: list[str]


class DesignOut(Strict):
    project: ProjectDraft
    scope: Scope
    architecture: Architecture
    program: Program
    concepts: list[Concept]
    phases: list[Phase]
    features: list[Feature]


class DraftCheckpoint(Strict):
    text: str
    by_hand: bool


class Drafts(Strict):
    checkpoints: list[DraftCheckpoint]


class JudgeOut(Strict):
    minutes: int
    reason: str


class CoverageOut(Strict):
    covered: bool
    problem: str


class StepDraft(Strict):
    id: str
    title: str
    goal: str
    checkpoints: list[str]
    concepts: list[str]
    prerequisites: list[str]


class StepsOut(Strict):
    steps: list[StepDraft]


def _parse(model, out):
    try:
        return model.model_validate(out), []
    except ValidationError as e:
        return None, [f"output doesn't match the schema: {e}"]


def research_errors(out: dict) -> list[str]:
    o, errs = _parse(ResearchOut, out)
    if o is None:
        return errs
    if not o.brief.strip():
        errs.append("brief is empty")
    if not o.references:
        errs.append("references is empty")
    return errs


def design_errors(out: dict) -> list[str]:
    o, errs = _parse(DesignOut, out)
    if o is None:
        return errs
    phases, e = id_errors("phase", [p.id for p in o.phases])
    errs += e
    errs += id_errors("feature", [f.id for f in o.features])[1]
    errs += id_errors("concept", [c.id for c in o.concepts])[1]
    components, e = id_errors("component", [c.id for c in o.architecture.components])
    errs += e
    if not o.phases:
        errs.append("there are no phases")
    for f in o.features:
        if f.phase not in phases:
            errs.append(f"feature {f.id!r}: phase {f.phase!r} is not declared")
    order: list[str] = []
    for f in o.features:
        if not order or order[-1] != f.phase:
            order.append(f.phase)
    for p in o.phases:
        if p.id not in order:
            errs.append(f'phase "{p.id}" has no features')
    declared = [p.id for p in o.phases if p.id in order]
    if order != declared:
        errs.append(
            "features must be grouped by phase in declared order: "
            f"features give [{' '.join(order)}], phases are [{' '.join(declared)}]"
        )
    for c in o.architecture.components:
        for other in c.talks_to:
            if other not in components:
                errs.append(f"component {c.id!r} talks to undeclared component {other!r}")
    return errs + program_errors(o.program)


def drafts_errors(out: dict, at_least: int) -> list[str]:
    o, errs = _parse(Drafts, out)
    if o is None:
        return errs
    if len(o.checkpoints) < at_least:
        return [f"returned {len(o.checkpoints)} checkpoints, want at least {at_least}"]
    return [f"checkpoint {i} has no text" for i, c in enumerate(o.checkpoints, 1) if not c.text.strip()]


def judge_errors(out: dict) -> list[str]:
    o, errs = _parse(JudgeOut, out)
    if o is None:
        return errs
    if o.minutes < 1:
        errs.append(f"minutes is {o.minutes}, want at least 1")
    if not o.reason.strip():
        errs.append("reason is empty")
    return errs


def coverage_errors(out: dict) -> list[str]:
    o, errs = _parse(CoverageOut, out)
    if o is None:
        return errs
    if not o.covered and not o.problem.strip():
        errs.append("problem is empty although covered is false")
    return errs


def steps_errors(out: dict, settled: list[dict], outline: dict) -> list[str]:
    o, errs = _parse(StepsOut, out)
    if o is None:
        return errs
    design, errs = _parse(DesignOut, outline)
    if design is None:
        return errs
    steps = o.steps
    errs += id_errors("step", [s.id for s in steps])[1]
    if len(steps) > MAX_STEPS:
        errs.append(f"there are {len(steps)} steps, the maximum is {MAX_STEPS}")

    n = len(settled)
    listed = [c for s in steps for c in s.checkpoints]
    if listed != [f"c{i}" for i in range(1, n + 1)]:
        errs.append(f"steps must list c1 to c{n} in order, each exactly once; they list {' '.join(listed)}")
    index = {f"c{i}": r for i, r in enumerate(settled, 1)}

    declared = {c.id for c in design.concepts}
    seen: set[str] = set()
    earlier: set[str] = set()
    for s in steps:
        if not 1 <= len(s.checkpoints) <= MAX_CHECKPOINTS:
            errs.append(
                f"step {s.id!r} has {len(s.checkpoints)} checkpoints; the maximum is {MAX_CHECKPOINTS}"
                if len(s.checkpoints) > MAX_CHECKPOINTS
                else f"step {s.id!r} has no checkpoints"
            )
        recs = [index[c] for c in s.checkpoints if c in index]
        features = sorted({r["feature"] for r in recs})
        if len(features) > 1:
            errs.append(f"step {s.id!r} mixes checkpoints from features {', '.join(features)}; one feature per step")
        if len(s.checkpoints) > 1:
            errs += [f"flagged checkpoint {c} must be a step on its own"
                     for c in s.checkpoints if index.get(c, {}).get("status") == "flagged"]
        words = len(s.goal.split())
        if words > MAX_GOAL_WORDS:
            errs.append(
                f"step {s.id!r}: goal has {words} words; the maximum is {MAX_GOAL_WORDS}: if it joins "
                "several behaviours split the step, otherwise move formulas and shared conventions to scope.in"
            )
        if not s.concepts:
            errs.append(f"step {s.id!r} has no concepts")
        fresh = []
        for c in s.concepts:
            if c not in declared:
                errs.append(f"step {s.id!r}: concept {c!r} is not declared")
            if c not in seen:
                fresh.append(c)
                seen.add(c)
        if len(fresh) > MAX_NEW_CONCEPTS:
            errs.append(
                f"step {s.id!r} introduces {len(fresh)} new concepts ({', '.join(fresh)}); "
                f"the maximum is {MAX_NEW_CONCEPTS}, so split the step"
            )
        for pre in s.prerequisites:
            if pre not in earlier:
                errs.append(f"step {s.id!r}: prerequisite {pre!r} is not an earlier step")
        earlier.add(s.id)
    for c in design.concepts:
        if c.id not in seen:
            errs.append(f"concept {c.id!r} is never used by a step")
    return errs


def detail_rules(kind: str) -> Callable[[dict], list[str]]:
    def rules(out: dict) -> list[str]:
        d, errs = _parse(Detail, out)
        return errs if d is None else detail_errors(kind, d)[0]

    return rules
