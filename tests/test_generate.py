import json

import pytest

from tattva import providers
from tattva.generate import REPAIR_PROMPT, InvalidOutput, add_cost, generate
from tattva.providers import Call, CallFailed, Result


def no_x(out):
    return ["x is forbidden"] if "x" in out else []


def fake(monkeypatch, results):
    calls = []

    def run(c):
        calls.append(c)
        r = results[len(calls) - 1]
        if isinstance(r, Exception):
            raise r
        return r

    monkeypatch.setattr(providers, "run_call", run)
    return calls


def call():
    return Call("design step", "sys", "make it", {"type": "object"}, model="m", title="T")


def test_passing_draft_makes_one_call(monkeypatch):
    calls = fake(monkeypatch, [Result({"ok": 1}, 0.5)])
    assert generate(call(), no_x, None) == ({"ok": 1}, 0.5, [])
    assert len(calls) == 1


def test_failing_draft_is_repaired_once(monkeypatch):
    calls = fake(monkeypatch, [Result({"x": 1}, 0.5), Result({"ok": 1}, 0.25)])
    assert generate(call(), no_x, None) == ({"ok": 1}, 0.75, ["x is forbidden"])
    first, repair = calls
    assert repair.label == "design step repair" and repair.system == REPAIR_PROMPT
    assert repair.web is False and repair.model == "m" and repair.schema == first.schema and repair.title == "T"
    assert repair.prompt == ("The answer below breaks these rules:\n- x is forbidden\n\nOriginal request:\nmake it"
                             "\n\nAnswer:\n" + json.dumps({"x": 1}, indent=2))


def test_still_invalid_saves_and_raises(monkeypatch, tmp_path):
    fake(monkeypatch, [Result({"x": 1}, 0.5), Result({"x": 2}, 0.25)])
    with pytest.raises(InvalidOutput) as e:
        generate(call(), no_x, tmp_path / "failed")
    err = e.value
    assert isinstance(err, CallFailed) and err.cost == 0.75 and err.output == {"x": 2}
    assert err.errors == ["x is forbidden"]
    assert err.saved.parent == tmp_path / "failed" and err.saved.name.startswith("design-step-")
    assert err.saved.suffix == ".json" and len(err.saved.stem) == len("design-step-20260101-000000")
    text = err.saved.read_text()
    assert json.loads(text) == {"errors": ["x is forbidden"], "output": {"x": 2}} and text.endswith("}\n")
    assert f"The output is saved in {err.saved}" in str(err)


def test_no_failed_dir_saves_nothing(monkeypatch):
    fake(monkeypatch, [Result({"x": 1}, None), Result({"x": 2}, 1.0)])
    with pytest.raises(InvalidOutput) as e:
        generate(call(), no_x, None)
    assert e.value.saved is None and e.value.cost is None and "saved" not in str(e.value)


def test_repair_call_failure_keeps_cost(monkeypatch):
    fake(monkeypatch, [Result({"x": 1}, 0.5), CallFailed("boom", 0.1)])
    with pytest.raises(CallFailed) as e:
        generate(call(), no_x, None)
    assert e.value.cost == 0.6


def test_add_cost():
    assert add_cost(1.0, 2.0) == 3.0 and add_cost(None, 2.0) is None and add_cost(1.0, None) is None
