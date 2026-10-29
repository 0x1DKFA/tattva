"""`tattva serve`: a local web workspace to read the curriculum and track progress."""

import mimetypes
import re
import sys
import webbrowser
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import TextIO
from urllib.parse import parse_qs, unquote, urlsplit

from jinja2 import Environment, PackageLoader
from markdown_it import MarkdownIt
from markupsafe import Markup

from . import TattvaError
from .progress import MARKS, Event, Progress, Status, append_event, hint_note, load_projects, progress_of, project_progress
from .project import Project, load_registry, open_project
from .render import StepView, checkpoint_line, contract, num, step_view
from .spec import Reference, Spec, step_file

# Colour themes in dropdown order; the first is the default. Each has a dark and a light block in app.css.
THEMES = ["catppuccin", "tokyo-night", "gruvbox"]
STATIC = Path(__file__).parent / "web" / "static"
MAX_FORM = 64 * 1024  # the forms here post one short field
CSP = ("default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; "
       "form-action 'self'; frame-ancestors 'none'")

# The text comes from a model, so raw HTML is escaped and unsafe link targets (javascript:) are left as text.
_md = MarkdownIt("commonmark", {"html": False}).enable(["table", "strikethrough"])


def markdown(s: str) -> Markup:
    return Markup(_md.render(s))


def safe_url(u: str) -> bool:
    """Only http and https references become links."""
    return re.match(r"https?://", u.strip(), re.I) is not None


def progress_bar(done: int, total: int) -> str:
    """A 12-cell text bar such as "███████░░░░░"."""
    full = done * 12 // total if total else 0
    return "█" * full + "░" * (12 - full)


_env = Environment(loader=PackageLoader("tattva", "web/templates"), autoescape=True)
_env.filters["markdown"] = markdown
_env.globals.update(themes=THEMES, hint_note=hint_note, progress_bar=progress_bar, safe_url=safe_url)

STATUS_NAMES = {Status.DONE: "done", Status.IN_PROGRESS: "in-progress", Status.AVAILABLE: "available",
                Status.LOCKED: "locked"}


@dataclass
class StepNav:
    n: int
    num: str
    title: str
    status: str
    mark: str
    hints: str  # "2 hints", or ""
    selected: bool


@dataclass
class PhaseNav:
    title: str
    steps: list[StepNav] = field(default_factory=list)


@dataclass
class HintItem:
    level: int
    label: str
    text: str
    revealed: bool
    next: bool  # the one hint that can be opened now


@dataclass
class StepPage:
    view: StepView
    file: str  # "08-key-expiry.md", the pane title
    base: str  # "/p/<id>/s/8", where the buttons post
    status: str
    missing: str  # prerequisites that aren't done, by number
    checkpoints: list[str]
    reveal: list[HintItem]
    references: list[Reference]


def phase_navs(s: Spec, pr: Progress, selected: int) -> list[PhaseNav]:
    navs = []
    for ph in s.phases:
        nav = PhaseNav(ph.title)
        for i, st in enumerate(s.steps):
            if st.phase == ph.id:
                nav.steps.append(StepNav(i + 1, num(i), st.title, STATUS_NAMES[pr.status[st.id]],
                                         MARKS[pr.status[st.id]], hint_note(pr.hints.get(st.id, 0)), i == selected))
        navs.append(nav)
    return navs


def step_page(id: str, s: Spec, pr: Progress, i: int) -> StepPage:
    st = s.steps[i]
    v = step_view(s, i)
    opened = pr.hints.get(st.id, 0)
    missing = [num(s.step_index(pre)) for pre in st.prerequisites if pr.status[pre] != Status.DONE]
    return StepPage(
        v, Path(step_file(i, st.id)).name, f"/p/{id}/s/{i + 1}", STATUS_NAMES[pr.status[st.id]], ", ".join(missing),
        [checkpoint_line(c) for c in st.checkpoints],
        [HintItem(n, label, text, n <= opened, n == opened + 1) for n, (label, text) in enumerate(v.hints, 1)],
        s.references)


def make_server(port: int) -> ThreadingHTTPServer:
    """A workspace server on 127.0.0.1; port 0 picks a free one. The Host check uses the port it got."""
    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    return srv


class Handler(BaseHTTPRequestHandler):
    timeout = 10  # a stalled connection can't hold a thread forever

    def log_message(self, *args):  # no per-request stderr noise
        pass

    def end_headers(self):
        """Headers that stop model-written content from running scripts, and the pages from being framed."""
        self.send_header("Content-Security-Policy", CSP)
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Referrer-Policy", "no-referrer")
        super().end_headers()

    def reply(self, status: int, body: str | bytes, ctype: str = "text/html; charset=utf-8") -> None:
        data = body.encode() if isinstance(body, str) else body
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def fail(self, status: int, message: str) -> None:
        self.reply(status, message + "\n", "text/plain; charset=utf-8")

    def local(self) -> bool:
        """Only requests addressed to 127.0.0.1 or localhost on our port, so a web page can't reach us by DNS rebinding."""
        port = self.server.server_address[1]
        return self.headers.get("Host") in (f"127.0.0.1:{port}", f"localhost:{port}")

    def same_origin(self) -> bool:
        """Cross-origin protection for POSTs: Sec-Fetch-Site when sent, else Origin against Host."""
        site = self.headers.get("Sec-Fetch-Site")
        if site is not None:
            return site in ("same-origin", "none")
        origin = self.headers.get("Origin")
        return origin is None or urlsplit(origin).netloc == self.headers.get("Host")

    def do_GET(self):
        self.guarded(self.get)

    def do_POST(self):
        self.guarded(self.post)

    def guarded(self, handle) -> None:
        try:
            handle()
        except Exception as e:  # answer 500 with the error rather than dropping the connection
            self.fail(500, str(e) or type(e).__name__)

    def get(self):
        if not self.local():
            return self.fail(403, "the tattva workspace only answers on 127.0.0.1 and localhost")
        path = unquote(urlsplit(self.path).path)
        if m := re.fullmatch(r"/static/([^/]+)", path):
            return self.static(m[1])
        if path == "/":
            projects, warn = load_projects()
            return self.page("home.html", projects=projects, warn=warn, current=None)
        if m := re.fullmatch(r"/p/([^/]+)(?:/s/([^/]+))?", path):
            return self.project(m[1], m[2])
        self.fail(404, "not found")

    def post(self):
        if not self.local():
            return self.fail(403, "the tattva workspace only answers on 127.0.0.1 and localhost")
        if not self.same_origin():
            return self.fail(403, "cross-origin request rejected")
        size = self.headers.get("Content-Length") or "0"
        if not (size.isascii() and size.isdigit()):
            return self.fail(400, "bad Content-Length")
        if int(size) > MAX_FORM:
            return self.fail(413, "request body too large")
        form = parse_qs(self.rfile.read(int(size)).decode(errors="replace"))
        m = re.fullmatch(r"/p/([^/]+)/s/([^/]+)/([^/]+)", unquote(urlsplit(self.path).path))
        if not m:
            return self.fail(404, "not found")
        self.action(m[1], m[2], m[3], form)

    def static(self, name: str) -> None:
        f = STATIC / name  # name has no slash, and ".." isn't a file
        if name.startswith(".") or not f.is_file():
            return self.fail(404, "not found")
        self.reply(200, f.read_bytes(), mimetypes.guess_type(name)[0] or "application/octet-stream")

    def page(self, template: str, **v) -> None:
        self.reply(200, _env.get_template(template).render(**v))

    def project(self, id: str, n: str | None) -> None:
        """A project's overview, or its step n."""
        projects, _ = load_projects()
        cur = next((p for p in projects if p.id == id), None)
        if cur is None:
            return self.fail(404, "not found")
        v = dict(projects=projects, current=cur, warn="", problem=cur.problem, overview=None, step=None, phases=[])
        if not cur.problem:
            s, selected = cur.spec, -1
            pr = progress_of(s, cur.events)
            if n is None:
                v["overview"] = {"spec": s, "contract": contract(s.program)}
            elif n.isascii() and n.isdigit() and 1 <= int(n) <= len(s.steps):
                selected = int(n) - 1
                v["step"] = step_page(cur.id, s, pr, selected)
            else:
                return self.fail(404, "not found")
            v["phases"] = phase_navs(s, pr, selected)
        self.page("project.html", **v)

    def action(self, id: str, n: str, action: str, form: dict[str, list[str]]) -> None:
        """Runs a step's start, done or hint button, then sends the browser back to the step so a reload doesn't post again."""
        entry = next((e for e in load_registry()[0] if e.id == id), None)
        if entry is None:
            return self.fail(404, "not found")
        try:
            p = open_project(Path(entry.path))
        except TattvaError as e:
            return self.fail(409, str(e))
        try:
            self.run_action(p, id, n, action, form)
        finally:
            p.print_notices(sys.stderr)

    def run_action(self, p: Project, id: str, n: str, action: str, form: dict[str, list[str]]) -> None:
        spec = p.spec
        if not (n.isascii() and n.isdigit() and 1 <= int(n) <= len(spec.steps)) or spec.project.id != id \
                or action not in ("start", "done", "hint"):
            return self.fail(404, "not found")
        st = spec.steps[int(n) - 1]
        pr = project_progress(p)
        back = f"/p/{id}/s/{n}"
        if action == "start" and pr.status[st.id] == Status.AVAILABLE:
            append_event(p, Event(None, st.id, "started"))
        elif action == "done" and pr.status[st.id] != Status.DONE:
            append_event(p, Event(None, st.id, "completed"))
        elif action == "hint":
            back += "#hints"
            level = (form.get("level") or [""])[0]
            if level.isascii() and level.isdigit() and int(level) == pr.hints.get(st.id, 0) + 1 \
                    and int(level) <= len(st.detail.hints):
                append_event(p, Event(None, st.id, "hint", int(level)))
        self.send_response(303)
        self.send_header("Location", back)
        self.send_header("Content-Length", "0")
        self.end_headers()


def cmd_serve(port: int, out: TextIO) -> None:
    """Runs the workspace on 127.0.0.1:port until Ctrl-C."""
    try:
        srv = make_server(port)
    except OSError as e:
        raise TattvaError(f"can't listen on 127.0.0.1:{port}: {e.strerror or e} "
                          "(pick another port with `tattva serve --port <n>`)") from e
    addr = f"http://127.0.0.1:{srv.server_address[1]}/"
    print(f"tattva workspace: {addr} (Ctrl-C to stop)", file=out, flush=True)
    webbrowser.open(addr)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        srv.server_close()
