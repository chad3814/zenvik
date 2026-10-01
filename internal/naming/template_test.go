package naming

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func render(t *testing.T, tmpl string, vars map[string]string) (string, error) {
	t.Helper()
	p, err := Parse(tmpl)
	if err != nil {
		return "", err
	}
	return p.Render(vars)
}

func TestParseErrors(t *testing.T) {
	for tmpl, want := range map[string]string{
		"{title}.mkv": "unknown variable {title}",
		"{name.mkv":   "unclosed {",
		"name}.mkv":   "unexpected }",
		"[a [b]]":     "nested [",
		"[{year}":     "unclosed [",
		"x]":          "unexpected ]",
		"":            "empty",
		"{}":          "unknown variable {}",
	} {
		_, err := Parse(tmpl)
		if !errors.Is(err, ErrTemplate) || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) err = %v, want ErrTemplate mentioning %q", tmpl, err, want)
		}
	}
}

func TestRender(t *testing.T) {
	vars := map[string]string{"name": "Sample Movie", "year": "2026", "label": "SAMPLE_MOVIE", "playlist": "00800"}
	noYear := map[string]string{"name": "Sample Movie", "label": "SAMPLE_MOVIE", "playlist": "00800"}
	tests := []struct {
		tmpl string
		vars map[string]string
		want string
	}{
		{"{name}[ ({year})].mkv", vars, "Sample Movie (2026).mkv"},
		{"{name}[ ({year})].mkv", noYear, "Sample Movie.mkv"},
		{"{name}[ ({year})]/{name}[ ({year})].mkv", vars, "Sample Movie (2026)/Sample Movie (2026).mkv"},
		{"{label}_{playlist}", vars, "SAMPLE_MOVIE_00800.mkv"},
		{"{name}.MKV", vars, "Sample Movie.MKV"},
		{"Movies/{name}: Director's Cut.mkv", vars, "Movies/Sample Movie_ Director's Cut.mkv"},
		{"[{year} - ]{name}", noYear, "Sample Movie.mkv"},
	}
	for _, tt := range tests {
		got, err := render(t, tt.tmpl, tt.vars)
		if err != nil || got != tt.want {
			t.Errorf("render(%q) = %q, %v; want %q", tt.tmpl, got, err, tt.want)
		}
	}
}

func TestRenderSafety(t *testing.T) {
	long := strings.Repeat("é", 200) // 400 bytes
	tests := []struct {
		name string
		tmpl string
		vars map[string]string
		want string
	}{
		{"slash in value", "{name}.mkv", map[string]string{"name": "AC/DC: Live"}, "AC_DC_ Live.mkv"},
		{"backslash in value", "{name}.mkv", map[string]string{"name": `a\b`}, "a_b.mkv"},
		{"dotdot value", "{name}.mkv", map[string]string{"name": ".."}, "_.mkv"},
		{"reserved name", "{name}.mkv", map[string]string{"name": "CON"}, "CON_.mkv"},
		{"reserved dir", "{name}/x.mkv", map[string]string{"name": "aux"}, "aux_/x.mkv"},
		{"trailing dots", "{name}.../x", map[string]string{"name": "Mr"}, "Mr/x.mkv"},
	}
	for _, tt := range tests {
		got, err := render(t, tt.tmpl, tt.vars)
		if err != nil || got != tt.want {
			t.Errorf("%s: got %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}

	got, err := render(t, "{name}.mkv", map[string]string{"name": long})
	if err != nil || len(got) > maxComponentBytes || !strings.HasSuffix(got, ".mkv") || !utf8.ValidString(got) {
		t.Errorf("long name: %d bytes %q, %v", len(got), got, err)
	}

	for name, tmpl := range map[string]string{
		"absolute":        "/movies/{name}",
		"parent dir":      "../{name}",
		"empty component": "{year}/{name}",
		"dot component":   "./{name}",
	} {
		if _, err := render(t, tmpl, map[string]string{"name": "x"}); !errors.Is(err, ErrTemplate) {
			t.Errorf("%s (%q): err = %v, want ErrTemplate", name, tmpl, err)
		}
	}
}
