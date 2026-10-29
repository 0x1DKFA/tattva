import pytest
from conftest import graph_input, run_graph

from tattva.graph import LEARNER, active, build_graph
from tattva.providers import CallFailed
from tattva.spec import load_spec, validate


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


def test_requests_carry_the_sections_the_prompts_name(tmp_path, fake):
    run_graph(tmp_path)
    assert "Target: Redis" in fake.prompt("design") and "Language: go" in fake.prompt("design")
    assert "Learner:" not in fake.prompt("research")
    assert "c1 [f1] ~10 min feature f1 checkpoint 1" in fake.prompt("steps")
    expand = fake.prompt("expand step-2")
    assert "References:\nRESP spec — https://redis.io/" in expand
    assert expand.endswith("Step to write: step-2")


def test_results_are_streamed_and_costs_add_up(tmp_path, fake):
    fake.breakdown = {"1": ["BIG parse"]}; fake.covered = [False, True]; fake.judge_big_once = True
    chunks = list(build_graph().stream(graph_input(), {"configurable": {"root": str(tmp_path)}},
                                       stream_mode=["custom", "values"]))
    said = [c["text"] for mode, c in chunks if mode == "custom" and c["stage"] == "judge"]
    assert '"BIG parse" too big (~45 min): two new ideas' in said
    assert '"BIG parse" → 2 checkpoints (split 1 of 3)' in said
    assert '"BIG parse" not covered: the error reply is in no child' in said
    assert '"BIG parse" covered' in said
    assert "Learner:" not in fake.prompt("coverage 1.1")
    assert [c for mode, c in chunks if mode == "values"][-1]["cost"] == pytest.approx(0.01 * len(fake.labels))
