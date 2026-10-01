package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

// writeUserConfig writes the test config file and removes it afterwards.
func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "zenvik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
}

// tomlPath quotes a path as a TOML literal string (safe for Windows backslashes).
func tomlPath(p string) string { return "'" + p + "'" }

// ripTarget runs rip with an empty PATH (so it stops before muxing, exit 4)
// and returns the output path printed on the "Ripping" line.
func ripTarget(t *testing.T, args ...string) string {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	code, out, errOut := runCLI(append([]string{"rip"}, args...)...)
	if code != 4 {
		t.Fatalf("rip %v: exit %d, stderr %q", args, code, errOut)
	}
	line := lineWith(out, "Ripping ")
	_, target, ok := strings.Cut(line, "→ ")
	if !ok {
		t.Fatalf("no Ripping line in %q", out)
	}
	return target
}

func TestRipNamingFlags(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	out := t.TempDir()
	if got, want := ripTarget(t, "-o", out, "--name", "Big Film", "--year", "2024", disc), filepath.Join(out, "Big Film (2024).mkv"); got != want {
		t.Errorf("target = %q, want %q", got, want)
	}
	if got, want := ripTarget(t, "-o", out, "--template", "{label}/{playlist}", disc), filepath.Join(out, "SAMPLE_MOVIE", "00800.mkv"); got != want {
		t.Errorf("template target = %q, want %q", got, want)
	}
}

func TestRipPresetAndFlags(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	writeUserConfig(t, fmt.Sprintf(`
output_dir = %s
preset     = "plex"

[presets.plex]
output_dir = %s
template   = "{name}/{name}.mkv"
`, tomlPath(a), tomlPath(b)))
	tests := []struct {
		args []string
		want string
	}{
		{[]string{disc}, filepath.Join(b, "Sample Movie", "Sample Movie.mkv")},
		{[]string{"--preset=", disc}, filepath.Join(a, "Sample Movie.mkv")},
		{[]string{"-o", c, "--template", "{label}", disc}, filepath.Join(c, "SAMPLE_MOVIE.mkv")},
	}
	for _, tt := range tests {
		if got := ripTarget(t, tt.args...); got != tt.want {
			t.Errorf("rip %v → %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestRipConfigErrors(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	cases := []struct {
		name   string
		config string
		args   []string
		want   string
	}{
		{"unknown key", `outptu_dir = "x"`, []string{disc}, "outptu_dir"},
		{"unknown preset", "[presets.plex]\n", []string{"--preset", "nope", disc}, "available: plex"},
		{"bad year", "", []string{"--year", "99", disc}, "four digits"},
		{"bad template flag", "", []string{"--template", "{bogus}", disc}, "bogus"},
		{"bad template config", `template = "[{name}"`, []string{disc}, `template "[{name}"`},
		{"empty component", "", []string{"--template", "{name}/{year}/{name}", disc}, "empty path component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.config != "" {
				writeUserConfig(t, tc.config)
			}
			code, _, errOut := runCLI(append([]string{"rip", "-o", t.TempDir()}, tc.args...)...)
			if code != 2 || !strings.Contains(errOut, tc.want) {
				t.Errorf("exit %d, stderr %q; want 2 mentioning %q", code, errOut, tc.want)
			}
		})
	}
}

func TestRipMkvmergePathFromConfig(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	missing := filepath.Join(t.TempDir(), "my-mkvmerge")
	writeUserConfig(t, "mkvmerge_path = "+tomlPath(missing))
	code, _, errOut := runCLI("rip", "-o", t.TempDir(), disc)
	if code != 4 || !strings.Contains(errOut, "my-mkvmerge") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestInfoMinDurationFromConfig(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	writeUserConfig(t, `min_duration = "10s"`)
	code, out, _ := runCLI("info", disc)
	if code != 0 || titleRow(out, "00099") == "" || strings.Contains(out, "hidden") {
		t.Errorf("exit %d, out:\n%s", code, out)
	}
}

func TestTemplateErrorText(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	code, _, errOut := runCLI("rip", "-o", t.TempDir(), "--template", "{bogus}", disc)
	if code != 2 || !strings.HasPrefix(errOut, "zenvik: invalid name template: ") ||
		strings.Contains(errOut, "configuration") || strings.Count(errOut, "zenvik:") != 1 {
		t.Errorf("flag: exit %d, stderr %q", code, errOut)
	}

	writeUserConfig(t, `template = "[{name}"`)
	code, _, errOut = runCLI("rip", "-o", t.TempDir(), disc)
	if code != 2 || !strings.HasPrefix(errOut, "zenvik: invalid configuration: ") ||
		!strings.Contains(errOut, `template "[{name}": invalid name template: `) || strings.Count(errOut, "zenvik:") != 1 {
		t.Errorf("config: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runCLI("info", disc); code != 2 || !strings.Contains(errOut, `template "[{name}"`) {
		t.Errorf("info with a bad config template: exit %d, stderr %q", code, errOut)
	}
}

func TestConfigReadErrorExits2(t *testing.T) {
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "zenvik", "config.toml")
	if err := os.MkdirAll(dir, 0o755); err != nil { // a directory where the file should be
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(dir) })
	disc := writeDisc(t, testdisc.SampleMovie())
	if code, _, errOut := runCLI("info", disc); code != 2 {
		t.Errorf("info: exit %d, stderr %q", code, errOut)
	}
}
