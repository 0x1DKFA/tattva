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
