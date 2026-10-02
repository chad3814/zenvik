package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestIsAbsPath(t *testing.T) {
	for _, tt := range []struct {
		goos, path string
		want       bool
	}{
		{"linux", "/srv/rips/movie.mkv", true},
		{"darwin", "/Volumes/x.mkv", true},
		{"linux", "rips/movie.mkv", false},
		{"linux", "../movie.mkv", false},
		{"linux", `C:\rips\movie.mkv`, false},
		{"windows", `C:\rips\movie.mkv`, true},
		{"windows", `d:/rips/movie.mkv`, true},
		{"windows", `C:movie.mkv`, true},
		{"windows", `\rips\movie.mkv`, true},
		{"windows", `/rips/movie.mkv`, true},
		{"windows", `\\server\share\movie.mkv`, true},
		{"windows", `rips\movie.mkv`, false},
		{"windows", `..\movie.mkv`, false},
		{"windows", `1:\movie.mkv`, false},
	} {
		if got := isAbsPath(tt.path, tt.goos); got != tt.want {
			t.Errorf("isAbsPath(%q, %s) = %v, want %v", tt.path, tt.goos, got, tt.want)
		}
	}
}

func TestRipOutputFile(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	base := t.TempDir()
	abs := filepath.Join(t.TempDir(), "out dir", "My Movie.mkv")
	if got := ripTarget(t, "-o", abs, disc); got != abs {
		t.Errorf("absolute: %q, want %q", got, abs)
	}
	if got, want := ripTarget(t, "-d", base, "--output-file", filepath.Join("sub", "x.mkv"), disc), filepath.Join(base, "sub", "x.mkv"); got != want {
		t.Errorf("relative to --output-dir: %q, want %q", got, want)
	}
	if got, want := ripTarget(t, "-d", base, "-o", filepath.Join("..", "up.mkv"), disc), filepath.Join(base, "..", "up.mkv"); got != want {
		t.Errorf("escaping the output dir: %q, want %q", got, want)
	}
	if got := ripTarget(t, "-o", "here.mkv", disc); got != "here.mkv" {
		t.Errorf("relative to the current directory: %q", got)
	}
	noExt := filepath.Join(t.TempDir(), "no-extension")
	if got := ripTarget(t, "-o", noExt, disc); got != noExt {
		t.Errorf("no .mkv may be added: %q", got)
	}
	if runtime.GOOS != "windows" {
		raw := filepath.Join(base, "Movie: Part 1?.mkv")
		if got := ripTarget(t, "-o", raw, disc); got != raw {
			t.Errorf("the name must not be cleaned: %q", got)
		}
	}
}

func TestRipOutputFileUsesConfigOutputDir(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	dir := t.TempDir()
	writeUserConfig(t, "output_dir = "+tomlPath(dir))
	if got, want := ripTarget(t, "-o", "m.mkv", disc), filepath.Join(dir, "m.mkv"); got != want {
		t.Errorf("relative to config output_dir: %q, want %q", got, want)
	}
}

func TestRipOutputFileHome(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got, want := ripTarget(t, "-o", "~/rips/m.mkv", disc), filepath.Join(home, "rips", "m.mkv"); got != want {
		t.Errorf("~: %q, want %q", got, want)
	}
}

func TestRipOutputFileUsageErrors(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	f := filepath.Join(t.TempDir(), "x.mkv")
	for _, args := range [][]string{
		{"-o", f, "--template", "{name}.mkv"},
		{"-o", f, "--name", "Film"},
		{"-o", f, "--year", "1999"},
		{"-o", ""},
		{"-o", "somewhere" + string(os.PathSeparator)},
	} {
		code, _, errOut := runCLI(append(append([]string{"rip"}, args...), disc)...)
		if code != 2 || !strings.Contains(errOut, "--output-file") {
			t.Errorf("rip %q: exit %d, stderr %q", args, code, errOut)
		}
	}
}
