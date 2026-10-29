import io
import os
import signal
import time

import pytest

from tattva import TattvaError
from tattva.cli import run
from tattva.run import cmd_new


def handlers():
    return [signal.getsignal(s) for s in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)]


def test_new_writes_curriculum_and_prints_graph(tmp_path, fake):
    out = io.StringIO()
    cmd_new(tmp_path, "Redis", "go", "claude", "", out)
    assert "build_steps" in out.getvalue() and "Starting…" in out.getvalue()
    assert (tmp_path / "curriculum/spec.json").exists()
    assert "Run `tattva next` to start the first step." in out.getvalue()


def test_resume_does_not_repeat_finished_calls(tmp_path, fake):
    fake.fail = {"expand " + fake.step_id(2): 2}
    with pytest.raises(TattvaError, match="again to resume"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    first = list(fake.labels); fake.labels.clear(); fake.fail = {}
    out = io.StringIO()
    cmd_new(tmp_path, "Redis", "go", "claude", "", out)
    assert "Resuming the unfinished run…" in out.getvalue()
    assert not any(l.startswith(("research", "design", "breakdown", "judge")) for l in fake.labels)
    assert ("expand " + fake.step_id(2)) in fake.labels


def test_refuses_to_mix_runs(tmp_path, fake):
    fake.fail = {"design": 2}
    with pytest.raises(TattvaError):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    with pytest.raises(TattvaError, match='unfinished run of tattva new "Redis" --lang go --provider claude'):
        cmd_new(tmp_path, "Redis", "rust", "claude", "", io.StringIO())


def test_refuses_when_curriculum_exists(tmp_path, fake):
    cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    with pytest.raises(TattvaError, match="already exists here"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())


def test_interrupt_kills_children_and_says_resume(tmp_path, fake, monkeypatch):
    killed = []
    monkeypatch.setattr("tattva.run.kill_children", lambda: killed.append(True))
    fake.raise_on = {"design": KeyboardInterrupt}
    before = handlers()
    with pytest.raises(TattvaError, match="interrupted; run the same `tattva new` again to resume"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    assert killed
    assert handlers() == before


@pytest.mark.parametrize("sig", [signal.SIGTERM, signal.SIGHUP], ids=["SIGTERM", "SIGHUP"])
def test_kill_signals_kill_children_while_they_run(tmp_path, fake, monkeypatch, sig):
    killed, killed_during_call = [], []
    monkeypatch.setattr("tattva.run.kill_children", lambda: killed.append(True))

    def call_that_gets_killed(call):
        if call.label == "design":
            os.kill(os.getpid(), sig)
            deadline = time.monotonic() + 2
            while not killed and time.monotonic() < deadline:
                time.sleep(0.01)
            killed_during_call.append(bool(killed))  # the handler killed at once, not after the graph waited
        return fake(call)
    monkeypatch.setattr("tattva.providers.run_call", call_that_gets_killed)
    before = handlers()
    with pytest.raises(TattvaError, match="interrupted; run the same `tattva new` again to resume"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    assert killed_during_call == [True]
    assert handlers() == before


def test_sighup_stays_ignored_under_nohup(tmp_path, fake, monkeypatch):
    seen = []

    def call(c):
        seen.append(signal.getsignal(signal.SIGHUP))
        return fake(c)
    monkeypatch.setattr("tattva.providers.run_call", call)
    old = signal.signal(signal.SIGHUP, signal.SIG_IGN)
    try:
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
        assert set(seen) == {signal.SIG_IGN} and signal.getsignal(signal.SIGHUP) == signal.SIG_IGN
    finally:
        signal.signal(signal.SIGHUP, old)


def test_interrupt_outside_the_stream_says_resume(tmp_path, fake, monkeypatch):
    killed = []
    monkeypatch.setattr("tattva.run.kill_children", lambda: killed.append(True))

    def interrupted(self):
        raise KeyboardInterrupt
    monkeypatch.setattr("tattva.run.Display.__enter__", interrupted)
    with pytest.raises(TattvaError, match="interrupted; run the same `tattva new` again to resume"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    assert killed


def test_resume_keeps_and_logs_the_saved_model(tmp_path, fake):
    fake.fail = {"design": 2}
    with pytest.raises(TattvaError):
        cmd_new(tmp_path, "Redis", "go", "claude", "sonnet", io.StringIO())
    fake.fail = {}
    cmd_new(tmp_path, "Redis", "go", "claude", "opus", io.StringIO())
    assert {c.model for c in fake.calls} == {"sonnet"}
    log = "".join(f.read_text() for f in (tmp_path / "home/.tattva/logs").iterdir())
    assert '--model sonnet in' in next(line for line in log.splitlines() if "(resuming)" in line)


def test_refuses_finished_run_without_spec(tmp_path, fake):
    cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())
    (tmp_path / "curriculum/spec.json").unlink()
    with pytest.raises(TattvaError, match="holds a finished run but curriculum/spec.json is missing"):
        cmd_new(tmp_path, "Redis", "go", "claude", "", io.StringIO())


def test_cli_new_checks_provider(tmp_path):
    with pytest.raises(TattvaError, match='unknown provider "gpt"'):
        run(tmp_path, ["new", "Redis", "--lang", "go", "--provider", "gpt"], io.StringIO())


@pytest.mark.parametrize("argv", [["new", "Redis"], ["new", "--lang", "go"], ["new", "Redis", "--lang", "go", "x"],
                                  ["new", "Redis", "--lang"]])
def test_cli_new_usage(tmp_path, argv):
    with pytest.raises(TattvaError, match=r'^usage: tattva new "<target>" --lang <lang> \[--provider <p>\]'):
        run(tmp_path, argv, io.StringIO())
