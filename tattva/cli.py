import argparse
import sys
from pathlib import Path
from typing import TextIO

from tattva import TattvaError
from tattva.progress import cmd_done, cmd_list, cmd_next, cmd_status
from tattva.project import with_project

USAGE = """tattva turns a system you've used into a curriculum you build yourself.

Usage:
  tattva new "<target>" --lang <lang> [--provider <p>] [--model <m>]   write a curriculum here (re-run to resume)
  tattva next                                         start the next step
  tattva done [step]                                  complete a step
  tattva status                                       show progress
  tattva list                                         list your projects
  tattva serve [--port <n>]                           read and track in your browser
"""


class _Parser(argparse.ArgumentParser):
    def error(self, message):
        raise TattvaError(message)


def run(cwd: Path, argv: list[str], out: TextIO) -> None:
    if not argv:
        out.write(USAGE)
        raise TattvaError("no command given")
    parser = _Parser(prog="tattva", add_help=False)
    commands = parser.add_subparsers(dest="command")
    commands.add_parser("help", add_help=False)  # later tasks add their commands here
    for name in ("next", "status", "list"):
        commands.add_parser(name, add_help=False)
    commands.add_parser("done", add_help=False).add_argument("step", nargs="*")
    if argv[0] in ("-h", "--help"):
        argv = ["help"]
    if argv[0] not in commands.choices:
        raise TattvaError(f'unknown command "{argv[0]}" (run `tattva help`)')
    args, extra = parser.parse_known_args(argv)
    if args.command == "help":
        out.write(USAGE)
    elif args.command == "done":
        if extra or len(args.step) > 1:
            raise TattvaError("usage: tattva done [step]")
        with_project(cwd, out, lambda p: cmd_done(p, args.step[0] if args.step else "", out))
    elif args.command in ("next", "status", "list"):
        if extra:
            raise TattvaError(f"usage: tattva {args.command}")
        if args.command == "list":
            cmd_list(out)
        else:
            fn = cmd_next if args.command == "next" else cmd_status
            with_project(cwd, out, lambda p: fn(p, out))


def main() -> None:
    try:
        run(Path.cwd(), sys.argv[1:], sys.stdout)
    except TattvaError as e:
        print(f"tattva: {e}", file=sys.stderr)
        sys.exit(1)
