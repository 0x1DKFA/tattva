import http.client
import io
import json
import re
import threading
import urllib.parse
from pathlib import Path

import pytest

from tattva import TattvaError
from tattva import serve
from tattva.cli import run
from tattva.progress import read_events
from tattva.project import Entry, Project, save_entry
from tattva.serve import THEMES, cmd_serve, make_server, markdown
from tattva.spec import PROGRESS_FILE, SPEC_FILE

CSP = ("default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; "
       "form-action 'self'; frame-ancestors 'none'")
STATIC = Path(serve.__file__).parent / "web" / "static"


@pytest.fixture
def server():
    srv = make_server(0)
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    yield srv
    srv.shutdown()
    srv.server_close()
    t.join()


@pytest.fixture
def project(tmp_path, sample_spec):
    p = Project.create(tmp_path / "mini", sample_spec)
    p.save_spec()
    return p


@pytest.fixture
def old_project_entry(tmp_path):
    root = tmp_path / "old"
    (root / SPEC_FILE).parent.mkdir(parents=True)
    (root / SPEC_FILE).write_text(json.dumps({"schema_version": 1}))
    e = Entry("old-id", str(root), "Old Go Project")
    save_entry(e)
    return e


def request(server, method, path, headers=None, form=None, host=None):
    port = server.server_address[1]
    conn = http.client.HTTPConnection("127.0.0.1", port)
    h = {"Host": host or f"127.0.0.1:{port}", **(headers or {})}
    body = None
    if form is not None:
        body = urllib.parse.urlencode(form)
        h["Content-Type"] = "application/x-www-form-urlencoded"
    conn.request(method, path, body, h)
    resp = conn.getresponse()
    data = resp.read().decode(errors="replace")
    conn.close()
    return resp, data


def get(server, path):
    return request(server, "GET", path)[1]


def post(server, path, headers=None, form=None):
    return request(server, "POST", path, headers, form if form is not None else {})[0].status


def events(project):
    return read_events(project.abs(PROGRESS_FILE))[0]


def url(project, tail=""):
    return f"/p/{project.spec.project.id}{tail}"


# --- security ---

def test_only_local_host_names(server):
    port = server.server_address[1]
    for host, want in {f"127.0.0.1:{port}": 200, f"localhost:{port}": 200, f"evil.example:{port}": 403,
                       "127.0.0.1:9": 403}.items():
        assert request(server, "GET", "/", host=host)[0].status == want, host


def test_security_headers(server):
    resp, _ = request(server, "GET", "/")
    assert resp.getheader("Content-Security-Policy") == CSP
    assert resp.getheader("X-Content-Type-Options") == "nosniff"
    assert resp.getheader("Referrer-Policy") == "no-referrer"


def test_model_html_is_escaped_and_js_links_dropped():
    html = str(markdown('<script>alert(1)</script> [x](javascript:alert(1))'))
    assert "<script>" not in html and 'href="javascript:' not in html


def test_markdown_renders_tables_and_strikethrough():
    html = str(markdown("| a |\n|---|\n| b |\n\n~~gone~~"))
    assert "<table>" in html and "<s>gone</s>" in html


def test_cross_site_post_rejected(server, project):
    status = post(server, f"/p/{project.spec.project.id}/s/1/start", headers={"Sec-Fetch-Site": "cross-site"})
    assert status == 403
    assert events(project) == []


def test_post_with_foreign_origin_rejected_and_same_origin_accepted(server, project):
    base = url(project, "/s/1/start")
    assert post(server, base, headers={"Origin": "http://evil.example"}) == 403
    port = server.server_address[1]
    assert post(server, base, headers={"Origin": f"http://127.0.0.1:{port}"}) == 303
    assert post(server, base, headers={"Sec-Fetch-Site": "same-origin"}) == 303


def test_escapes_model_content(server, project):
    spec = project.spec
    spec.steps[0].title = "<img src=x onerror=alert(1)>"
    spec.steps[0].detail.task = "Build it. <script>alert(1)</script>"
    spec.references[0].url = "javascript:alert(1)"
    spec.architecture.diagram = 'flowchart LR\n  A["</pre><script>alert(1)</script>"] --> B'
    project.save_spec()
    for path in (url(project, "/s/1"), url(project)):
        body = get(server, path)
        for bad in ("<script>alert", "<img src=x", 'href="javascript:', "</pre><script>"):
            assert bad not in body, (path, bad)


def test_static_files(server):
    for path, typ in {"/static/app.css": "text/css", "/static/app.js": "javascript",
                      "/static/theme-boot.js": "javascript"}.items():
        resp, _ = request(server, "GET", path)
        assert resp.status == 200 and typ in resp.getheader("Content-Type"), path


def test_static_is_not_listed_and_cannot_escape(server):
    for path in ("/static/", "/static", "/static/nope.css", "/static/../serve.py", "/static/%2e%2e/serve.py",
                 "/static/..%2fserve.py", "/static//etc/passwd"):
        assert request(server, "GET", path)[0].status == 404, path


# --- pages ---

def test_home_shows_cards(server, project, old_project_entry):
    body = get(server, "/")
    for want in [f'href="{url(project)}"', "1:mini-redis", "Mini Redis", "Redis · python", "[░░░░░░░░░░░░]",
                 "0/3", "no step in progress", '<section class="pane card missing">', "Old Go Project",
                 "made by an older tattva", '<option value="catppuccin">catppuccin</option>',
                 'data-themes="catppuccin tokyo-night gruvbox"', 'data-theme="catppuccin"',
                 '<script src="/static/theme-boot.js"></script>']:
        assert want in body, want
    assert body.count('href="/p/old-id"') == 1  # only in the window list


def test_home_with_no_projects(server):
    assert "No projects yet" in get(server, "/")


def test_home_shows_registry_warning(server):
    path = Path.home() / ".tattva" / "projects.json"
    path.parent.mkdir(parents=True)
    path.write_text("{not json")
    assert "ignoring unreadable registry" in get(server, "/")


def test_overview(server, project):
    body = get(server, url(project))
    for want in ['<h2 class="pane-title">README.md</h2>', 'class="nav-item selected"', "▸ overview",
                 "<h1>Mini Redis</h1>", "not needed to learn the protocol",
                 "Redis is a single-threaded event-loop server.", '<pre class="mermaid" data-source="flowchart LR',
                 '<a href="https://redis.io/docs/latest/develop/reference/protocol-spec/"',
                 '<script src="/static/mermaid.min.js"></script>', "1:mini-redis*", "0/3 ✓"]:
        assert want in body, want


def test_step_page(server, project):
    body = get(server, url(project, "/s/1"))
    for want in ['<h2 class="pane-title">01-create-run-script.md</h2>', "01 · create-run-script",
                 "phase 1: Basics", "[ start step ]", "[ mark done ]", "[ show hint 1 ]",
                 "opens after the hint above", "<h2>checkpoints</h2>", "port 6379 accepts a connection",
                 "machine checks (run by the verifier)", "<strong>accepts a connection</strong>",
                 "Forgetting to make run.sh executable.", "What happens if two programs listen on the same port?",
                 "02 · respond-to-ping", 'class="nav-item status-available selected"',
                 'class="nav-item status-locked"', 'aria-valuemin="200"']:
        assert want in body, want
    assert "Not expanded" not in body and "unexpanded" not in body
    assert "Servers listen, then accept." not in body  # hint 1 stays closed until opened


def test_step_page_shows_checkpoint_estimates(server, project):
    assert " min" in get(server, f"/p/{project.spec.project.id}/s/2")


def test_flagged_checkpoint_shown(server, project):
    assert "flagged: still too big after 3 splits" in get(server, url(project, "/s/3"))


def test_opened_hints_show(server, project):
    post(server, url(project, "/s/1/hint"), form={"level": "1"})
    body = get(server, url(project, "/s/1"))
    assert "Servers listen, then accept." in body and "[ show hint 2 ]" in body
    assert "[ show hint 1 ]" not in body
    assert '<span class="hint-count">1 hint</span>' in body


def test_not_found(server, project):
    for path in ("/p/nope", url(project, "/s/0"), url(project, "/s/9"), url(project, "/s/x"), "/nope"):
        assert request(server, "GET", path)[0].status == 404, path


def test_old_project_shows_problem(server, old_project_entry):
    assert "made by an older tattva" in get(server, f"/p/{old_project_entry.id}")


def test_broken_spec_shows_the_error(server, project):
    project.abs(SPEC_FILE).write_text('{"schema_version": 2,')
    body = get(server, url(project))
    assert "can&#39;t read its spec" in body


# --- actions ---

def test_start_and_done(server, project):
    base = url(project, "/s/1")
    for action in ("start", "start", "done", "done"):
        resp, _ = request(server, "POST", f"{base}/{action}", form={})
        assert resp.status == 303 and resp.getheader("Location") == base
    assert [e.event for e in events(project)] == ["started", "completed"]  # repeats add nothing


def test_hints_open_in_order(server, project):
    base = url(project, "/s/1")
    for level in ["2", "1", "1", "3", "2", "3", "4", "x"]:
        resp, _ = request(server, "POST", f"{base}/hint", form={"level": level})
        assert resp.status == 303 and resp.getheader("Location") == base + "#hints"
    assert [e.level for e in events(project)] == [1, 2, 3]


def test_rejected_actions_write_nothing(server, project):
    for path in ("/p/nope/s/1/start", url(project, "/s/9/done"), url(project, "/s/1/explode")):
        assert post(server, path) == 404, path
    assert not project.abs(PROGRESS_FILE).exists()


def test_actions_on_old_project_conflict(server, old_project_entry):
    assert post(server, f"/p/{old_project_entry.id}/s/1/start") == 409


def test_viewing_pages_writes_nothing(server, project):
    files = [Path.home() / ".tattva" / "projects.json", project.abs(SPEC_FILE)]
    before = [f.read_bytes() for f in files]
    for path in ("/", url(project), url(project, "/s/1"), "/static/app.css"):
        assert request(server, "GET", path)[0].status == 200, path
    assert [f.read_bytes() for f in files] == before
    assert not project.abs(PROGRESS_FILE).exists()


def test_actions_agree_with_the_cli(server, project):
    post(server, url(project, "/s/1/hint"), form={"level": "1"})
    post(server, url(project, "/s/1/done"))
    out = io.StringIO()
    run(project.root, ["status"], out)
    assert "✓ 01 create-run-script (1 hint)" in out.getvalue()
    assert "edited outside tattva" not in out.getvalue()


# --- command ---

def test_cmd_serve_reports_a_busy_port(monkeypatch):
    busy = make_server(0)
    try:
        with pytest.raises(TattvaError, match="--port"):
            cmd_serve(busy.server_address[1], io.StringIO())
    finally:
        busy.server_close()


def test_cmd_serve_prints_address_opens_browser_and_stops_on_ctrl_c(monkeypatch):
    opened = []
    monkeypatch.setattr("webbrowser.open", opened.append)
    monkeypatch.setattr("tattva.serve.ThreadingHTTPServer.serve_forever",
                        lambda self, *a: (_ for _ in ()).throw(KeyboardInterrupt))
    out = io.StringIO()
    cmd_serve(0, out)
    addr = opened[0]
    assert addr.startswith("http://127.0.0.1:")
    assert out.getvalue() == f"tattva workspace: {addr} (Ctrl-C to stop)\n"


def test_cli_serve_usage():
    with pytest.raises(TattvaError, match=re.escape("usage: tattva serve [--port <n>]")):
        run(Path("."), ["serve", "--port"], io.StringIO())
    with pytest.raises(TattvaError, match="usage: tattva serve"):
        run(Path("."), ["serve", "extra"], io.StringIO())


# --- static assets (ported from web_test.go) ---

def theme_blocks():
    css = (STATIC / "app.css").read_text()
    start, end = css.index("/* === themes === */"), css.index("/* === end themes === */")
    return css[start:end], css[:start] + css[end:]


BLOCK = re.compile(r':root\[data-theme="([a-z0-9-]+)"\]\[data-mode="(dark|light)"\]\s*\{([^}]*)\}')


def test_css_colours_only_in_theme_blocks():
    _, rest = theme_blocks()
    assert not re.search(r"#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch)\(", rest)


def test_every_theme_has_dark_and_light_blocks_with_the_same_variables():
    blocks, _ = theme_blocks()
    sets = {f"{m[1]}/{m[2]}": sorted(re.findall(r"(--[a-z0-9-]+)\s*:", m[3])) for m in BLOCK.finditer(blocks)}
    assert set(sets) == {f"{t}/{m}" for t in THEMES for m in ("dark", "light")}
    assert len({tuple(v) for v in sets.values()}) == 1


def test_css_variables_are_defined():
    css = (STATIC / "app.css").read_text()
    defined = set(re.findall(r"(--[a-z0-9-]+)\s*:", css))
    assert set(re.findall(r"var\((--[a-z0-9-]+)", css)) <= defined


def test_mermaid_is_bundled_and_scripts_use_storage_keys():
    assert (STATIC / "mermaid.min.js").stat().st_size > 1 << 20
    assert "MIT" in (STATIC / "mermaid.LICENSE").read_text()
    for name in ("theme-boot.js", "app.js"):
        assert "tattva-" in (STATIC / name).read_text()


def lum(h):
    def lin(c):
        v = c / 255
        return v / 12.92 if v <= 0.03928 else ((v + 0.055) / 1.055) ** 2.4
    r, g, b = (int(h[i:i + 2], 16) for i in (1, 3, 5))
    return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)


def test_theme_contrast():
    blocks, _ = theme_blocks()
    pairs = [("--text", "--surface", 4.5), ("--accent", "--surface", 4.5), ("--link", "--surface", 4.5),
             ("--text", "--surface-2", 4.5), ("--subtext", "--surface", 3.0), ("--subtext", "--surface-2", 3.0)]
    for m in BLOCK.finditer(blocks):
        v = dict(re.findall(r"(--[a-z0-9-]+):\s*(#[0-9a-fA-F]{6})", m[3]))
        for fg, bg, want in pairs:
            hi, lo = sorted((lum(v[fg]), lum(v[bg])), reverse=True)
            assert (hi + 0.05) / (lo + 0.05) >= want, (m[1], m[2], fg, bg)


# --- robustness ---

def raw_post(server, headers):
    port = server.server_address[1]
    conn = http.client.HTTPConnection("127.0.0.1", port)
    conn.putrequest("POST", "/p/x/s/1/start", skip_host=True)
    for k, v in {"Host": f"127.0.0.1:{port}", **headers}.items():
        conn.putheader(k, v)
    conn.endheaders()
    resp = conn.getresponse()
    status = resp.status
    conn.close()
    return status


def test_post_body_limits_and_checks_come_before_reading(server):
    assert raw_post(server, {"Content-Length": "999999999"}) == 413  # no body sent: must not wait for it
    assert raw_post(server, {"Content-Length": "-5"}) == 400
    assert raw_post(server, {"Content-Length": "abc"}) == 400
    assert raw_post(server, {"Content-Length": "999999999", "Sec-Fetch-Site": "cross-site"}) == 403


def test_handler_has_a_socket_timeout():
    assert serve.Handler.timeout == 10


def test_unexpected_errors_answer_500_with_headers(server, project, monkeypatch):
    def boom(*a):
        raise OSError("disk full")
    monkeypatch.setattr("tattva.serve.append_event", boom)
    resp, body = request(server, "POST", url(project, "/s/1/start"), form={})
    assert resp.status == 500 and "disk full" in body
    assert resp.getheader("Content-Security-Policy") == CSP
    monkeypatch.setattr("tattva.serve._env.get_template", boom)
    resp, body = request(server, "GET", "/")
    assert resp.status == 500 and "disk full" in body
