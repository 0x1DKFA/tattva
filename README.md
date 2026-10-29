# tattva

Turn a system you've used, such as Redis, Git or a SQL database, into a build-your-own-x curriculum that you implement yourself. Claude or Codex designs the path, with small steps, hints and checks. You write all the code.

## Install

You need Python 3.12 or newer, and either [Claude Code](https://claude.com/claude-code) (the `claude` CLI) or [Codex CLI](https://developers.openai.com/codex/cli/) (`codex`, used with `--provider codex`), installed and logged in.

```
git clone git@github.com:0x1DKFA/tattva.git && cd tattva
python3 -m venv .venv && .venv/bin/pip install -e .
mkdir -p ~/.local/bin && ln -s "$PWD/.venv/bin/tattva" ~/.local/bin/tattva   # any directory on your PATH
```

## Use

```
tattva new "<target>" --lang <lang> [--provider <p>] [--model <m>]   write a curriculum here (re-run to resume)
tattva next                                         start the next step
tattva done [step]                                  complete a step
tattva status                                       show progress
tattva list                                         list your projects
tattva serve [--port <n>]                           read and track in your browser
```

For example:

```
mkdir mini-redis && cd mini-redis
tattva new "Redis" --lang go --provider codex     # Claude is the default provider
```

`tattva new` runs unattended and takes a few minutes. It prints the graph first, then shows a live progress panel. The steps are:

1. Research the target.
2. Design the curriculum.
3. Break it into checkpoints.
4. A judge checks that every checkpoint fits about 20 minutes for an intermediate programmer with no AI help. Anything that doesn't is split, up to 3 times, and a fresh agent checks each split dropped nothing.
5. Group checkpoints into steps of 1 to 3 checkpoints.
6. Write each step's details.

Each run writes a log to `~/.tattva/logs/`. If the run stops (a crash, Ctrl-C, your laptop going to sleep), run the same `tattva new` again in the same folder and it resumes.

Everything lives in `curriculum/` inside your project: `spec.json` (the curriculum), `progress.jsonl` (your progress), `README.md` and `steps/*.md`. Commit it with your code and git shows your progress next to the code that made it.

Note: curricula made by the old Go version of tattva don't open. Generate them again with `tattva new`.

`tattva serve` opens a workspace at http://127.0.0.1:4747: your projects as cards, and each project's steps next to the step you're reading. Hints open one at a time and are recorded, so you can see which steps you finished on your own.
