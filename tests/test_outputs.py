import copy

import pytest

from tattva.outputs import (coverage_errors, design_errors, detail_rules, drafts_errors,
                            judge_errors, research_errors, steps_errors)


def rec(id, feature, status="fits"):
    return {"id": id, "feature": feature, "text": "t" + id, "status": status, "minutes": 10}


def step(id, ids, goal="does x", concepts=None, prerequisites=None):
    return {"id": id, "title": id, "goal": goal, "checkpoints": ids,
            "concepts": concepts or ["x"], "prerequisites": prerequisites or []}


@pytest.fixture
def design_out():
    return {
        "project": {"name": "n", "summary": "s", "assumes": []},
        "scope": {"in": ["a"], "out": []},
        "architecture": {"real": "r", "diagram": "d", "components": [
            {"id": "srv", "responsibility": "r", "talks_to": ["db"]},
            {"id": "db", "responsibility": "r", "talks_to": []}]},
        "program": {"kind": "server", "port": 7000},
        "concepts": [{"id": "x", "name": "X", "summary": "s"}],
        "phases": [{"id": "core", "title": "c", "purpose": "p"},
                   {"id": "storage", "title": "s", "purpose": "p"}],
        "features": [{"id": "skeleton", "phase": "core", "title": "t", "description": "d"},
                     {"id": "ping", "phase": "storage", "title": "t", "description": "d"}],
    }


@pytest.fixture
def outline(design_out):
    return design_out


@pytest.fixture
def settled():
    return [rec("1", "skeleton"), rec("2", "ping"), rec("3", "ping")]


@pytest.fixture
def settled4():
    return [rec(str(i), "skeleton") for i in range(1, 5)]


@pytest.fixture
def settled_with_flag():
    return [rec("1", "skeleton"), rec("2", "skeleton", "flagged"), rec("3", "skeleton")]


def test_design_valid(design_out):
    assert design_errors(design_out) == []


def test_design_schema_mismatch():
    [e] = design_errors({})
    assert e.startswith("output doesn't match the schema: ")


def test_design_needs_features_in_every_phase(design_out):
    design_out["features"] = [f for f in design_out["features"] if f["phase"] != "storage"]
    assert any('phase "storage" has no features' in e for e in design_errors(design_out))


def test_design_features_grouped_by_phase(design_out):
    design_out["features"].reverse()
    assert any("grouped by phase" in e for e in design_errors(design_out))


def test_design_ids_talks_to_program(design_out):
    design_out["phases"][0]["id"] = "Bad"
    design_out["concepts"].append(copy.deepcopy(design_out["concepts"][0]))
    design_out["architecture"]["components"][0]["talks_to"] = ["nope"]
    design_out["program"]["port"] = 5
    errs = design_errors(design_out)
    assert any("not kebab-case" in e for e in errs)
    assert any("duplicate concept id" in e for e in errs)
    assert any("undeclared component" in e for e in errs)
    assert any("server port 5" in e for e in errs)


def test_steps_valid(settled, outline):
    out = {"steps": [step("a", ["c1"]), step("b", ["c2", "c3"], prerequisites=["a"])]}
    assert steps_errors(out, settled, outline) == []


def test_steps_must_read_c1_to_cn_in_order(settled, outline):
    out = {"steps": [step("a", ["c2"]), step("b", ["c1", "c3"])]}
    assert any("must list c1 to c3 in order" in e for e in steps_errors(out, settled, outline))


def test_steps_at_most_three_checkpoints(settled4, outline):
    out = {"steps": [step("a", ["c1", "c2", "c3", "c4"])]}
    assert any("has 4 checkpoints; the maximum is 3" in e for e in steps_errors(out, settled4, outline))


def test_steps_one_feature_each(settled, outline):
    out = {"steps": [step("a", ["c1", "c2"]), step("b", ["c3"])]}
    assert any("mixes checkpoints from features" in e for e in steps_errors(out, settled, outline))


def test_flagged_checkpoint_is_its_own_step(settled_with_flag, outline):
    out = {"steps": [step("a", ["c1"]), step("b", ["c2", "c3"])]}
    assert any("flagged checkpoint c2 must be a step on its own" in e
               for e in steps_errors(out, settled_with_flag, outline))


def test_steps_goal_and_concepts(settled, outline):
    long_goal = " ".join(["word"] * 31)
    out = {"steps": [step("a", ["c1"], goal=long_goal, concepts=["x", "y", "z"]),
                     step("b", ["c2"]), step("c", ["c3"])]}
    errs = steps_errors(out, settled, outline)
    assert any("goal has 31 words" in e for e in errs)
    assert any("introduces 3 new concepts" in e for e in errs)
    assert any("concept 'y' is not declared" in e for e in errs)


def test_steps_ids_prereqs_and_unused_concept(settled, outline):
    outline["concepts"].append({"id": "unused", "name": "U", "summary": "s"})
    out = {"steps": [step("a", ["c1"], prerequisites=["b"]), step("a", ["c2", "c3"])]}
    errs = steps_errors(out, settled, outline)
    assert any("duplicate step id" in e for e in errs)
    assert any("prerequisite 'b' is not an earlier step" in e for e in errs)
    assert any("concept 'unused' is never used" in e for e in errs)


def test_steps_concept_required(settled, outline):
    out = {"steps": [step("a", ["c1"]), step("b", ["c2", "c3"])]}
    out["steps"][0]["concepts"] = []
    assert any("has no concepts" in e for e in steps_errors(out, settled, outline))


def test_split_needs_two():
    drafts1 = {"checkpoints": [{"text": "t", "by_hand": False}]}
    assert drafts_errors(drafts1, at_least=2) == ["returned 1 checkpoints, want at least 2"]
    assert drafts_errors(drafts1, at_least=1) == []


def test_coverage_needs_a_problem_when_not_covered():
    assert coverage_errors({"covered": False, "problem": ""}) == ["problem is empty although covered is false"]
    assert coverage_errors({"covered": True, "problem": ""}) == []


def test_judge():
    assert judge_errors({"minutes": 0, "reason": " "}) == ["minutes is 0, want at least 1", "reason is empty"]
    assert judge_errors({"minutes": 5, "reason": "ok"}) == []


def test_research():
    assert research_errors({"brief": "", "references": []}) == ["brief is empty", "references is empty"]
    assert research_errors({"brief": "b", "references": [{"title": "t", "url": "u"}]}) == []


def test_detail_rules():
    d = {"why": "w", "context": "c", "task": "t", "constraints": [], "hints": ["a", "b"], "checks": [],
         "by_hand": "b", "expected_outcome": "e", "common_mistakes": ["m"], "reflection": "r"}
    assert detail_rules("cli")(d) == ["has 2 hints, want exactly 3"]
    assert detail_rules("cli")({})[0].startswith("output doesn't match the schema: ")
