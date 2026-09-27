package main

import (
	"io/fs"
	"math"
	"regexp"
	"slices"
	"strconv"
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

// relLuminance is a colour's WCAG relative luminance, with sRGB linearisation.
func relLuminance(hex string) float64 {
	lin := func(c int64) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, _ := strconv.ParseInt(hex[1:3], 16, 32)
	g, _ := strconv.ParseInt(hex[3:5], 16, 32)
	b, _ := strconv.ParseInt(hex[5:7], 16, 32)
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastRatio is the WCAG 2 contrast ratio between two #rrggbb colours.
func contrastRatio(a, b string) float64 {
	l1, l2 := relLuminance(a), relLuminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// TestThemeContrast guards WCAG AA contrast for every theme, including ones
// added later.
func TestThemeContrast(t *testing.T) {
	blocks, _ := themeSection(t)
	block := regexp.MustCompile(`:root\[data-theme="([a-z0-9-]+)"\]\[data-mode="(dark|light)"\]\s*\{([^}]*)\}`)
	value := regexp.MustCompile(`(--[a-z0-9-]+):\s*(#[0-9a-fA-F]{6})`)
	pairs := []struct {
		fg, bg string
		min    float64
	}{
		{"--text", "--surface", 4.5},
		{"--accent", "--surface", 4.5},
		{"--link", "--surface", 4.5},
		{"--text", "--surface-2", 4.5},
		{"--subtext", "--surface", 3.0},
		{"--subtext", "--surface-2", 3.0},
	}
	for _, m := range block.FindAllStringSubmatch(blocks, -1) {
		theme, mode := m[1], m[2]
		vars := map[string]string{}
		for _, v := range value.FindAllStringSubmatch(m[3], -1) {
			vars[v[1]] = v[2]
		}
		for _, pair := range pairs {
			ratio := contrastRatio(vars[pair.fg], vars[pair.bg])
			if ratio < pair.min {
				t.Errorf("%s/%s: %s on %s = %.2f:1, want >= %.1f:1", theme, mode, pair.fg, pair.bg, ratio, pair.min)
			}
		}
	}
}
