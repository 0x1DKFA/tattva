"""The generation graph:
research → design → breakdown → judge/split/coverage loop → build_steps → expand → write.

Read it top to bottom: the state, a few helpers, then one function per node in graph order, the functions
that decide where to go next (fan_breakdown, dispatch, fan_expand), and build_graph, which wires them up.
"""
import json
import operator
from importlib.resources import files
from pathlib import Path
from typing import Annotated, TypedDict

from langchain_core.runnables import RunnableConfig
from langgraph.config import get_stream_writer
from langgraph.graph import END, START, StateGraph
from langgraph.graph.state import CompiledStateGraph
from langgraph.types import RetryPolicy, Send

from .generate import add_cost, generate
from .outputs import (CoverageOut, DesignOut, Drafts, JudgeOut, ResearchOut, StepsOut, coverage_errors,
                      design_errors, detail_rules, drafts_errors, judge_errors, research_errors, steps_errors)
from .project import Project
from .providers import Call, CallFailed, clip
from .spec import (CHECKPOINT_MINUTES, FAILED_DIR, MAX_SPLITS, RESEARCH_FILE, SCHEMA_VERSION, Detail, Spec,
                   json_schema, validate)

LEARNER = ("An intermediate programmer in {lang}, new to this system's internals, working with no AI help. "
           "Time estimates include the reading and studying they need to do, not just typing.")
MAX_CONCURRENCY = 4
STAGES = ["research", "design", "breakdown", "judge", "steps", "expand", "write"]  # judge covers judge/split/coverage


def merge(a: dict, b: dict) -> dict:
    return {**a, **b}


class State(TypedDict, total=False):
    # the run's input
    target: str
    lang: str
    provider: str
    model: str
    project_id: str
    # filled in by the nodes
    brief: str
    references: list[dict]
    outline: dict                                # DesignOut as a dict
    cps: Annotated[dict[str, dict], merge]       # checkpoint id -> record; parallel nodes each write their own ids
    steps: list[dict]                            # StepDraft dicts, checkpoint ids mapped back to real ids
    details: Annotated[dict[str, dict], merge]   # step id -> Detail dict
    cost: Annotated[float | None, add_cost]      # None once any call didn't report its cost (codex)
    repairs: Annotated[list[str], operator.add]  # "<label>: <rule the first draft broke>"


# --- helpers ---

def cp_key(cp_id: str) -> tuple[int, ...]:
    """Build order: "1.2.1" -> (1, 2, 1), so children sort where their parent was."""
    return tuple(int(n) for n in cp_id.split("."))


def active(cps: dict) -> list[dict]:
    """Checkpoint records still in the curriculum (not replaced by their children), in build order."""
    return sorted((c for c in cps.values() if c["status"] != "replaced"), key=lambda c: cp_key(c["id"]))


def record(cp_id: str, feature: str, draft: dict, splits: int, parent: str) -> dict:
    """A new checkpoint record, waiting for the judge."""
    return {"id": cp_id, "feature": feature, "text": draft["text"], "by_hand": draft["by_hand"],
            "splits": splits, "parent": parent, "status": "new", "minutes": 0, "reason": "",
            "proposed": [], "problem": "", "redo": False, "flag_reason": ""}


def shared(state: State) -> dict:
    """What every Send payload carries."""
    return {k: state[k] for k in ("lang", "provider", "model", "brief", "outline")}


def request(*sections: tuple[str, str | list[str]]) -> str:
    """Labelled plain-text sections. One line of text sits after its label; a list, one item per line,
    or longer text goes below it."""
    parts = []
    for name, body in sections:
        if isinstance(body, list):
            body = "\n".join(body)
            parts.append(f"{name}:\n{body}")
        else:
            parts.append(f"{name}:\n{body}" if "\n" in body else f"{name}: {body}")
    return "\n\n".join(parts)


def context(p: dict, learner: bool = True) -> list[tuple[str, str]]:
    """The sections most requests start with."""
    sections = [("Language", p["lang"])]
    if learner:
        sections.append(("Learner", LEARNER.format(lang=p["lang"])))
    return sections + [("Research brief", p["brief"]), ("Outline", json.dumps(p["outline"], indent=2))]


def feature_json(p: dict, cp: dict) -> str:
    feature = next(f for f in p["outline"]["features"] if f["id"] == cp["feature"])
    return json.dumps(feature, indent=2)


def quoted(text: str) -> str:
    return f'"{clip(text, 30)}"'


def ask(p: dict, config: RunnableConfig, label: str, title: str, prompt: str, output, rules,
        web: bool = False) -> tuple[dict, dict]:
    """One model call with at most one repair. Returns the output and the state update for its cost and repairs."""
    name = label.split()[0]  # the system prompt is prompts/<first word of the label>.md
    call = Call(label, system=(files("tattva") / "prompts" / f"{name}.md").read_text(), prompt=prompt,
                schema=json_schema(output), provider=p["provider"], model=p["model"], web=web, title=title,
                on_event=get_stream_writer())
    out, cost, broken = generate(call, rules, Path(config["configurable"]["root"]) / FAILED_DIR)
    return out, {"cost": cost, "repairs": [f"{label}: {rule}" for rule in broken]}


def say(stage: str, text: str) -> None:
    """A result line for the run log and the terminal."""
    get_stream_writer()({"type": "result", "stage": stage, "text": " ".join(text.split())})


# --- the nodes, in graph order ---

def research(state: State, config: RunnableConfig) -> dict:
    prompt = request(("Target", state["target"]), ("Language", state["lang"]))
    out, update = ask(state, config, "research", state["target"], prompt, ResearchOut, research_errors, web=True)
    say("research", f"brief of {len(out['brief'].split())} words from {len(out['references'])} references")
    return {**update, "brief": out["brief"], "references": out["references"]}


def design(state: State, config: RunnableConfig) -> dict:
    prompt = request(("Target", state["target"]), ("Language", state["lang"]),
                     ("Learner", LEARNER.format(lang=state["lang"])), ("Research brief", state["brief"]))
    out, update = ask(state, config, "design", state["target"], prompt, DesignOut, design_errors)
    say("design", f"{quoted(out['project']['name'])}: {len(out['features'])} features in {len(out['phases'])} phases")
    return {**update, "outline": out}


def breakdown(p: dict, config: RunnableConfig) -> dict:
    """One feature into checkpoints "<fi>.1", "<fi>.2", …"""
    fi, feature = p["fi"], p["feature"]
    earlier = [f["title"] for f in p["outline"]["features"][:fi - 1]]
    prompt = request(*context(p), ("Feature", json.dumps(feature, indent=2)),
                     ("Earlier features", [f"- {t}" for t in earlier] or "none, this is the first"))
    out, update = ask(p, config, f"breakdown {fi}", feature["title"], prompt, Drafts, lambda o: drafts_errors(o, 1))
    cps = {f"{fi}.{k}": record(f"{fi}.{k}", feature["id"], d, 0, "") for k, d in enumerate(out["checkpoints"], 1)}
    say("breakdown", f"{quoted(feature['title'])} → {len(cps)} checkpoints")
    return {**update, "cps": cps}


def judge(p: dict, config: RunnableConfig) -> dict:
    """Can the learner do this checkpoint in 20 minutes, with every earlier one already built?"""
    cp = p["cp"]
    prompt = request(*context(p), ("Feature", feature_json(p, cp)),
                     ("Already built", [f"- {t}" for t in p["earlier"]] or "nothing yet"),
                     ("Checkpoint to judge", cp["text"]))
    out, update = ask(p, config, f"judge {cp['id']}", cp["text"], prompt, JudgeOut, judge_errors)
    minutes, reason = out["minutes"], out["reason"]
    if minutes <= CHECKPOINT_MINUTES:
        status, flag_reason, said = "fits", "", f"fits (~{minutes} min)"
    elif cp["splits"] < MAX_SPLITS:
        status, flag_reason, said = "too_big", "", f"too big (~{minutes} min): {reason}"
    else:
        status, flag_reason = "flagged", f"still too big after {MAX_SPLITS} splits"
        said = f"still too big (~{minutes} min) after {MAX_SPLITS} splits, flagged: {reason}"
    say("judge", f"{quoted(cp['text'])} {said}")
    cp = {**cp, "status": status, "minutes": minutes, "reason": reason, "flag_reason": flag_reason}
    return {**update, "cps": {cp["id"]: cp}}


def split(p: dict, config: RunnableConfig) -> dict:
    """A too-big checkpoint into smaller ones, which coverage reviews before they replace it."""
    cp = p["cp"]
    sections = [*context(p), ("Feature", feature_json(p, cp)), ("Checkpoint", cp["text"]),
                ("Judge's reason", cp["reason"])]
    if cp["redo"]:
        sections.append(("Coverage problem to fix", cp["problem"]))
    out, update = ask(p, config, f"split {cp['id']}", cp["text"], request(*sections), Drafts,
                      lambda o: drafts_errors(o, 2))
    redo = ", redo" if cp["redo"] else ""
    say("judge", f"{quoted(cp['text'])} → {len(out['checkpoints'])} checkpoints "
                 f"(split {cp['splits'] + 1} of {MAX_SPLITS}{redo})")
    return {**update, "cps": {cp["id"]: {**cp, "status": "reviewing", "proposed": out["checkpoints"]}}}


def coverage(p: dict, config: RunnableConfig) -> dict:
    """Do the proposed children cover exactly the parent? Yes: they replace it. No: redo once, then flag."""
    cp = p["cp"]
    children = [f"{k}. {d['text']}" + (" (by hand)" if d["by_hand"] else "") for k, d in enumerate(cp["proposed"], 1)]
    prompt = request(*context(p, learner=False), ("Feature", feature_json(p, cp)), ("Parent", cp["text"]),
                     ("Children", children))
    out, update = ask(p, config, f"coverage {cp['id']}", cp["text"], prompt, CoverageOut, coverage_errors)
    problem = out["problem"]
    if out["covered"]:
        cps = {cp["id"]: {**cp, "status": "replaced"}}
        for k, d in enumerate(cp["proposed"], 1):
            cps[f"{cp['id']}.{k}"] = record(f"{cp['id']}.{k}", cp["feature"], d, cp["splits"] + 1, cp["text"])
        said = "covered"
    elif not cp["redo"]:
        cps = {cp["id"]: {**cp, "status": "too_big", "redo": True, "problem": problem}}
        said = f"not covered: {problem}"
    else:
        reason = "couldn't be split without losing behaviour: " + problem
        cps = {cp["id"]: {**cp, "status": "flagged", "flag_reason": reason}}
        said = f"not covered again, kept unsplit and flagged: {problem}"
    say("judge", f"{quoted(cp['text'])} {said}")
    return {**update, "cps": cps}


def build_steps(state: State, config: RunnableConfig) -> dict:
    """Group the settled checkpoints into steps. The model sees them as c1…cN; we map those back to real ids."""
    settled = active(state["cps"])
    lines = [f"c{n} [{c['feature']}] ~{c['minutes']} min{' FLAGGED' if c['status'] == 'flagged' else ''} {c['text']}"
             for n, c in enumerate(settled, 1)]
    prompt = request(*context(state), ("Checkpoints", lines))
    out, update = ask(state, config, "steps", f"{len(settled)} checkpoints", prompt, StepsOut,
                      lambda o: steps_errors(o, settled, state["outline"]))
    real = {f"c{n}": c["id"] for n, c in enumerate(settled, 1)}
    steps = [{**s, "checkpoints": [real[c] for c in s["checkpoints"]]} for s in out["steps"]]
    say("steps", f"{len(steps)} steps from {len(settled)} checkpoints")
    return {**update, "steps": steps}


def expand(p: dict, config: RunnableConfig) -> dict:
    """One step's detail: why, task, hints, checks…"""
    step = p["step"]
    listing = "\n\n".join(
        f"{s['id']}: {s['title']}\n  feature: {s['checkpoints'][0]['feature']}\n  goal: {s['goal']}\n  checkpoints:\n"
        + "\n".join(f"  - {c['text']}" + (" (by hand)" if c["by_hand"] else "") for c in s["checkpoints"])
        for s in p["steps"])
    references = [f"{r['title']} — {r['url']}" for r in p["references"]]
    prompt = request(*context(p), ("References", references), ("Steps", listing), ("Step to write", step["id"]),
                     ("Feature", feature_json(p, step["checkpoints"][0])))
    rules = detail_rules(p["outline"]["program"]["kind"])
    out, update = ask(p, config, f"expand {step['id']}", step["title"], prompt, Detail, rules, web=True)
    say("expand", f"{quoted(step['title'])} written, {len(out['checks'])} checks")
    return {**update, "details": {step["id"]: out}}


def write(state: State, config: RunnableConfig) -> dict:
    """Plain Python: spec.json, README.md, the step files, research.md and the registry entry."""
    spec = assemble_spec(state)
    errs, _ = validate(spec)
    if errs:
        raise RuntimeError("bug: assembled spec breaks the rules: " + "; ".join(errs))
    project = Project.create(Path(config["configurable"]["root"]), spec)
    project.save_spec()
    project.render()
    project.write(RESEARCH_FILE, state["brief"].encode())
    say("write", f"wrote {len(state['steps'])} step files")
    return {}


def assemble_spec(state: State) -> Spec:
    outline, cps = state["outline"], state["cps"]
    phase_of = {f["id"]: f["phase"] for f in outline["features"]}

    def checkpoint(c: dict) -> dict:
        return {"text": c["text"], "feature": c["feature"], "by_hand": c["by_hand"],
                "judge": {"fits": c["minutes"] <= CHECKPOINT_MINUTES, "minutes": c["minutes"], "reason": c["reason"]},
                "splits": c["splits"], "parent": c["parent"], "flagged": c["status"] == "flagged",
                "flag_reason": c["flag_reason"]}

    steps = []
    for s in state["steps"]:
        feature = cps[s["checkpoints"][0]]["feature"]  # steps_errors made sure a step has one feature
        steps.append({"id": s["id"], "phase": phase_of[feature], "feature": feature, "title": s["title"],
                      "goal": s["goal"], "checkpoints": [checkpoint(cps[c]) for c in s["checkpoints"]],
                      "concepts": s["concepts"], "prerequisites": s["prerequisites"],
                      "detail": state["details"][s["id"]]})
    project = outline["project"]
    return Spec.model_validate({
        "schema_version": SCHEMA_VERSION,
        "project": {"id": state["project_id"], "name": project["name"], "target": state["target"],
                    "language": state["lang"], "summary": project["summary"], "assumes": project["assumes"]},
        **{k: outline[k] for k in ("scope", "architecture", "program", "concepts", "phases", "features")},
        "references": state["references"],
        "steps": steps,
    })


# --- where to go next ---

def fan_breakdown(state: State) -> list[Send]:
    """One breakdown per feature, in parallel."""
    return [Send("breakdown", {**shared(state), "feature": f, "fi": fi})
            for fi, f in enumerate(state["outline"]["features"], 1)]


def dispatch(state: State) -> list[Send] | str:
    """Every unsettled checkpoint to its next node, in parallel; build_steps once they're all settled."""
    cps = active(state["cps"])
    sends = []
    for i, cp in enumerate(cps):
        if cp["status"] == "new":
            sends.append(Send("judge", {**shared(state), "cp": cp, "earlier": [c["text"] for c in cps[:i]]}))
        elif cp["status"] == "too_big":
            sends.append(Send("split", {**shared(state), "cp": cp}))
        elif cp["status"] == "reviewing":
            sends.append(Send("coverage", {**shared(state), "cp": cp}))
    return sends or "build_steps"


def fan_expand(state: State) -> list[Send]:
    """One expand per step, in parallel. Each sees every step with its checkpoint records."""
    steps = [{**s, "checkpoints": [state["cps"][c] for c in s["checkpoints"]]} for s in state["steps"]]
    return [Send("expand", {**shared(state), "references": state["references"], "step": s, "steps": steps})
            for s in steps]


def build_graph(checkpointer=None) -> CompiledStateGraph:
    retry = RetryPolicy(max_attempts=2, retry_on=CallFailed)  # a failed call runs once more, then the run stops
    g = StateGraph(State)
    for node in (research, design, breakdown, judge, split, coverage, build_steps, expand):
        g.add_node(node.__name__, node, retry_policy=retry)
    g.add_node("route", lambda s: {})  # does nothing: parallel judge/split/coverage calls meet here each round
    g.add_node("write", write)

    g.add_edge(START, "research")
    g.add_edge("research", "design")
    g.add_conditional_edges("design", fan_breakdown, ["breakdown"])
    g.add_edge("breakdown", "route")
    g.add_conditional_edges("route", dispatch, ["judge", "split", "coverage", "build_steps"])
    g.add_edge("judge", "route")
    g.add_edge("split", "route")
    g.add_edge("coverage", "route")
    g.add_conditional_edges("build_steps", fan_expand, ["expand"])
    g.add_edge("expand", "write")
    g.add_edge("write", END)
    return g.compile(checkpointer=checkpointer)
