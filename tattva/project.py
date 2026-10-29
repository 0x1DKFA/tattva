"""The project registry (~/.tattva/projects.json) and a project's generated files."""
import fcntl
import hashlib
import json
import os
import tempfile
from contextlib import contextmanager
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Callable, TextIO

from . import TattvaError
from .render import render_readme, render_step
from .spec import (PROGRESS_FILE, README_FILE, SPEC_FILE, STEPS_DIR, OldSpec, Spec, decode_spec,
                   encode_spec, step_file, validate)


@dataclass
class Entry:
    id: str
    path: str
    name: str
    files: dict[str, str] = field(default_factory=dict)  # path relative to the root -> sha256


def registry_path() -> Path:
    return Path.home() / ".tattva" / "projects.json"


def load_registry() -> tuple[list[Entry], str]:
    """A missing registry is empty; an unreadable one is too, with a warning (it rebuilds itself)."""
    path = registry_path()
    try:
        data = json.loads(path.read_bytes())
        return [Entry(**e) for e in data["projects"]], ""
    except FileNotFoundError:
        return [], ""
    except (OSError, ValueError, KeyError, TypeError) as err:
        return [], f"ignoring unreadable registry {path} ({err}); it rebuilds as you use tattva in each project"


@contextmanager
def lock_registry():
    """Exclusive lock on projects.lock so concurrent runs update the registry one at a time."""
    lock = registry_path().with_suffix(".lock")
    lock.parent.mkdir(parents=True, exist_ok=True)
    with open(lock, "a") as f:
        fcntl.flock(f, fcntl.LOCK_EX)
        yield  # closing the file releases the lock


def save_entry(e: Entry) -> None:
    """Re-reads the registry under the lock, so other projects keep their entries; drops any other entry with e's id or path."""
    with lock_registry():
        kept = [o for o in load_registry()[0] if o.id != e.id and o.path != e.path]
        data = {"projects": [asdict(o) for o in kept + [e]]}
        write_file_atomic(registry_path(), (json.dumps(data, indent=2) + "\n").encode())


def write_file_atomic(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".tattva-", delete=False) as f:
        try:
            f.write(data)
            f.flush()
            os.chmod(f.name, 0o644)
            os.replace(f.name, path)
        except BaseException:
            os.unlink(f.name)
            raise


def hash_of(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def find_root(cwd: Path) -> Path:
    """The nearest directory at or above cwd that contains curriculum/spec.json."""
    cwd = Path(cwd).resolve()
    for d in (cwd, *cwd.parents):
        if (d / SPEC_FILE).exists():
            return d
    raise TattvaError("not in a tattva project: no curriculum/spec.json here or in any parent directory (start one with `tattva new`)")


# ponytail: only registry writes are locked; two commands on one project at once can still overwrite each other's spec.json (last write wins).
class Project:
    def __init__(self, root: Path, spec: Spec, entry: Entry):
        self.root, self.spec, self.entry = root, spec, entry
        self.notices: list[str] = []  # shown by every command
        self.edited: list[str] = []  # generated files changed outside tattva; shown by status

    @classmethod
    def create(cls, root: Path, spec: Spec) -> "Project":
        root = Path(root).resolve()
        p = cls(root, spec, Entry(spec.project.id, str(root), spec.project.name))
        save_entry(p.entry)
        return p

    def abs(self, rel: str) -> Path:
        return self.root / rel

    def changed(self, rel: str, data: bytes) -> bool:
        """Data differs from the hash tattva stored; with no stored hash it isn't a change."""
        h = self.entry.files.get(rel)
        return h is not None and h != hash_of(data)

    def untouched(self, rel: str, cur: bytes) -> bool:
        """cur is exactly what tattva last wrote to rel."""
        return self.entry.files.get(rel) == hash_of(cur)

    def generated(self) -> dict[str, bytes]:
        files = {README_FILE: render_readme(self.spec)}
        for i, st in enumerate(self.spec.steps):
            files[step_file(i, st.id)] = render_step(self.spec, i)
        return files

    def adopt(self) -> None:
        """Hash generated files tattva has no hash for (new machine, lost registry) when they match what it would write now."""
        for rel, want in self.generated().items():
            if rel not in self.entry.files and self.abs(rel).is_file() and self.abs(rel).read_bytes() == want:
                self.entry.files[rel] = hash_of(want)

    def write(self, rel: str, data: bytes) -> None:
        write_file_atomic(self.abs(rel), data)
        self.entry.files[rel] = hash_of(data)
        save_entry(self.entry)

    def save_spec(self) -> None:
        self.write(SPEC_FILE, encode_spec(self.spec))

    def render(self) -> None:
        """Write missing files, refresh files tattva wrote, leave edited ones (listed in self.edited),
        and delete untouched step files no longer in the curriculum."""
        want = self.generated()
        self.edited = []
        for rel in sorted(want):
            path = self.abs(rel)
            if path.exists():
                cur = path.read_bytes()
                if cur == want[rel]:
                    self.entry.files[rel] = hash_of(cur)
                    continue
                if not self.untouched(rel, cur):
                    self.edited.append(rel)
                    continue
            self.write(rel, want[rel])
        for path in sorted(self.abs(STEPS_DIR).glob("*.md")):
            rel = f"{STEPS_DIR}/{path.name}"
            if rel in want:
                continue
            if not self.untouched(rel, path.read_bytes()):
                self.edited.append(rel + " (no longer part of the curriculum)")
                continue
            path.unlink()
            del self.entry.files[rel]
        save_entry(self.entry)

    def print_notices(self, out: TextIO) -> None:
        for n in self.notices:
            print("note:", n, file=out)


def open_project(cwd: Path) -> Project:
    """Find the project at or above cwd, validate its spec, refresh its registry entry, note files edited outside tattva."""
    root = find_root(cwd)
    data = (root / SPEC_FILE).read_bytes()
    try:
        spec = decode_spec(data)
    except OldSpec as err:
        raise TattvaError(str(err)) from err
    except ValueError as err:
        raise TattvaError(f"{SPEC_FILE}: {err}") from err
    entries, warn = load_registry()
    entry = next((e for e in entries if e.id == spec.project.id), Entry(spec.project.id, "", ""))
    entry.path, entry.name = str(root), spec.project.name
    p = Project(root, spec, entry)
    if warn:
        p.notices.append(warn)

    edited = p.changed(SPEC_FILE, data)
    errs, _ = validate(spec)
    if errs:
        how = " (it was edited outside tattva)" if edited else ""
        raise TattvaError(f"{SPEC_FILE} breaks the curriculum rules{how}:\n  " + "\n  ".join(errs))
    if edited:
        p.notices.append(f"{SPEC_FILE} was edited outside tattva; the edit is valid, so it's accepted")
    entry.files[SPEC_FILE] = hash_of(data)
    if p.abs(PROGRESS_FILE).is_file():
        prog = p.abs(PROGRESS_FILE).read_bytes()
        if p.changed(PROGRESS_FILE, prog):
            p.notices.append(f"{PROGRESS_FILE} was edited outside tattva; accepted")
        entry.files[PROGRESS_FILE] = hash_of(prog)
    p.adopt()
    save_entry(entry)
    return p


def with_project(cwd: Path, out: TextIO, fn: Callable[[Project], None]) -> None:
    p = open_project(cwd)
    try:
        p.render()
        fn(p)
    finally:
        p.print_notices(out)
