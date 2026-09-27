package main

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func readStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(staticFiles, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// themeSection splits app.css into its theme blocks and everything else.
func themeSection(t *testing.T) (blocks, rest string) {
	t.Helper()
	css := readStatic(t, "app.css")
	start := strings.Index(css, "/* === themes === */")
	end := strings.Index(css, "/* === end themes === */")
	if start < 0 || end < start {
		t.Fatal("app.css must keep its theme blocks between /* === themes === */ and /* === end themes === */")
	}
	return css[start:end], css[:start] + css[end:]
}

func TestCSSColoursOnlyInThemeBlocks(t *testing.T) {
	_, rest := themeSection(t)
	colour := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch)\(`)
	if m := colour.FindString(rest); m != "" {
		t.Errorf("colour %q is written outside the theme blocks; use a variable instead", m)
	}
}

func TestEveryThemeHasDarkAndLightBlocks(t *testing.T) {
	blocks, _ := themeSection(t)
	block := regexp.MustCompile(`:root\[data-theme="([a-z0-9-]+)"\]\[data-mode="(dark|light)"\]\s*\{([^}]*)\}`)
	varName := regexp.MustCompile(`(--[a-z0-9-]+)\s*:`)
	sets := map[string][]string{}
	var first []string
	for _, m := range block.FindAllStringSubmatch(blocks, -1) {
		var names []string
		for _, v := range varName.FindAllStringSubmatch(m[3], -1) {
			names = append(names, v[1])
		}
		slices.Sort(names)
		sets[m[1]+"/"+m[2]] = names
		if first == nil {
			first = names
		}
	}
	for _, name := range themes {
		for _, mode := range []string{"dark", "light"} {
			got, ok := sets[name+"/"+mode]
			switch {
			case !ok:
				t.Errorf("theme %q has no %s block in app.css", name, mode)
			case !slices.Equal(got, first):
				t.Errorf("theme %s/%s sets %v; every block must set the same variables: %v", name, mode, got, first)
			}
		}
	}
	if len(sets) != 2*len(themes) {
		t.Errorf("app.css has %d theme blocks for %d themes; list every theme in web.go", len(sets), len(themes))
	}
}

func TestCSSVariablesAreDefined(t *testing.T) {
	css := readStatic(t, "app.css")
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--[a-z0-9-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`var\((--[a-z0-9-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("app.css uses %s but never defines it", m[1])
		}
	}
}

func TestMermaidIsBundled(t *testing.T) {
	js := readStatic(t, "mermaid.min.js")
	if len(js) < 1<<20 || !strings.Contains(js, "mermaid") {
		t.Fatalf("web/static/mermaid.min.js looks wrong (%d bytes)", len(js))
	}
	if !strings.Contains(readStatic(t, "mermaid.LICENSE"), "MIT") {
		t.Error("mermaid's licence must ship with the bundle")
	}
}

func TestScriptsAreServedFiles(t *testing.T) {
	for _, name := range []string{"theme-boot.js", "app.js"} {
		if js := readStatic(t, name); !strings.Contains(js, "tattva-") {
			t.Errorf("%s doesn't use the tattva-* storage keys", name)
		}
	}
}
