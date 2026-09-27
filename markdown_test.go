package main

import (
	"strings"
	"testing"
)

func TestMarkdownRendersFormatting(t *testing.T) {
	got := string(markdown("**bold**, `code` and a [link](https://redis.io)\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"))
	for _, want := range []string{"<strong>bold</strong>", "<code>code</code>", `<a href="https://redis.io">link</a>`, "<table>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownDropsUnsafeContent(t *testing.T) {
	got := string(markdown("<script>alert(1)</script>\n\n[click](javascript:alert(1)) and <img src=x onerror=alert(1)>\n"))
	for _, bad := range []string{"<script", "javascript:", "onerror", "<img"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe %q reached the page:\n%s", bad, got)
		}
	}
}
