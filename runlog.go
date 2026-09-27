package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// runLog is one command's log file in ~/.tattva/logs: every Claude call it
// makes, what Claude searched for and read, and how the command ended. Show
// also prints a line in the terminal. A nil runLog does nothing, and one
// whose file couldn't be created still prints. Logging never stops a run.
type runLog struct {
	mu   sync.Mutex // expand logs from several goroutines
	f    *os.File
	out  io.Writer
	Path string
}

// openLog creates the log for one run of command and says where it is.
func openLog(command string, out io.Writer) *runLog {
	l := &runLog{out: out}
	path, err := registryPath()
	if err == nil {
		dir := filepath.Join(filepath.Dir(path), "logs")
		path = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.log", time.Now().Format("2006-01-02T15-04-05"), command, os.Getpid()))
		if err = os.MkdirAll(dir, 0o755); err == nil {
			l.f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintf(out, "  log: unavailable (%v)\n", err)
		return l
	}
	l.Path = path
	fmt.Fprintf(out, "  log: %s\n", path)
	return l
}

// Printf writes one timestamped line to the log.
func (l *runLog) Printf(format string, a ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.write(fmt.Sprintf(format, a...))
}

// Show prints a line in the terminal and writes it to the log.
func (l *runLog) Show(format string, a ...any) {
	if l == nil {
		return
	}
	msg := fmt.Sprintf(format, a...)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.out, msg)
	l.write(strings.TrimLeft(msg, " "))
}

// write adds a line to the file. The caller holds l.mu.
func (l *runLog) write(msg string) {
	if l.f != nil {
		fmt.Fprintf(l.f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)
	}
}

// Close records how the command ended and closes the file.
func (l *runLog) Close(err error) {
	if l == nil {
		return
	}
	if err != nil {
		l.Printf("failed: %v", err)
	} else {
		l.Printf("done")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

// clip puts s on one line and cuts it to at most n characters.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}
