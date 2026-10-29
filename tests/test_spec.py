import json

import pytest

from tattva.spec import (
    Check, OldSpec, Spec, check_errors, decode_spec, detail_errors, encode_spec, is_text,
    json_schema, load_spec, safe_rel_path, step_file, validate,
)


def errs(s):
    return validate(s)[0]


def has(lst, sub):
    return any(sub in x for x in lst)


def test_sample_spec_is_valid(sample_spec):
    assert validate(sample_spec) == ([], [])


def test_round_trip(sample_spec, tmp_path):
    assert decode_spec(encode_spec(sample_spec)) == sample_spec
    assert b'"in": [' in encode_spec(sample_spec)
    p = tmp_path / "spec.json"
    p.write_bytes(encode_spec(sample_spec))
    assert load_spec(p) == sample_spec


def test_old_schema_is_reported_as_older_tattva():
    with pytest.raises(OldSpec, match="made by an older tattva"):
        decode_spec(b'{"schema_version": 1, "project": {}}')


def test_unknown_field_rejected(sample_spec):
    d = json.loads(encode_spec(sample_spec))
    d["steps"][0]["extra"] = 1
    with pytest.raises(ValueError):
        decode_spec(json.dumps(d).encode())


def test_load_spec_names_the_file(tmp_path):
    p = tmp_path / "spec.json"
    p.write_text('{"schema_version": 2}')
    with pytest.raises(ValueError, match="spec.json"):
        load_spec(p)


def test_step_file_and_index(sample_spec):
    assert step_file(0, "a-b") == "curriculum/steps/01-a-b.md"
    assert sample_spec.step_index("get-and-set") == 2
    assert sample_spec.step_index("nope") == -1


def test_step_needs_one_to_three_checkpoints(sample_spec):
    st = sample_spec.steps[1]
    st.checkpoints = st.checkpoints[:1] * 4
    assert any("has 4 checkpoints" in e for e in validate(sample_spec)[0])


def test_step_phase_must_match_its_feature(sample_spec):
    sample_spec.steps[2].phase = "basics"
    assert any("feature" in e and "phase" in e for e in validate(sample_spec)[0])


def test_checkpoint_feature_must_match_step(sample_spec):
    sample_spec.steps[0].checkpoints[0].feature = "ping"
    assert has(errs(sample_spec), "checkpoint feature")


def test_regex_uses_python_syntax(sample_spec):
    c = sample_spec.steps[1].detail.checks[0]
    c.match, c.expect = "regex", "(?P<x>a"
    assert any("regex doesn't compile" in e for e in check_errors("server", c))


def add_check(s, **kw):
    s.steps[0].detail.checks.append(Check(**{
        "name": "c", "args": [], "input": "", "path": "", "expect": "", "match": "exact", "exit_code": 0, **kw}))


def _undeclared_feature(s): s.steps[2].feature = "ghost"
def _dup_feature(s): s.features.append(s.features[0])
def _feature_phase(s): s.features[0].phase = "ghost"


RULES = [
    ("schema version", lambda s: setattr(s, "schema_version", 3), "schema_version is 3"),
    ("kebab", lambda s: setattr(s.architecture.components[1], "id", "Parser"), "component id 'Parser' is not kebab-case"),
    ("duplicate", lambda s: s.concepts.append(s.concepts[0]), "duplicate concept id 'tcp-streams'"),
    ("no phases", lambda s: setattr(s, "phases", []), "there are no phases"),
    ("undeclared phase", lambda s: setattr(s.steps[2], "phase", "nope"), "phase 'nope' is not declared"),
    ("phases out of order", lambda s: s.phases.reverse(), "grouped by phase in declared order"),
    ("empty phase", lambda s: s.phases.append(s.phases[0].model_copy(update={"id": "extra"})), "no empty phase"),
    ("forward prereq", lambda s: setattr(s.steps[1], "prerequisites", ["get-and-set"]), "is not an earlier step"),
    ("self prereq", lambda s: setattr(s.steps[1], "prerequisites", ["respond-to-ping"]), "is not an earlier step"),
    ("no concepts", lambda s: setattr(s.steps[2], "concepts", []), "has no concepts"),
    ("undeclared concept", lambda s: setattr(s.steps[2], "concepts", ["ghost"]), "concept 'ghost' is not declared"),
    ("unused concept", lambda s: s.concepts.append(s.concepts[0].model_copy(update={"id": "unused"})), "concept 'unused' is never used"),
    ("too many new concepts", lambda s: (
        s.concepts.extend(s.concepts[0].model_copy(update={"id": i}) for i in ("a", "b", "c")),
        setattr(s.steps[1], "concepts", ["a", "b", "c"])), "introduces 3 new concepts"),
    ("undeclared component", lambda s: setattr(s.architecture.components[1], "talks_to", ["ghost"]), "undeclared component 'ghost'"),
    ("server port", lambda s: setattr(s.program, "port", 80), "server port 80 must be between"),
    ("cli port", lambda s: (setattr(s.program, "kind", "cli"), setattr(s.program, "port", 7)), "cli programs have port 0"),
    ("too many steps", lambda s: s.steps.extend(s.steps[2].model_copy(update={"id": f"x-{i}"}) for i in range(97)), "there are 100 steps"),
    ("undeclared feature", _undeclared_feature, "feature 'ghost' is not declared"),
    ("duplicate feature", _dup_feature, "duplicate feature id 'skeleton'"),
    ("feature phase", _feature_phase, "phase 'ghost' is not declared"),
    ("empty detail field", lambda s: setattr(s.steps[0].detail, "why", " "), "why is empty"),
    ("hint count", lambda s: setattr(s.steps[0].detail, "hints", ["a", "b"]), "has 2 hints, want exactly 3"),
    ("no common mistakes", lambda s: setattr(s.steps[0].detail, "common_mistakes", []), "has no common mistakes"),
    ("exec on server", lambda s: setattr(s.steps[0].detail.checks[0], "do", "exec"), "exec checks are only for cli"),
    ("tcp on cli", lambda s: (setattr(s.program, "kind", "cli"), setattr(s.program, "port", 0)), "tcp checks are only for server"),
    ("tcp without input", lambda s: setattr(s.steps[0].detail.checks[0], "input", ""), "tcp checks need input"),
    ("unused check field", lambda s: setattr(s.steps[0].detail.checks[0], "exit_code", 1), "tcp checks only use input"),
    ("path escapes", lambda s: add_check(s, do="write", path="../x", input="hi"), "path '../x' must be relative"),
    ("absolute path", lambda s: add_check(s, do="file", path="/etc/passwd"), "path '/etc/passwd' must be relative"),
    ("bad regex", lambda s: (setattr(s.steps[0].detail.checks[0], "match", "regex"),
                             setattr(s.steps[0].detail.checks[0], "expect", "(")), "regex doesn't compile"),
    ("binary fixture", lambda s: add_check(s, do="write", path="a.db", input="SQLite\x00"), "input contains binary bytes"),
    ("binary expect", lambda s: setattr(s.steps[0].detail.checks[0], "expect", "\x01"), "expect contains binary bytes"),
]


@pytest.mark.parametrize("mutate,want", [r[1:] for r in RULES], ids=[r[0] for r in RULES])
def test_validate_rules(sample_spec, mutate, want):
    mutate(sample_spec)
    assert has(errs(sample_spec), want), errs(sample_spec)


def test_warns_when_step_has_no_checks(sample_spec):
    sample_spec.steps[0].detail.checks = []
    e, w = validate(sample_spec)
    assert e == [] and has(w, "step 'create-run-script': has no checks")


def test_detail_errors_alone(sample_spec):
    d = sample_spec.steps[0].detail
    assert detail_errors("server", d) == ([], [])


def test_is_text():
    assert is_text("a\tb\r\n") and not is_text("a\x00") and not is_text("\x7f")


def test_safe_rel_path():
    assert safe_rel_path("a/b.txt")
    assert not any(safe_rel_path(p) for p in ["", "/x", "../x", "a/../b", "a\\..\\b"])


def _keys(node):
    if isinstance(node, dict):
        for k, v in node.items():
            yield k
            yield from _keys(v) if k != "properties" else (x for s in v.values() for x in _keys(s))
    elif isinstance(node, list):
        for n in node:
            yield from _keys(n)


def test_schema_is_inline_and_strict():
    s = json_schema(Spec)
    text = json.dumps(s)
    assert "$ref" not in text and "$defs" not in text
    # "title" is also a real field name (Phase.title), so only look at schema keywords
    assert not {"title", "default", "description"} & set(_keys(s))
    assert "in" in s["properties"]["scope"]["properties"]
    assert s["additionalProperties"] is False
    assert set(s["required"]) == set(s["properties"])
    assert s["properties"]["program"]["properties"]["kind"]["enum"] == ["server", "cli"]
    check = s["properties"]["steps"]["items"]["properties"]["detail"]["properties"]["checks"]["items"]
    assert check["properties"]["do"]["enum"] == ["exec", "tcp", "write", "file"]
    assert check["additionalProperties"] is False
