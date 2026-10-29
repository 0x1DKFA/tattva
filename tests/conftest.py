import pytest


@pytest.fixture(autouse=True)
def home(tmp_path, monkeypatch):
    monkeypatch.setenv("HOME", str(tmp_path / "home"))


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
