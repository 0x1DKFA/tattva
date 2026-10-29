"""The run log in ~/.tattva/logs and the live progress display for `tattva new`."""

import json
import os
import threading
import time
from datetime import datetime
from pathlib import Path
from typing import TextIO

from rich.console import Console
from rich.live import Live
from rich.text import Text

from .graph import STAGES
from .project import registry_path
from .providers import clip

HEARTBEAT_SECONDS = 60


class RunLog:
    """One command's log file: every event, and how the command ended. Logging never stops a run."""

    def __init__(self, command: str, out: TextIO):
        self.path: Path | None = None
        self._f = None
        self._lock = threading.Lock()
        try:
            logs = registry_path().parent / "logs"
            logs.mkdir(parents=True, exist_ok=True)
            path = logs / f"{datetime.now():%Y-%m-%dT%H-%M-%S}-{command}-{os.getpid()}.log"
            self._f = open(path, "a")
        except OSError as e:
            print(f"  log: unavailable ({e})", file=out)
            return
        self.path = path
        print(f"  log: {path}", file=out)

    def write(self, msg: str) -> None:
        with self._lock:
            if self._f:
                self._f.write(f"{datetime.now():%Y-%m-%d %H:%M:%S.%f}"[:-3] + f" {msg}\n")
                self._f.flush()

    def close(self, err: BaseException | None) -> None:
        self.write(f"failed: {err}" if err else "done")
        with self._lock:
            if self._f:
                self._f.close()
                self._f = None


def stage_of(label: str) -> str:
    word = label.split()[0]
    return word if word in STAGES else "judge"  # split and coverage belong to judge


def mmss(seconds: float) -> str:
    m, s = divmod(int(seconds), 60)
    h, m = divmod(m, 60)
    return f"{h}:{m:02}:{s:02}" if h else f"{m:02}:{s:02}"


def age(seconds: float) -> str:
    m, s = divmod(int(seconds), 60)
    return f"{m}m {s:02}s" if m else f"{s}s"


class Display:
    def __init__(self, out: TextIO, log: RunLog, target: str, provider: str, live: bool | None = None,
                 clock=time.monotonic):
        self.out, self.log, self.target, self.clock = out, log, target, clock
        self.live = out.isatty() if live is None else live
        self.start = self.last_print = clock()
        self.cost = 0.0
        self.cost_known = provider != "codex"
        self.stage: str | None = None
        self.calls: dict[str, dict] = {}  # label -> title, start, last output
        self.counts = {"fits": 0, "too_big": 0, "reviewing": 0, "flagged": 0, "new": 0}
        self._lock = threading.RLock()
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self._live: Live | None = None

    def __enter__(self) -> "Display":
        if self.live:
            self._live = Live(get_renderable=lambda: Text("\n".join(self.panel_lines(self.clock()))),
                              console=Console(file=self.out, force_terminal=True), refresh_per_second=2)
            self._live.start()
        else:
            self._thread = threading.Thread(target=self._beat, daemon=True)
            self._thread.start()
        return self

    def __exit__(self, *exc) -> None:
        self._stop.set()
        if self._thread:
            self._thread.join()
        if self._live:
            self._live.stop()

    def _beat(self) -> None:
        while not self._stop.wait(1):
            self.tick(self.clock())

    def _print(self, line: str | None) -> None:
        """Called without self._lock: Live's refresh thread holds its own lock while it asks for the panel."""
        if line is None:
            return
        if self._live:
            self._live.console.print(line, markup=False, highlight=False, soft_wrap=True)
        else:
            print(line, file=self.out)

    def handle(self, mode: str, chunk) -> None:
        with self._lock:
            now = self.clock()
            line = None
            if mode == "values":
                cps = chunk.get("cps", {})
                self.counts = {k: sum(c["status"] == k for c in cps.values()) for k in self.counts}
            elif mode == "custom":
                self.log.write(json.dumps(chunk, ensure_ascii=False))
                line = self._event(chunk, now)
            if line:
                self.last_print = now
        self._print(line)

    def _event(self, e: dict, now: float) -> str | None:
        kind = e["type"]
        if kind == "call_start":
            self.stage = stage_of(e["label"])
            self.calls[e["label"]] = {"title": e["title"], "start": now, "last": now}
        elif kind == "activity":
            if e["label"] in self.calls:
                self.calls[e["label"]]["last"] = now
            if e["show"]:
                return f"{mmss(now - self.start)} {stage_of(e['label']):<9} {e['text']}"
        elif kind == "call_end":
            self.calls.pop(e["label"], None)
            if e["cost"] is None:
                self.cost_known = False
            else:
                self.cost += e["cost"]
        elif kind == "result":
            self.stage = stage_of(e["stage"])
            return f"{mmss(now - self.start)} {e['stage']:<9} {e['text']}"
        return None

    def _cost(self) -> str:
        return f"${self.cost:.2f}" if self.cost_known else "cost not reported"

    def panel_lines(self, now: float) -> list[str]:
        with self._lock:
            current = STAGES.index(self.stage) if self.stage else -1
            row = "  ".join(f"{'✓' if i < current else '▶' if i == current else '·'} {s}" for i, s in enumerate(STAGES))
            c = self.counts
            so_far = f"${self.cost:.2f} so far" if self.cost_known else "cost not reported"
            lines = ["─" * 54,
                     f' tattva new "{self.target}" · {mmss(now - self.start)} elapsed · {so_far}',
                     f" {row}",
                     f" checkpoints  {c['fits']} fit · {c['too_big'] + c['reviewing']} splitting"
                     f" · {c['flagged']} flagged · {c['new']} waiting"]
            for i, (label, call) in enumerate(self.calls.items()):
                head = " running      " if i == 0 else " " * 14
                lines.append(f"{head}{label.split()[0]} \"{clip(call['title'], 30)}\"  "
                             f"{age(now - call['start'])} · last output {age(now - call['last'])} ago")
            return lines

    def tick(self, now: float) -> None:
        """Plain mode: say so when a minute has passed without a printed line."""
        with self._lock:
            if now - self.last_print < HEARTBEAT_SECONDS:
                return
            self.last_print = now
            running: dict[str, int] = {}
            for label in self.calls:
                word = label.split()[0]
                running[word] = running.get(word, 0) + 1
            quiet = age(now - max((c["last"] for c in self.calls.values()), default=self.last_print))
            busy = ", ".join(f"{w} ×{n}" for w, n in running.items()) or "waiting"
            line = f"{mmss(now - self.start)} … still working: {busy}, last output {quiet} ago"
        self._print(line)

    def summary(self, state: dict) -> str:
        flagged = [c for c in state["cps"].values() if c["status"] == "flagged"]
        lines = [f"Finished in {mmss(self.clock() - self.start)}: {len(state['steps'])} steps, "
                 f"{len(flagged)} flagged checkpoints, {len(state['repairs'])} repairs, {self._cost()}"]
        lines += [f"  flagged: {c['text']}\n    {c['flag_reason']}" for c in flagged]
        return "\n".join(lines)
