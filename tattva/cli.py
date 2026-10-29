import argparse
import sys
from pathlib import Path
from typing import TextIO

from tattva import TattvaError

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
    if argv[0] in ("-h", "--help"):
        argv = ["help"]
    if argv[0] not in commands.choices:
        raise TattvaError(f'unknown command "{argv[0]}" (run `tattva help`)')
    args = parser.parse_args(argv)
    if args.command == "help":
        out.write(USAGE)


def main() -> None:
    try:
        run(Path.cwd(), sys.argv[1:], sys.stdout)
    except TattvaError as e:
        print(f"tattva: {e}", file=sys.stderr)
        sys.exit(1)
