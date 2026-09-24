// Command tattva turns a system you've used into a curriculum you build yourself.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
)

const usage = `tattva turns a system you've used into a curriculum you build yourself.

Usage:
  tattva new "<target>" --lang <lang> [--model <m>]   design a curriculum here
  tattva revise "<feedback>" [--force] [--model <m>]  rewrite the outline
  tattva expand [--model <m>]                         write the step details
  tattva next                                         start the next step
  tattva done [step]                                  complete a step
  tattva status                                       show progress
  tattva list                                         list your projects
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	dir, err := os.Getwd()
	if err == nil {
		err = run(ctx, dir, os.Args[1:], os.Stdout)
	}
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tattva:", err)
		os.Exit(1)
	}
}

// run executes one command with dir as the working directory. It is main
// without the process exit, so tests can call it.
func run(ctx context.Context, dir string, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return errors.New("no command given")
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	case "next", "status":
		pos, err := parseArgs(newFlags(cmd), args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return fmt.Errorf("usage: tattva %s", cmd)
		}
		return withProject(dir, out, func(p *Project) error {
			if cmd == "next" {
				return cmdNext(p, out)
			}
			return cmdStatus(p, out)
		})
	case "done":
		pos, err := parseArgs(newFlags(cmd), args)
		if err != nil {
			return err
		}
		if len(pos) > 1 {
			return errors.New("usage: tattva done [step]")
		}
		step := ""
		if len(pos) == 1 {
			step = pos[0]
		}
		return withProject(dir, out, func(p *Project) error { return cmdDone(p, step, out) })
	case "new":
		fs := newFlags(cmd)
		lang := fs.String("lang", "", "language you'll build in")
		model := fs.String("model", "", "Claude model")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 1 || *lang == "" {
			return errors.New(`usage: tattva new "<target>" --lang <lang> [--model <m>]`)
		}
		return cmdNew(ctx, dir, pos[0], *lang, *model, out)
	case "revise":
		fs := newFlags(cmd)
		force := fs.Bool("force", false, "drop step details and step files")
		model := fs.String("model", "", "Claude model")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return errors.New(`usage: tattva revise "<feedback>" [--force] [--model <m>]`)
		}
		return withProject(dir, out, func(p *Project) error { return cmdRevise(ctx, p, pos[0], *force, *model, out) })
	case "expand":
		fs := newFlags(cmd)
		model := fs.String("model", "", "Claude model")
		pos, err := parseArgs(fs, args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: tattva expand [--model <m>]")
		}
		return withProject(dir, out, func(p *Project) error { return cmdExpand(ctx, p, *model, out) })
	case "list":
		pos, err := parseArgs(newFlags(cmd), args)
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return errors.New("usage: tattva list")
		}
		return cmdList(out)
	default:
		return fmt.Errorf("unknown command %q (run `tattva help`)", cmd)
	}
}

// newFlags returns a flag set for one command that reports errors instead of
// printing them and exiting.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseArgs parses fs's flags wherever they appear among args and returns the
// positional arguments. The flag package alone stops at the first positional
// argument, which would ignore the --lang in `tattva new "Redis" --lang go`.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}

// withProject opens the project around dir, brings its generated files in
// line with the spec, runs fn, and prints any notices. Commands that change
// the spec render again themselves.
func withProject(dir string, out io.Writer, fn func(*Project) error) error {
	p, err := openProject(dir)
	if err != nil {
		return err
	}
	defer p.printNotices(out)
	if err := p.render(); err != nil {
		return err
	}
	return fn(p)
}
