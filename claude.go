package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// call is one headless Claude Code invocation.
type call struct {
	System string // system prompt, replacing Claude Code's default
	Prompt string // sent on stdin
	Schema []byte // JSON Schema the output must match
	Web    bool   // allow WebSearch and WebFetch
	Model  string // empty means Claude Code's default
}

// callTimeout bounds one call. It's a var so tests can shorten it.
var callTimeout = 20 * time.Minute

// runClaude runs `claude -p` for c in an empty temp dir and returns the
// structured output and the call's cost in USD.
func runClaude(ctx context.Context, c call) (json.RawMessage, float64, error) {
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
		"--output-format", "json",
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	runErr := cmd.Run()
	if errors.Is(runErr, exec.ErrNotFound) {
		return nil, 0, errors.New("the claude CLI isn't on your PATH; install Claude Code and log in first")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, 0, fmt.Errorf("claude timed out after %s", callTimeout)
	}
	if ctx.Err() != nil {
		return nil, 0, errors.New("interrupted")
	}
	var res struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		TotalCostUSD     float64         `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
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

// lastLine returns the last line of s, for short error messages.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
