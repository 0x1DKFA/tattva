import json
import threading

import pytest

from tattva import TattvaError
from tattva.project import (Entry, Project, find_root, hash_of, load_registry, open_project,
                            registry_path, save_entry, with_project, write_file_atomic)
from tattva.spec import PROGRESS_FILE, README_FILE, SPEC_FILE, encode_spec, step_file

STEP0 = step_file(0, "create-run-script")


@pytest.fixture
def proj(tmp_path, sample_spec):
    root = tmp_path / "proj"
    root.mkdir()
    p = Project.create(root, sample_spec)
    p.save_spec()
    p.render()
    return p


def test_write_file_atomic(tmp_path):
    f = tmp_path / "a" / "b.txt"
    write_file_atomic(f, b"one")
    write_file_atomic(f, b"two")
    assert f.read_bytes() == b"two"
    assert f.stat().st_mode & 0o777 == 0o644
    assert [p.name for p in f.parent.iterdir()] == ["b.txt"]


def test_create_writes_files_and_registers(proj):
    for rel in (SPEC_FILE, README_FILE, STEP0):
        assert proj.abs(rel).exists()
    entries, _ = load_registry()
    assert len(entries) == 1 and entries[0].path == str(proj.root) and len(entries[0].files) == 5


def test_registry_format_matches_go(tmp_path, sample_spec):
    Project.create(tmp_path, sample_spec)
    data = json.loads(registry_path().read_text())
    assert set(data["projects"][0]) == {"id", "path", "name", "files"}


def test_save_entry_replaces_same_id_or_path_keeps_others():
    save_entry(Entry("a", "/a", "A", {}))
    save_entry(Entry("b", "/b", "B", {}))
    save_entry(Entry("a", "/a2", "A2", {}))
    save_entry(Entry("c", "/b", "C", {}))
    assert sorted((e.id, e.path) for e in load_registry()[0]) == [("a", "/a2"), ("c", "/b")]


def test_concurrent_saves_keep_every_entry():
    def work(i):
        for _ in range(5):
            save_entry(Entry(str(i), f"/p/{i}", "", {}))
    ts = [threading.Thread(target=work, args=(i,)) for i in range(20)]
    [t.start() for t in ts]
    [t.join() for t in ts]
    assert len(load_registry()[0]) == 20


def test_missing_registry_is_empty():
    assert load_registry() == ([], "")


def test_find_root_walks_up(proj):
    sub = proj.root / "internal" / "server"
    sub.mkdir(parents=True)
    assert open_project(sub).root == proj.root.resolve()


def test_outside_project(tmp_path):
    with pytest.raises(TattvaError, match="not in a tattva project.*tattva new"):
        find_root(tmp_path)


def test_old_project_is_reported_not_crashed(tmp_path):
    (tmp_path / "curriculum").mkdir()
    (tmp_path / "curriculum/spec.json").write_text('{"schema_version": 1}')
    with pytest.raises(TattvaError, match="made by an older tattva"):
        open_project(tmp_path)


def test_malformed_json_rejected(proj):
    proj.abs(SPEC_FILE).write_text('{"schema_version": 2,')
    with pytest.raises(TattvaError, match="curriculum/spec.json"):
        open_project(proj.root)


def test_broken_spec_edit_rejected(proj):
    proj.spec.steps[1].prerequisites = ["ghost"]
    proj.abs(SPEC_FILE).write_bytes(encode_spec(proj.spec))
    with pytest.raises(TattvaError, match=r"edited outside tattva(.|\n)*prerequisite .ghost"):
        open_project(proj.root)


def test_valid_spec_edit_accepted_once(proj):
    proj.spec.steps[0].title = "Create run.sh by hand"
    proj.abs(SPEC_FILE).write_bytes(encode_spec(proj.spec))
    assert any("edited outside tattva" in n for n in open_project(proj.root).notices)
    assert not any("edited outside tattva" in n for n in open_project(proj.root).notices)


def test_render_leaves_edited_files_alone(proj):
    step = proj.abs(STEP0)
    step.write_text("my notes")
    proj.spec.steps[0].title = "Renamed"
    proj.render()
    assert step.read_text() == "my notes"
    assert STEP0 in proj.edited
    assert "Renamed" in proj.abs(README_FILE).read_text()


def test_render_regenerates_deleted_file(proj):
    proj.abs(STEP0).unlink()
    proj.render()
    assert proj.abs(STEP0).exists()


def test_render_deletes_stale_untouched_step_file(proj):
    stale = proj.abs("curriculum/steps/99-old.md")
    write_file_atomic(stale, b"old")
    proj.entry.files["curriculum/steps/99-old.md"] = hash_of(b"old")
    proj.render()
    assert not stale.exists()
    assert "curriculum/steps/99-old.md" not in proj.entry.files


def test_render_keeps_stale_edited_step_file(proj):
    stale = proj.abs("curriculum/steps/99-old.md")
    write_file_atomic(stale, b"old")
    proj.entry.files["curriculum/steps/99-old.md"] = hash_of(b"old")
    stale.write_text("mine")
    proj.render()
    assert stale.exists() and any("99-old.md" in e for e in proj.edited)


def test_no_baseline_adopts_matching_files(proj):
    registry_path().unlink()
    q = open_project(proj.root)
    q.spec.steps[0].title = "Renamed"
    q.render()
    assert "Renamed" in q.abs(STEP0).read_text()


def test_registry_follows_moved_project(proj, tmp_path):
    moved = tmp_path / "moved"
    proj.root.rename(moved)
    open_project(moved)
    entries, _ = load_registry()
    assert [e.path for e in entries] == [str(moved.resolve())]


def test_corrupt_registry_is_ignored(proj):
    registry_path().write_text("{not json")
    q = open_project(proj.root)
    assert any("ignoring unreadable registry" in n for n in q.notices)
    entries, warn = load_registry()
    assert warn == "" and len(entries) == 1


def test_progress_edit_noted(proj):
    proj.abs(PROGRESS_FILE).write_text("")
    open_project(proj.root)
    proj.abs(PROGRESS_FILE).write_text("x\n")
    assert any("progress.jsonl was edited" in n for n in open_project(proj.root).notices)


def test_with_project_prints_notices_after_fn(proj, capsys):
    import io
    registry_path().write_text("{bad")
    out = io.StringIO()
    seen = []
    with_project(proj.root, out, lambda p: seen.append(p.root))
    assert seen and out.getvalue().startswith("note: ignoring unreadable registry")
