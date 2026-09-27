# tattva

Turn a system you've used, such as Redis, Git or a SQL database, into a build-your-own-x curriculum that you implement yourself. Claude designs the path, with small steps, hints and checks. You write all the code.

## Install

You need Go 1.25+ and [Claude Code](https://claude.com/claude-code), installed and logged in.

```
go install github.com/0x1DKFA/tattva@latest
```

## Use

```
mkdir mini-redis && cd mini-redis
tattva new "Redis" --lang go                          # design the curriculum (a few minutes)
tattva revise "skip persistence, go deeper on RESP"   # optional: reshape the outline
tattva expand                                         # write every step's details
tattva next                                           # start the next step
tattva done                                           # finish it
tattva status                                         # see where you are
tattva list                                           # all your projects
tattva serve                                          # read and track in your browser
```

`new`, `revise` and `expand` take a few minutes. While they run you see what Claude searches for and reads. Each run also writes a full log to `~/.tattva/logs/`, and its first line of output names the file. The log holds every Claude call, what Claude said and looked up, any repairs, the cost, and how the run ended.

Everything lives in `curriculum/` inside your project: `spec.json` (the curriculum), `progress.jsonl` (your progress), `README.md` and `steps/*.md`. Commit it with your code and git shows your progress next to the code that made it.

`tattva serve` opens a workspace at http://127.0.0.1:4747: your projects as cards, and each project's steps next to the step you're reading. Hints open one at a time and are recorded, so you can see which steps you finished on your own. Pick a theme and dark or light mode in the top-left corner.
