package main

import (
	"embed"
	"io/fs"
)

// webFS holds the workspace's templates and static files, built into the binary.
//
//go:embed web
var webFS embed.FS

// themes are the workspace's colour themes in dropdown order; the first is
// the default. Each has a dark and a light block in web/static/app.css.
var themes = []string{"catppuccin", "tokyo-night", "gruvbox"}

// staticFiles is web/static, served at /static/.
var staticFiles = mustSub(webFS, "web/static")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
