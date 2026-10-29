"""Model calls through the claude and codex CLIs, run as subprocesses."""
import json
import os
import signal
import subprocess
import tempfile
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

from tattva import TattvaError

MAX_CALLS = 4  # CLI children running at once, whatever the graph schedules
CALL_TIMEOUT = 45 * 60  # seconds  # ponytail: fixed guard against runaway calls; large outlines took 15-20+ min at 20


@dataclass
class Call:
    label: str                 # "judge 1.2": names the call in the log and the panel
    system: str
    prompt: str
    schema: dict
    provider: str = "claude"
    model: str = ""
    web: bool = False
    title: str = ""            # short subject for the panel, such as the checkpoint text
    on_event: Callable[[dict], None] = lambda ev: None


@dataclass
class Result:
    output: dict
    cost: float | None         # None: the provider doesn't report cost (codex)


class CallFailed(Exception):
    """A failed call; the graph retries it once."""

    def __init__(self, message: str, cost: float | None = None):
        super().__init__(message)
        self.cost = cost  # what the failed call still cost, when the CLI said


class CallTimedOut(Exception):
    """A call that ran past CALL_TIMEOUT; not retried."""


class Interrupted(Exception):
    """A call stopped by kill_children; not retried."""


NOT_ON_PATH = {
    "claude": "the claude CLI isn't on your PATH; install Claude Code and log in first",
    "codex": "the codex CLI isn't on your PATH; install Codex and sign in first",
}

_lock = threading.RLock()  # re-entrant, so kill_children can run in a signal handler
_live: dict[subprocess.Popen, str] = {}  # running child -> why it was killed ("" while it wasn't)
_stopped = False  # set by kill_children: later calls are interrupted before they start
_slots = threading.BoundedSemaphore(MAX_CALLS)


def check_provider(name: str) -> None:
    if name not in ("claude", "codex"):
        raise TattvaError(f'unknown provider "{name}" (choose claude or codex)')


def kill_children() -> None:
    """Kills every running CLI child (their run_call raises Interrupted) and stops new ones starting."""
    global _stopped
    with _lock:
        _stopped = True
        for proc in list(_live):
            _kill(proc, "interrupted")


def _kill(proc: subprocess.Popen, why: str) -> None:
    with _lock:
        if proc in _live and proc.returncode is None:
            _live[proc] = _live[proc] or why
            _killpg(proc.pid)


def _killpg(pgid: int) -> None:
    try:
        os.killpg(pgid, signal.SIGKILL)  # the whole group, so the CLI's own children die too
    except OSError:
        pass  # already gone


def clip(s: str, n: int) -> str:
    """One line, at most n characters, "…" when cut."""
    s = " ".join(s.split())
    return s[:n] + "…" if len(s) > n else s


def last_line(s: str) -> str:
    return s.strip().split("\n")[-1]


def _compact(v) -> str:
    return json.dumps(v, separators=(",", ":"))


def run_call(call: Call) -> Result:
    """Runs one call; events go to call.on_event, call_start first and call_end last."""
    def emit(type: str, **fields) -> None:
        call.on_event({"type": type, "label": call.label, **fields})

    def log(text: str, show: bool = False) -> None:
        emit("activity", text=text, show=show)

    emit("call_start", title=call.title, provider=call.provider, model=call.model)
    try:
        check_provider(call.provider)
        res = (_claude if call.provider == "claude" else _codex)(call, log)
    except BaseException as e:
        emit("call_end", cost=getattr(e, "cost", None), error=str(e) or type(e).__name__)
        raise
    emit("call_end", cost=res.cost, error="")
    return res


def _run(cmd: list[str], cwd: str, prompt: str, on_line: Callable[[str], None],
         log: Callable[[str], None], stderr_label: str) -> tuple[int, str]:
    """Runs cmd in cwd with prompt on stdin, passing each stdout line to on_line.
    Returns the exit code and stderr; raises CallTimedOut or Interrupted when it was killed."""
    # stderr goes to a file, not a pipe: a leftover child of the CLI holding a pipe open would block us
    with _slots, tempfile.TemporaryFile() as errf:  # after kill_children, each waiter gets a slot and stops at once
        with _lock:  # so kill_children can't miss a child that is starting
            if _stopped:
                raise Interrupted("interrupted")
            try:
                proc = subprocess.Popen(cmd, cwd=cwd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=errf,
                                        encoding="utf-8", errors="replace", start_new_session=True)
            except FileNotFoundError:
                raise CallFailed(NOT_ON_PATH[cmd[0]]) from None
            _live[proc] = ""
        timer = threading.Timer(CALL_TIMEOUT, _kill, (proc, "timeout"))
        with proc:
            timer.start()
            try:
                try:
                    with proc.stdin as w:
                        w.write(prompt)
                except BrokenPipeError:
                    pass  # it exited without reading it all; its exit status and stderr say why
                for line in proc.stdout:  # a leftover holding stdout keeps the CLI unreaped, so still killable
                    on_line(line.rstrip("\n"))
                proc.wait()
            finally:
                timer.cancel()
                _kill(proc, "")  # only does anything when an error left it running
                proc.wait()
                with _lock:
                    why = _live.pop(proc)
                    _killpg(proc.pid)  # leftovers the CLI started; its group outlives it while they run
        errf.seek(0)
        err = errf.read().decode("utf-8", "replace")
    for line in err.strip().splitlines():
        log(f"{stderr_label}: {line}")
    if why == "timeout":
        raise CallTimedOut(f"{cmd[0]} timed out after {CALL_TIMEOUT // 60} minutes")
    if why:
        raise Interrupted("interrupted")
    return proc.returncode, err


def _claude(call: Call, log) -> Result:
    began = time.monotonic()
    tools = "WebSearch,WebFetch" if call.web else ""
    cmd = ["claude", "-p",
           "--output-format", "stream-json",
           "--verbose",  # stream-json requires it with -p
           "--json-schema", _compact(call.schema),
           "--system-prompt", call.system,
           "--tools", tools,
           "--safe-mode",  # keeps the user's CLAUDE.md, hooks, plugins and MCP servers out
           "--no-session-persistence"]
    if call.web:
        cmd += ["--allowedTools", tools]
    if call.model:
        cmd += ["--model", call.model]
    log(f"calling claude: model {call.model or 'default'}, web tools {str(call.web).lower()}, "
        f"prompt {len(call.prompt.encode())} bytes")
    res = None

    def on_line(line: str) -> None:
        nonlocal res
        try:
            ev = json.loads(line)
            lines = list(_claude_lines(ev, line))
        except (ValueError, AttributeError, TypeError):  # not the shape we expect: log it raw
            lines = [(f"stdout: {clip(line, 300)}", False)]
        else:
            if ev.get("type") == "result":
                res = ev
        for text, show in lines:
            log(text, show)

    with tempfile.TemporaryDirectory(prefix="tattva-call-") as cwd:
        code, stderr = _run(cmd, cwd, call.prompt, on_line, log, "stderr")
    if res is None:
        raise CallFailed(f"claude failed (exit status {code}): {last_line(stderr)}")
    cost = float(res.get("total_cost_usd") or 0)
    if res.get("is_error"):
        raise CallFailed(f"claude: {res.get('result', '')}", cost)
    if code:
        raise CallFailed(f"claude failed (exit status {code}): {last_line(stderr)}", cost)
    if res.get("structured_output") is None:
        raise CallFailed(f"claude returned no structured output: {last_line(res.get('result') or '')}", cost)
    log(f"finished in {round(time.monotonic() - began)}s: {res.get('num_turns', 0)} turns, ${cost:.2f}")
    return Result(res["structured_output"], cost)


def _claude_lines(ev: dict, line: str):
    """Yields (text, show) for one stream-json event. Searches and fetched pages show in the terminal."""
    kind = ev.get("type", "")
    blocks = (ev.get("message") or {}).get("content") or []
    if kind == "system" and ev.get("subtype") == "init":
        yield f"session: model {ev.get('model', '')}, tools {', '.join(ev.get('tools') or [])}", False
    elif kind == "assistant":
        for b in blocks:
            if b.get("type") == "text":
                yield f"claude: {clip(b.get('text', ''), 500)}", False
            elif b.get("type") == "tool_use":
                name, args = b.get("name"), b.get("input") or {}
                if name == "StructuredOutput":
                    yield f"answer written ({len(_compact(args))} bytes)", False
                elif name == "WebSearch" and args.get("query"):
                    yield f"searching: {clip(args['query'], 200)}", True
                elif name == "WebFetch" and args.get("url"):
                    yield f"reading: {clip(args['url'], 200)}", True
                else:
                    yield f"tool {name}: {clip(json.dumps(args, ensure_ascii=False), 300)}", False
    elif kind == "user":
        for b in blocks:
            if b.get("type") == "tool_result" and b.get("is_error"):
                yield f"tool error: {clip(json.dumps(b.get('content'), ensure_ascii=False), 300)}", False
    elif kind != "result":  # run_call reports how the call ended
        yield f"{kind}: {clip(line, 300)}", False


def _codex(call: Call, log) -> Result:
    with tempfile.TemporaryDirectory(prefix="tattva-call-") as cwd:
        schema, answer = Path(cwd, "schema.json"), Path(cwd, "answer.json")
        schema.write_text(_compact(call.schema))
        cmd = ["codex", *(["--search"] if call.web else []),
               "exec", "--ignore-user-config", "--json", "--ephemeral", "--skip-git-repo-check",
               "--sandbox", "read-only", "--ask-for-approval", "never",
               "--output-schema", str(schema), "--output-last-message", str(answer)]
        if call.model:
            cmd += ["--model", call.model]
        cmd.append("-")
        prompt = f"Instructions:\n{call.system}\n\nRequest:\n{call.prompt}" if call.system else call.prompt
        log(f"calling codex: model {call.model or 'default'}, web search {str(call.web).lower()}, "
            f"prompt {len(call.prompt.encode())} bytes")

        def on_line(line: str) -> None:
            try:
                text, show = _codex_line(json.loads(line), line)
            except (ValueError, AttributeError, TypeError):  # not the shape we expect: log it raw
                text, show = f"codex output: {clip(line, 300)}", False
            log(text, show)

        code, stderr = _run(cmd, cwd, prompt, on_line, log, "codex stderr")
        if code:
            raise CallFailed(f"codex failed (exit status {code}): {last_line(stderr)}")
        try:
            text = answer.read_text()
        except FileNotFoundError:
            raise CallFailed(f"codex returned no structured output: {last_line(stderr)}") from None
    try:
        output = json.loads(text)
    except ValueError:
        raise CallFailed(f"codex returned invalid JSON: {clip(text, 200)}") from None
    log("finished; Codex does not report call cost")
    return Result(output, None)


def _codex_line(ev: dict, line: str) -> tuple[str, bool]:
    item = ev.get("item") or {}
    action = item.get("action") or {}
    if item.get("type") == "agent_message" and item.get("text"):
        return f"codex: {clip(item['text'], 300)}", True
    if item.get("type") == "web_search_call":
        query = action.get("query") or ", ".join(action.get("queries") or [])
        if query:
            return f"searching: {clip(query, 200)}", True
        if action.get("url"):
            return f"reading: {clip(action['url'], 200)}", True
    return f"codex event: {clip(line, 300)}", False
