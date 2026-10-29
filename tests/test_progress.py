import io
from datetime import datetime, timezone

import pytest

from tattva import TattvaError
from tattva import progress
from tattva.cli import run
from tattva.progress import (Event, Status, append_event, cmd_done, cmd_list, cmd_next, cmd_status, hint_note,
                             load_projects, progress_of, read_events, slug)
from tattva.project import Entry, Project, save_entry
from tattva.spec import PROGRESS_FILE, SPEC_FILE


def ev(step, event, minute, level=0):
    return Event(datetime(2026, 9, 24, 10, minute, tzinfo=timezone.utc), step, event, level)


@pytest.fixture
def proj(tmp_path, sample_spec):
    p = Project.create(tmp_path / "mini", sample_spec)
    p.save_spec()
    return p


def test_progress_of(sample_spec):
    pr = progress_of(sample_spec, [
        ev("create-run-script", "started", 1), ev("create-run-script", "completed", 2),
        ev("respond-to-ping", "started", 3), ev("ghost-step", "completed", 4),
        ev("respond-to-ping", "reflected", 5),  # a later version's event type: ignored
    ])
    assert pr.status == {"create-run-script": Status.DONE, "respond-to-ping": Status.IN_PROGRESS,
                         "get-and-set": Status.LOCKED}
    assert (pr.current, pr.done) == ("respond-to-ping", 1)
    assert any("1 progress events refer to steps no longer in the curriculum" in w for w in pr.warns)


def test_progress_of_picks_most_recently_started(sample_spec):
    sample_spec.steps[2].prerequisites = ["create-run-script"]
    pr = progress_of(sample_spec, [ev("create-run-script", "completed", 1), ev("get-and-set", "started", 3),
                                   ev("respond-to-ping", "started", 2)])
    assert pr.status["respond-to-ping"] == pr.status["get-and-set"] == Status.IN_PROGRESS
    assert pr.current == "get-and-set"
    only = progress_of(sample_spec, [ev("create-run-script", "completed", 1)])
    assert only.status["respond-to-ping"] == Status.AVAILABLE


def test_read_events_skips_broken_lines(tmp_path):
    path = tmp_path / "progress.jsonl"
    path.write_text('{"at":"2026-09-24T10:00:00Z","step":"a","event":"started"}\n{"at":"2026-09-24T10:0')
    events, warns = read_events(path)
    assert len(events) == 1 and any("line 2 isn't valid JSON" in w for w in warns)
    assert read_events(tmp_path / "missing") == ([], [])


def test_next_starts_first_available_step(proj):
    out = io.StringIO()
    cmd_next(proj, out)
    assert "Started: 01 · create-run-script →" in out.getvalue()
    out = io.StringIO()
    cmd_next(proj, out)
    assert "In progress: 01 · create-run-script" in out.getvalue()
    assert len(read_events(proj.abs(PROGRESS_FILE))[0]) == 1


def test_next_when_all_done(proj):
    for i in "123":
        cmd_done(proj, i, io.StringIO())
    out = io.StringIO()
    cmd_next(proj, out)
    assert out.getvalue() == "All 3 steps are done.\n"


def test_done(proj):
    out = io.StringIO()
    with pytest.raises(TattvaError, match="no step is in progress"):
        cmd_done(proj, "", out)
    cmd_next(proj, io.StringIO())
    cmd_done(proj, "", out)
    assert "Done: 01 · create-run-script (1/3 steps, 33%)" in out.getvalue()
    out = io.StringIO()
    cmd_done(proj, "create-run-script", out)
    assert "already done" in out.getvalue()
    out = io.StringIO()
    cmd_done(proj, "3", out)
    assert "prerequisite steps 02 aren't done" in out.getvalue()
    for bad in ["0", "4", "ghost"]:
        with pytest.raises(TattvaError, match="no step"):
            cmd_done(proj, bad, io.StringIO())


def test_status(proj):
    cmd_done(proj, "1", io.StringIO())
    proj.abs("curriculum/README.md").write_text("my own readme")
    proj.render()
    out = io.StringIO()
    cmd_status(proj, out)
    for want in ["Mini Redis: Redis in python · 1/3 steps done (33%)", "Phase 1 · Basics",
                 "  ✓ 01 create-run-script", "  ○ 02 respond-to-ping", "  · 03 get-and-set",
                 "edited outside tattva (not overwritten): curriculum/README.md"]:
        assert want in out.getvalue()


def test_progress_counts_hints(sample_spec):
    pr = progress_of(sample_spec, [ev("create-run-script", "hint", 1, 1), ev("create-run-script", "hint", 2, 2),
                                   ev("respond-to-ping", "hint", 3, 1)])
    assert pr.hints == {"create-run-script": 2, "respond-to-ping": 1}
    assert pr.status["create-run-script"] == Status.AVAILABLE


def test_status_shows_hint_counts_and_event_format(proj, monkeypatch):
    monkeypatch.setattr(progress, "now", lambda: datetime(2026, 10, 28, 10, tzinfo=timezone.utc))
    for level in (1, 2):
        append_event(proj, Event(None, "create-run-script", "hint", level))
    out = io.StringIO()
    cmd_status(proj, out)
    assert "  ○ 01 create-run-script (2 hints)\n" in out.getvalue()
    lines = proj.abs(PROGRESS_FILE).read_text().splitlines()
    assert lines[1] == '{"at":"2026-10-28T10:00:00Z","step":"create-run-script","event":"hint","level":2}'
    append_event(proj, Event(None, "x", "started"))
    assert proj.abs(PROGRESS_FILE).read_text().splitlines()[2] == '{"at":"2026-10-28T10:00:00Z","step":"x","event":"started"}'


def test_hint_note():
    assert [hint_note(n) for n in (0, 1, 3)] == ["", "1 hint", "3 hints"]


def test_slug():
    assert slug("Mini  Redis!") == "mini-redis"


def test_list(proj, tmp_path):
    cmd_next(proj, io.StringIO())
    gone = str(tmp_path / "gone")
    save_entry(Entry(id="x", path=gone, name="Gone Project", files={}))
    out = io.StringIO()
    cmd_list(out)
    s = out.getvalue()
    assert f"Gone Project  (missing: {gone})" in s
    assert "Mini Redis  0%  → 01 create-run-script  last activity " in s
    assert s.index("Gone Project") < s.index("Mini Redis")


def test_list_with_no_projects():
    out = io.StringIO()
    cmd_list(out)
    assert "No projects yet" in out.getvalue()


def test_list_survives_spec_that_breaks_rules(proj):
    proj.spec.steps = []
    proj.abs(SPEC_FILE).write_bytes(__import__("tattva.spec", fromlist=["x"]).encode_spec(proj.spec))
    out = io.StringIO()
    cmd_list(out)
    assert "Mini Redis  (its spec breaks the curriculum rules" in out.getvalue()


def test_list_survives_unreadable_progress_file(proj):
    proj.abs(PROGRESS_FILE).mkdir()
    out = io.StringIO()
    cmd_list(out)
    assert "Mini Redis  (can't read its progress: " in out.getvalue()


def test_list_reports_old_projects(tmp_path):
    old = tmp_path / "old"
    (old / "curriculum").mkdir(parents=True)
    (old / "curriculum/spec.json").write_text('{"schema_version": 1}')
    save_entry(Entry(id="x", path=str(old), name="Old Redis", files={}))
    out = io.StringIO()
    cmd_list(out)
    assert "Old Redis  (made by an older tattva" in out.getvalue()
    assert load_projects()[0][0].problem.startswith("made by an older tattva")


def test_cli_done_by_number(tmp_path, sample_spec):
    Project.create(tmp_path, sample_spec).save_spec()
    out = io.StringIO()
    run(tmp_path, ["done", "1"], out)
    assert out.getvalue().startswith("Done: 01 · ")


def test_cli_usage_errors(tmp_path, sample_spec):
    Project.create(tmp_path, sample_spec).save_spec()
    for argv, usage in [(["next", "x"], "usage: tattva next"), (["status", "x"], "usage: tattva status"),
                        (["list", "x"], "usage: tattva list"), (["done", "1", "2"], r"usage: tattva done \[step\]")]:
        with pytest.raises(TattvaError, match=usage):
            run(tmp_path, argv, io.StringIO())
