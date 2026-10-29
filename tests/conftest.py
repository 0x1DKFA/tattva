import os

import pytest

# Stand-ins for the claude and codex CLIs (ported from claude_test.go and codex_test.go). They record their
# arguments (NUL-separated), stdin and working directory in $FAKE_DIR, then reply per $FAKE_MODE.
FAKE_CLIS = {
    "claude": r"""#!/bin/sh
printf '%s\0' "$@" > "$FAKE_DIR/args"
cat > "$FAKE_DIR/stdin"
pwd > "$FAKE_DIR/cwd"
case "$FAKE_MODE" in
ok) printf '%s\n' \
  '{"type":"system","subtype":"init","model":"test-model","tools":["StructuredOutput","WebSearch","WebFetch"]}' \
  '{"type":"assistant","message":{"content":[{"type":"text","text":"Looking up the spec."},{"type":"tool_use","name":"WebSearch","input":{"query":"parquet spec"}}]}}' \
  '{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":"fetch failed: 404"}]}}' \
  '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"WebFetch","input":{"url":"https://example.com/spec","prompt":"read it"}}]}}' \
  '{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}' \
  '{"type":"result","subtype":"success","is_error":false,"result":"{}","structured_output":{"answer":42},"total_cost_usd":0.25,"num_turns":3}'
  echo "a warning from claude" >&2 ;;
error) printf '%s\n' '{"type":"result","is_error":true,"result":"There is an issue with the selected model","total_cost_usd":0}'; exit 1 ;;
garbage) echo "not json"; echo "segfault in module" >&2; exit 2 ;;
empty) printf '%s\n' '{"type":"result","is_error":false,"result":"I could not comply","total_cost_usd":0.1}' ;;
hang) exec sleep 10 ;;
hang-child) sleep 10 & wait ;;
leftover) sleep 10 >/dev/null & echo $! > "$FAKE_DIR/leftover"
  printf '%s\n' '{"type":"result","is_error":false,"result":"","structured_output":{"answer":42},"total_cost_usd":0.1,"num_turns":1}' ;;
esac
""",
    "codex": r"""#!/bin/sh
printf '%s\0' "$@" > "$FAKE_DIR/args"
cat > "$FAKE_DIR/stdin"
schema=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-schema) schema="$2"; shift 2 ;;
    --output-last-message) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cp "$schema" "$FAKE_DIR/schema"
if [ -n "$FAKE_OUTPUT" ]; then
  printf '%s\n' "$FAKE_OUTPUT" > "$output"
else
  printf '%s\n' '{"answer":42}' > "$output"
fi
printf '%s\n' '{"type":"turn.started"}' '{"type":"item.completed","item":{"type":"agent_message","text":"Done."}}'
""",
}


@pytest.fixture(autouse=True)
def home(tmp_path, monkeypatch):
    monkeypatch.setenv("HOME", str(tmp_path / "home"))


@pytest.fixture(autouse=True)
def providers_not_stopped(monkeypatch):
    monkeypatch.setattr("tattva.providers._stopped", False)  # undoes any kill_children() after the test


@pytest.fixture
def fake_cli(tmp_path, monkeypatch):
    """fake_cli(name, mode) puts a fake claude or codex first on PATH and returns the dir it records into."""
    def install(name: str, mode: str):
        bindir, rec = tmp_path / "fake-bin", tmp_path / "fake-rec"
        bindir.mkdir(exist_ok=True)
        rec.mkdir(exist_ok=True)
        (bindir / name).write_text(FAKE_CLIS[name])
        (bindir / name).chmod(0o755)
        monkeypatch.setenv("PATH", f"{bindir}{os.pathsep}{os.environ['PATH']}")
        monkeypatch.setenv("FAKE_DIR", str(rec))
        monkeypatch.setenv("FAKE_MODE", mode)
        return rec
    return install


@pytest.fixture
def sample_spec():
    from tattva.spec import Spec

    def cp(text, feature, flagged=False):
        return {
            "text": text, "feature": feature, "by_hand": False,
            "judge": {"fits": not flagged, "minutes": 15, "reason": "small enough"},
            "splits": 3 if flagged else 0, "parent": "",
            "flagged": flagged,
            "flag_reason": "still too big after 3 splits" if flagged else "",
        }

    def detail(port_msg):
        return {
            "why": "Every check starts your program through run.sh.",
            "context": "Nothing exists yet.",
            "task": port_msg,
            "constraints": ["standard library only"],
            "hints": ["Servers listen, then accept.", "Listen on TCP port 6379.", "listen; loop: accept"],
            "checks": [{"name": "accepts a connection", "do": "tcp", "args": [], "input": "PING\r\n",
                        "path": "", "expect": "", "match": "contains", "exit_code": 0}],
            "by_hand": "Run `./run.sh`, then `nc localhost 6379`.",
            "expected_outcome": "The server accepts connections.",
            "common_mistakes": ["Forgetting to make run.sh executable."],
            "reflection": "What happens if two programs listen on the same port?",
        }

    def step(id, phase, feature, concepts, pre, cps):
        return {"id": id, "phase": phase, "feature": feature, "title": id, "goal": "Do " + id + ".",
                "checkpoints": cps, "concepts": concepts, "prerequisites": pre,
                "detail": detail("Write it so it works on port 6379.")}

    return Spec.model_validate({
        "schema_version": 2,
        "project": {"id": "00000000-0000-4000-8000-000000000000", "name": "Mini Redis", "target": "Redis",
                    "language": "python", "summary": "A tiny Redis.", "assumes": ["comfortable with Python"]},
        "scope": {"in": ["TCP server", "PING"],
                  "out": [{"feature": "persistence", "why": "not needed to learn the protocol"}]},
        "architecture": {
            "real": "Redis is a single-threaded event-loop server.",
            "components": [{"id": "server", "responsibility": "accept connections", "talks_to": ["parser"]},
                           {"id": "parser", "responsibility": "parse RESP", "talks_to": []}],
            "diagram": "flowchart LR\n  server --> parser"},
        "program": {"kind": "server", "port": 6379},
        "concepts": [{"id": "tcp-streams", "name": "TCP streams", "summary": "Bytes, not messages."},
                     {"id": "resp-framing", "name": "RESP framing", "summary": "Finding message boundaries."}],
        "references": [{"title": "RESP spec", "url": "https://redis.io/docs/latest/develop/reference/protocol-spec/"}],
        "phases": [{"id": "basics", "title": "Basics", "purpose": "Get a program running."},
                   {"id": "storage", "title": "Storage", "purpose": "Store values."}],
        "features": [{"id": "skeleton", "phase": "basics", "title": "Skeleton", "description": "run.sh and a listener."},
                     {"id": "ping", "phase": "basics", "title": "PING", "description": "Reply to PING."},
                     {"id": "get-set", "phase": "storage", "title": "GET/SET", "description": "Store values."}],
        "steps": [
            step("create-run-script", "basics", "skeleton", ["tcp-streams"], [],
                 [cp("`./run.sh` keeps running", "skeleton"), cp("port 6379 accepts a connection", "skeleton")]),
            step("respond-to-ping", "basics", "ping", ["resp-framing"], ["create-run-script"],
                 [cp("PING gets +PONG", "ping")]),
            step("get-and-set", "storage", "get-set", ["resp-framing"], ["respond-to-ping"],
                 [cp("SET then GET returns the value", "get-set", flagged=True)]),
        ],
    })
