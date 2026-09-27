package main

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// md renders the markdown Claude writes. goldmark leaves raw HTML out and
// drops dangerous link targets unless it's told to trust its input, and it
// isn't: this text comes from Claude, not from us.
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// markdown renders s as HTML for the workspace pages.
func markdown(s string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(s))
	}
	return template.HTML(buf.String())
}
