import io
import threading
from pathlib import Path

from tattva.display import Display, RunLog


class FakeClock:
    def __init__(self, t):
        self.t = t

    def __call__(self):
        return self.t


def start(label, title="x"):
    return {"type": "call_start", "label": label, "title": title, "provider": "claude", "model": ""}


def test_panel_shows_running_calls_and_quiet_time(tmp_path):
    clock = FakeClock(0)
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), 'redis', "claude", live=False, clock=clock)
    d.handle("custom", start("judge 1.2", "AOF append on write"))
    clock.t = 30; d.handle("custom", {"type": "activity", "label": "judge 1.2", "text": "thinking", "show": False})
    lines = d.panel_lines(41)
    assert any('judge "AOF append on write"' in l and "41s · last output 11s ago" in l for l in lines)
    assert any("▶ judge" in l for l in lines)


def test_checkpoint_counts_from_values():
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "claude", live=False)
    d.handle("values", {"cps": {"1": {"status": "fits"}, "2": {"status": "too_big"}, "3": {"status": "flagged"}, "4": {"status": "new"}, "5": {"status": "replaced"}}})
    assert any(l.strip() == "checkpoints  1 fit · 1 splitting · 1 flagged · 1 waiting" for l in d.panel_lines(0))


def test_plain_mode_heartbeat():
    out = io.StringIO(); clock = FakeClock(0)
    d = Display(out, RunLog("new", io.StringIO()), "redis", "claude", live=False, clock=clock)
    d.handle("custom", start("judge 1.1"))
    d.tick(59); assert "still working" not in out.getvalue()
    d.tick(61); assert "… still working: judge ×1, last output 1m 01s ago" in out.getvalue()


def test_codex_cost_not_reported():
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "codex", live=False)
    d.handle("custom", {"type": "call_end", "label": "design", "cost": None, "error": ""})
    assert "cost not reported" in d.panel_lines(0)[1]
    assert "cost not reported" in d.summary({"steps": [], "cps": {}, "repairs": []})


def test_run_log_written(home):
    log = RunLog("new", io.StringIO()); log.write("hello"); log.close(None)
    text = log.path.read_text()
    assert "hello" in text and text.rstrip().endswith("done")
    assert log.path.parent == Path.home() / ".tattva" / "logs"


def test_cost_sums_every_call_end_and_formats_times():
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "claude", live=False, clock=FakeClock(0))
    d.handle("custom", {"type": "call_end", "label": "design", "cost": 1.5, "error": ""})
    d.handle("custom", {"type": "call_end", "label": "design", "cost": 2.62, "error": ""})
    assert d.panel_lines(3725)[1] == ' tattva new "redis" · 1:02:05 elapsed · $4.12 so far'
    d.handle("custom", start("split 1.1"))
    assert "1m 03s" in d.panel_lines(63)[-1]


def test_printed_lines_and_stages():
    out = io.StringIO(); clock = FakeClock(0)
    d = Display(out, RunLog("new", io.StringIO()), "redis", "claude", live=False, clock=clock)
    clock.t = 75
    d.handle("custom", {"type": "result", "stage": "judge", "text": "fits"})
    d.handle("custom", {"type": "activity", "label": "research", "text": "searching: resp", "show": True})
    d.handle("custom", {"type": "activity", "label": "research", "text": "quiet", "show": False})
    assert out.getvalue() == "01:15 judge     fits\n01:15 research  searching: resp\n"
    d.handle("custom", start("coverage 1.1"))
    row = [l for l in d.panel_lines(75) if "research" in l][0]
    assert row.strip() == "✓ research  ✓ design  ✓ breakdown  ▶ judge  · steps  · expand  · write"


def test_summary_lists_flagged():
    clock = FakeClock(0)
    d = Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "claude", live=False, clock=clock)
    clock.t = 65
    d.handle("custom", {"type": "call_end", "label": "x", "cost": 0.5, "error": ""})
    s = d.summary({"steps": [1, 2], "repairs": ["a"], "cps": {"1": {"status": "flagged", "text": "big one", "flag_reason": "too big"}, "2": {"status": "fits"}}})
    assert s.splitlines()[0] == "Finished in 01:05: 2 steps, 1 flagged checkpoints, 1 repairs, $0.50"
    assert "big one" in s and "too big" in s


def test_heartbeat_thread_stops():
    before = threading.active_count()
    with Display(io.StringIO(), RunLog("new", io.StringIO()), "redis", "claude", live=False):
        assert threading.active_count() == before + 1
    assert threading.active_count() == before


def test_failed_call_prints_a_line_and_keeps_cost_known():
    out = io.StringIO()
    d = Display(out, RunLog("new", io.StringIO()), "redis", "claude", live=False)
    d.handle("custom", {"type": "call_end", "label": "design", "cost": None, "error": "boom"})
    assert "design failed: boom" in out.getvalue()
    assert d.cost_known


def test_live_log_lines_keep_colon_names():
    out = io.StringIO()
    d = Display(out, RunLog("new", io.StringIO()), "redis", "claude", live=True)
    with d:
        d.handle("custom", {"type": "result", "stage": "design", "text": "redis user:id:name"})
    assert "user:id:name" in out.getvalue()
