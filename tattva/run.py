"""`tattva new`: runs the generation graph with a SQLite checkpointer, so a run that dies resumes where it stopped.

Read it top to bottom: checks → open the checkpointer → print the graph → run with the display → summary.
"""
import os
import shutil
import signal
import sqlite3
import subprocess
import sys
from contextlib import closing, contextmanager
from pathlib import Path
from typing import TextIO
from uuid import uuid4

from langgraph.checkpoint.sqlite import SqliteSaver

from tattva import TattvaError

from .display import Display, RunLog
from .graph import MAX_CONCURRENCY, build_graph
from .providers import CallFailed, CallTimedOut, Interrupted, check_provider, kill_children
from .spec import RUN_DIR, SPEC_FILE


def cmd_new(cwd: Path, target: str, lang: str, provider: str, model: str, out: TextIO) -> None:
    check_provider(provider)
    if (cwd / SPEC_FILE).exists():
        raise TattvaError(f"{SPEC_FILE} already exists here")
    (cwd / RUN_DIR).mkdir(parents=True, exist_ok=True)
    with closing(sqlite3.connect(cwd / RUN_DIR / "graph.sqlite", check_same_thread=False)) as conn:
        app = build_graph(SqliteSaver(conn))
        cfg = {"configurable": {"thread_id": "new", "root": str(cwd)}, "max_concurrency": MAX_CONCURRENCY}
        snap = app.get_state(cfg)
        saved = snap.values
        if saved and (saved["target"], saved["lang"], saved["provider"]) != (target, lang, provider):
            raise TattvaError(f'this folder has an unfinished run of tattva new "{saved["target"]}" '
                              f'--lang {saved["lang"]} --provider {saved["provider"]}; run that to resume it, '
                              f"or delete {RUN_DIR} to start over")
        if saved and not snap.next:
            raise TattvaError(f"{RUN_DIR} holds a finished run but {SPEC_FILE} is missing; "
                              f"delete {RUN_DIR} to start over")

        print(app.get_graph().draw_ascii(), file=out)
        if saved:
            print("Resuming the unfinished run…", file=out)
            start = None
        else:
            print("Starting…", file=out)
            start = {"target": target, "lang": lang, "provider": provider, "model": model,
                     "project_id": str(uuid4()), "cost": 0.0}

        log = RunLog("new", out)
        log.write(f'tattva new "{target}" --lang {lang} --provider {provider} --model {model or "default"} '
                  f"in {cwd}{' (resuming)' if saved else ''}")
        try:
            with awake(), signals_stop_run(), Display(out, log, target, provider) as display:
                stream(app, start, cfg, display)
            summary = display.summary(app.get_state(cfg).values)
            log.write(summary)
        except BaseException as e:
            log.close(e)
            raise
        log.close(None)
    print(summary, file=out)
    print("Run `tattva next` to start the first step.", file=out)


def stream(app, start: dict | None, cfg: dict, display: Display) -> None:
    """Runs the graph (start=None resumes it) into the display. A stopped run says how to resume it."""
    try:
        for mode, chunk in app.stream(start, cfg, stream_mode=["custom", "values"]):
            display.handle(mode, chunk)
    except KeyboardInterrupt:
        kill_children()  # the signal handler already did, unless the interrupt came from elsewhere
        raise TattvaError("interrupted; run the same `tattva new` again to resume") from None
    except (CallFailed, CallTimedOut, Interrupted) as e:
        raise TattvaError(f"{e}\nThe run is saved; run the same `tattva new` again to resume.") from None


@contextmanager
def signals_stop_run():
    """Ctrl-C or kill (SIGTERM) kills the running CLI children right away, then stops the run.
    Killing in the handler matters: LangGraph waits for running and queued nodes before the
    KeyboardInterrupt reaches us, and they must end at once instead of starting new children."""
    def stop(signum, frame):
        kill_children()
        signal.signal(signal.SIGINT, signal.SIG_DFL)  # a second Ctrl-C exits at once
        raise KeyboardInterrupt

    # ponytail: kill_children can't miss a starting child only because nodes run on worker threads: it waits
    # for providers._lock, held across Popen and adding the child to the live set. A node run on the main
    # thread could take the signal between the two and leak its child; block signals there if that changes.
    old = {s: signal.signal(s, stop) for s in (signal.SIGINT, signal.SIGTERM)}
    try:
        yield
    finally:
        for s, handler in old.items():
            signal.signal(s, handler)


@contextmanager
def awake():
    """On macOS, holds an idle-sleep assertion for the run, so an idle Mac doesn't sleep mid-run."""
    proc = None
    if sys.platform == "darwin" and shutil.which("caffeinate"):
        proc = subprocess.Popen(["caffeinate", "-i", "-w", str(os.getpid())])
    try:
        yield
    finally:
        if proc:
            proc.terminate()
            proc.wait()
