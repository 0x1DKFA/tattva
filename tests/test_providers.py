import os
import threading
import time
from pathlib import Path

import pytest

from tattva import TattvaError, providers
from tattva.providers import (Call, CallFailed, CallTimedOut, Interrupted, check_provider, kill_children,
                              run_call)


def recorded_args(rec: Path) -> list[str]:
    return (rec / "args").read_text().split("\0")[:-1]


def test_claude_call(fake_cli):
    rec = fake_cli("claude", "ok")
    prompt = "Target: \"Redis\" with 'quotes'\nand a second line ✓"
    res = run_call(Call("design", "be precise", prompt, {"type": "object"}, web=True, model="sonnet"))
    assert res.output == {"answer": 42} and res.cost == 0.25
    assert recorded_args(rec) == [
        "-p", "--output-format", "stream-json", "--verbose", "--json-schema", '{"type":"object"}',
        "--system-prompt", "be precise", "--tools", "WebSearch,WebFetch", "--safe-mode", "--no-session-persistence",
        "--allowedTools", "WebSearch,WebFetch", "--model", "sonnet"]
    assert (rec / "stdin").read_text() == prompt
    cwd = Path((rec / "cwd").read_text().strip())
    assert cwd.name.startswith("tattva-call-") and cwd != Path.cwd() and not cwd.exists()


def test_claude_call_without_web_tools(fake_cli):
    rec = fake_cli("claude", "ok")
    run_call(Call("x", "s", "p", {}))
    assert recorded_args(rec) == [
        "-p", "--output-format", "stream-json", "--verbose", "--json-schema", "{}", "--system-prompt", "s",
        "--tools", "", "--safe-mode", "--no-session-persistence"]


@pytest.mark.parametrize("mode, want", [
    ("error", "claude: There is an issue with the selected model"),
    ("garbage", "segfault in module"),
    ("empty", "claude returned no structured output: I could not comply"),
])
def test_claude_errors(fake_cli, mode, want):
    fake_cli("claude", mode)
    evs = []
    with pytest.raises(CallFailed, match=want):
        run_call(Call("x", "s", "p", {}, on_event=evs.append))
    assert evs[-1]["type"] == "call_end" and want in evs[-1]["error"]


def test_claude_timeout_kills_the_process_group(fake_cli, monkeypatch):
    fake_cli("claude", "hang-child")  # the shell waits on a sleep that holds stdout open
    monkeypatch.setattr(providers, "CALL_TIMEOUT", 1)
    start = time.monotonic()
    with pytest.raises(CallTimedOut, match="claude timed out after 0 minutes"):
        run_call(Call("x", "s", "p", {}))
    assert time.monotonic() - start < 5
    assert not issubclass(CallTimedOut, CallFailed) and not issubclass(Interrupted, CallFailed)


def test_leftover_child_holding_stderr_neither_hangs_nor_survives(fake_cli):
    rec = fake_cli("claude", "leftover")  # exits at once, leaving a sleep that holds its stderr
    start = time.monotonic()
    assert run_call(Call("x", "s", "p", {})).output == {"answer": 42}
    assert time.monotonic() - start < 5
    pid = int((rec / "leftover").read_text())
    for _ in range(100):
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            break
        time.sleep(0.02)
    else:
        pytest.fail("the CLI's leftover child is still running")


@pytest.mark.parametrize("provider, want", [
    ("claude", "the claude CLI isn't on your PATH; install Claude Code and log in first"),
    ("codex", "the codex CLI isn't on your PATH; install Codex and sign in first"),
])
def test_missing_cli(tmp_path, monkeypatch, provider, want):
    monkeypatch.setenv("PATH", str(tmp_path))
    evs = []
    with pytest.raises(CallFailed, match=want):
        run_call(Call("x", "s", "p", {}, provider=provider, on_event=evs.append))
    assert evs[-1] == {"type": "call_end", "label": "x", "cost": None, "error": want}


def test_kill_children_interrupts_running_call(fake_cli):
    fake_cli("claude", "hang")
    err = []
    t = threading.Thread(target=lambda: err.append(pytest.raises(Interrupted, run_call, Call("x", "s", "p", {}))))
    t.start(); time.sleep(0.3); kill_children(); t.join(5)
    assert not t.is_alive() and err


def test_calls_after_kill_children_never_start(fake_cli):
    rec = fake_cli("claude", "ok")
    kill_children()
    with pytest.raises(Interrupted):
        run_call(Call("x", "s", "p", {}))
    assert not (rec / "args").exists()


def test_events_show_searches(fake_cli):
    fake_cli("claude", "ok")
    evs = []
    run_call(Call("design", "s", "p", {}, on_event=evs.append))
    assert {"type": "activity", "label": "design", "text": "searching: parquet spec", "show": True} in evs
    assert evs[0]["type"] == "call_start" and evs[-1] == {"type": "call_end", "label": "design", "cost": 0.25, "error": ""}


def test_events_log_claude_progress(fake_cli):
    fake_cli("claude", "ok")
    evs = []
    run_call(Call("design", "s", "p", {}, web=True, title="the outline", on_event=evs.append))
    assert evs[0] == {"type": "call_start", "label": "design", "title": "the outline", "provider": "claude", "model": ""}
    acts = [(e["text"], e["show"]) for e in evs if e["type"] == "activity"]
    for want, show in [
        ("calling claude: model default, web tools true", False),
        ("session: model test-model, tools StructuredOutput, WebSearch, WebFetch", False),
        ("claude: Looking up the spec.", False),
        ("searching: parquet spec", True),
        ('tool error: "fetch failed: 404"', False),
        ("reading: https://example.com/spec", True),
        ("rate_limit_event: ", False),
        ("stderr: a warning from claude", False),
        ("finished in ", False),
        (": 3 turns, $0.25", False),
    ]:
        assert any(want in text and s == show for text, s in acts), (want, acts)


def test_codex_call(fake_cli):
    rec = fake_cli("codex", "ok")
    prompt = "Research Redis and return JSON."
    evs = []
    res = run_call(Call("research", "be precise", prompt, {"type": "object"}, provider="codex", web=True,
                        model="gpt-test", on_event=evs.append))
    assert res.output == {"answer": 42} and res.cost is None
    args = recorded_args(rec)
    schema, answer = args[args.index("--output-schema") + 1], args[args.index("--output-last-message") + 1]
    assert args == [
        "--search", "exec", "--ignore-user-config", "--json", "--ephemeral", "--skip-git-repo-check",
        "--sandbox", "read-only", "--ask-for-approval", "never", "--output-schema", schema,
        "--output-last-message", answer, "--model", "gpt-test", "-"]
    assert Path(schema).name == "schema.json" and Path(schema).parent.name.startswith("tattva-call-")
    assert Path(answer).parent == Path(schema).parent and not Path(schema).parent.exists()
    assert (rec / "schema").read_text() == '{"type":"object"}'
    assert (rec / "stdin").read_text() == "Instructions:\nbe precise\n\nRequest:\n" + prompt
    assert {"type": "activity", "label": "research", "text": "codex: Done.", "show": True} in evs
    assert {"type": "activity", "label": "research", "text": 'codex event: {"type":"turn.started"}',
            "show": False} in evs
    assert evs[-1] == {"type": "call_end", "label": "research", "cost": None, "error": ""}


def test_codex_call_without_search(fake_cli):
    rec = fake_cli("codex", "ok")
    run_call(Call("x", "", "p", {}, provider="codex"))
    args = recorded_args(rec)
    assert "--search" not in args and "--model" not in args
    assert (rec / "stdin").read_text() == "p"


def test_codex_invalid_output(fake_cli, monkeypatch):
    fake_cli("codex", "ok")
    monkeypatch.setenv("FAKE_OUTPUT", "not json")
    with pytest.raises(CallFailed, match="codex returned invalid JSON: not json"):
        run_call(Call("x", "s", "p", {}, provider="codex"))


def test_check_provider():
    check_provider("claude")
    check_provider("codex")
    with pytest.raises(TattvaError, match=r'^unknown provider "x" \(choose claude or codex\)$'):
        check_provider("x")
