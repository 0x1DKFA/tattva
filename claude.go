package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// call is one headless Claude Code invocation.
type call struct {
	System string  // system prompt, replacing Claude Code's default
	Prompt string  // sent on stdin
	Schema []byte  // JSON Schema the output must match
	Web    bool    // allow WebSearch and WebFetch
	Model  string  // empty means Claude Code's default
	Label  string  // names the call in the log, like "design" or "expand storage"
	Log    *runLog // nil logs nothing
}

// callTimeout bounds one call. It's a var so tests can shorten it.
var callTimeout = 20 * time.Minute

// streamEvent is one line of `claude -p --output-format stream-json`, with
// only the fields tattva reads.
type streamEvent struct {
	Type    string   `json:"type"`
	Subtype string   `json:"subtype"`
	Model   string   `json:"model"`
	Tools   []string `json:"tools"`
	Message struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Name    string          `json:"name"`
			Input   json.RawMessage `json:"input"`
			IsError bool            `json:"is_error"`
			Content json.RawMessage `json:"content"`
		} `json:"content"`
	} `json:"message"`

	// The last event, of type "result".
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	NumTurns         int             `json:"num_turns"`
}

// runClaude runs `claude -p` for c in an empty temp dir and returns the
// structured output and the call's cost in USD. It logs Claude's progress to
// c.Log as it streams in, and shows the searches and pages in the terminal.
func runClaude(ctx context.Context, c call) (out json.RawMessage, cost float64, err error) {
	began := time.Now()
	var res *streamEvent
	defer func() {
		took := time.Since(began).Round(time.Second)
		if err != nil {
			c.Log.Printf("[%s] failed after %s, $%.2f: %v", c.Label, took, cost, err)
			return
		}
		c.Log.Printf("[%s] finished in %s: %d turns, $%.2f", c.Label, took, res.NumTurns, cost)
	}()
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "tattva-call-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)

	tools := ""
	if c.Web {
		tools = "WebSearch,WebFetch"
	}
	args := []string{"-p",
		"--output-format", "stream-json",
		"--verbose", // stream-json requires it with -p
		"--json-schema", string(c.Schema),
		"--system-prompt", c.System,
		"--tools", tools,
		"--safe-mode", // keeps the user's CLAUDE.md, hooks, plugins and MCP servers out
		"--no-session-persistence",
	}
	if c.Web {
		args = append(args, "--allowedTools", tools)
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(c.Prompt)
	cmd.WaitDelay = 5 * time.Second // don't hang on pipes held open by a killed child's children
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	c.Log.Printf("[%s] calling claude: model %s, web tools %v, prompt %d bytes", c.Label, cmp.Or(c.Model, "default"), c.Web, len(c.Prompt))
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, 0, errors.New("the claude CLI isn't on your PATH; install Claude Code and log in first")
		}
		return nil, 0, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(nil, 64<<20) // the result line holds the whole answer
	for sc.Scan() {
		var ev streamEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			c.Log.Printf("[%s] stdout: %s", c.Label, clip(sc.Text(), 300))
			continue
		}
		if ev.Type == "result" {
			res = &ev
		}
		logEvent(c, &ev, sc.Text())
	}
	if err := sc.Err(); err != nil {
		c.Log.Printf("[%s] reading claude's output: %v", c.Label, err)
		io.Copy(io.Discard, stdout) // let claude finish so Wait returns
	}
	runErr := cmd.Wait()
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if line != "" {
			c.Log.Printf("[%s] stderr: %s", c.Label, line)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, 0, fmt.Errorf("claude timed out after %s", callTimeout)
	}
	if ctx.Err() != nil {
		return nil, 0, errors.New("interrupted")
	}
	if res == nil {
		return nil, 0, fmt.Errorf("claude failed (%v): %s", runErr, lastLine(stderr.String()))
	}
	if res.IsError {
		return nil, res.TotalCostUSD, fmt.Errorf("claude: %s", res.Result)
	}
	if runErr != nil {
		return nil, res.TotalCostUSD, fmt.Errorf("claude failed (%v): %s", runErr, lastLine(stderr.String()))
	}
	if len(res.StructuredOutput) == 0 || string(res.StructuredOutput) == "null" {
		return nil, res.TotalCostUSD, fmt.Errorf("claude returned no structured output: %s", lastLine(res.Result))
	}
	return res.StructuredOutput, res.TotalCostUSD, nil
}

// logEvent logs one stream event. Searches and fetched pages also show in
// the terminal, so a long call visibly makes progress.
func logEvent(c call, ev *streamEvent, line string) {
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			c.Log.Printf("[%s] session: model %s, tools %s", c.Label, ev.Model, strings.Join(ev.Tools, ", "))
			return
		}
	case "assistant":
		for _, b := range ev.Message.Content {
			if b.Type == "text" {
				c.Log.Printf("[%s] claude: %s", c.Label, clip(b.Text, 500))
			}
			if b.Type != "tool_use" {
				continue
			}
			var in struct{ Query, URL string }
			json.Unmarshal(b.Input, &in)
			switch {
			case b.Name == "StructuredOutput":
				c.Log.Printf("[%s] answer written (%d bytes)", c.Label, len(b.Input))
			case b.Name == "WebSearch" && in.Query != "":
				c.Log.Show("  [%s] searching: %s", c.Label, clip(in.Query, 200))
			case b.Name == "WebFetch" && in.URL != "":
				c.Log.Show("  [%s] reading: %s", c.Label, clip(in.URL, 200))
			default:
				c.Log.Printf("[%s] tool %s: %s", c.Label, b.Name, clip(string(b.Input), 300))
			}
		}
		return
	case "user":
		for _, b := range ev.Message.Content {
			if b.Type == "tool_result" && b.IsError {
				c.Log.Printf("[%s] tool error: %s", c.Label, clip(string(b.Content), 300))
			}
		}
		return
	case "result":
		return // runClaude logs how the call ended
	}
	c.Log.Printf("[%s] %s: %s", c.Label, ev.Type, clip(line, 300))
}

// lastLine returns the last line of s, for short error messages.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
