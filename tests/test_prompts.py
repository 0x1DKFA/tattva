from importlib.resources import files

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
