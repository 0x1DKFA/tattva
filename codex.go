package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runCodex runs one non-interactive Codex CLI call in an empty directory.
func runCodex(ctx context.Context, c call) (json.RawMessage, float64, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "tattva-call-")
	if err != nil {
		return nil, -1, err
	}
	defer os.RemoveAll(dir)
	schemaPath, outputPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "answer.json")
	if err := os.WriteFile(schemaPath, c.Schema, 0o600); err != nil {
		return nil, -1, err
	}
	args := []string{}
	if c.Web {
		args = append(args, "--search")
	}
	args = append(args, "exec", "--ignore-user-config", "--json", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "--ask-for-approval", "never",
		"--output-schema", schemaPath, "--output-last-message", outputPath}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	args = append(args, "-")
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = dir
	prompt := c.Prompt
	if c.System != "" {
		prompt = "Instructions:\n" + c.System + "\n\nRequest:\n" + prompt
	}
	cmd.Stdin = strings.NewReader(prompt)
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, -1, err
	}
	c.Log.Printf("[%s] calling codex: model %s, web search %v, prompt %d bytes", c.Label, displayModel(c.Model), c.Web, len(c.Prompt))
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, -1, errors.New("the codex CLI isn't on your PATH; install Codex and sign in first")
		}
		return nil, -1, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		logCodexEvent(c, sc.Bytes())
	}
	if err := sc.Err(); err != nil {
		c.Log.Printf("[%s] reading codex output: %v", c.Label, err)
		io.Copy(io.Discard, stdout)
	}
	runErr := cmd.Wait()
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if line != "" {
			c.Log.Printf("[%s] codex stderr: %s", c.Label, line)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, -1, fmt.Errorf("codex timed out after %s", callTimeout)
	}
	if ctx.Err() != nil {
		return nil, -1, errors.New("interrupted")
	}
	if runErr != nil {
		return nil, -1, fmt.Errorf("codex failed (%v): %s", runErr, lastLine(stderr.String()))
	}
	out, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, -1, fmt.Errorf("codex returned no structured output: %s", lastLine(stderr.String()))
	}
	c.Log.Printf("[%s] finished; Codex does not report call cost", c.Label)
	return json.RawMessage(bytes.TrimSpace(out)), -1, nil
}

func logCodexEvent(c call, line []byte) {
	var event struct {
		Type string `json:"type"`
		Item struct {
			Type   string `json:"type"`
			Text   string `json:"text"`
			Action struct {
				Type    string   `json:"type"`
				Query   string   `json:"query"`
				Queries []string `json:"queries"`
				URL     string   `json:"url"`
			} `json:"action"`
		} `json:"item"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		c.Log.Printf("[%s] codex output: %s", c.Label, clip(string(line), 300))
		return
	}
	if event.Item.Type == "agent_message" && event.Item.Text != "" {
		c.Log.Show("  [%s] codex: %s", c.Label, clip(event.Item.Text, 300))
		return
	}
	if event.Item.Type == "web_search_call" {
		query := event.Item.Action.Query
		if query == "" && len(event.Item.Action.Queries) > 0 {
			query = strings.Join(event.Item.Action.Queries, ", ")
		}
		if query != "" {
			c.Log.Show("  [%s] searching: %s", c.Label, clip(query, 200))
			return
		}
		if event.Item.Action.URL != "" {
			c.Log.Show("  [%s] reading: %s", c.Label, clip(event.Item.Action.URL, 200))
			return
		}
	}
	c.Log.Printf("[%s] codex event: %s", c.Label, clip(string(line), 300))
}

func displayModel(model string) string {
	if model == "" {
		return "default"
	}
	return model
}

func costLabel(cost float64) string {
	if cost < 0 {
		return "cost not reported"
	}
	return fmt.Sprintf("$%.2f", cost)
}
