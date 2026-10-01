# zenvik Milestone 4 (Polish) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the user-facing polish from the spec:
- a TOML config file with presets
- output naming templates (`zenvik.FormatName`) and the `rip` flags `--name`, `--year`, `--template` and `--preset`
- `zenvik doctor`, which reports dependencies, config and leftover mounts

It also covers the items deferred from Milestone 3: `mkvmerge_path`, a configurable minimum duration, a record of active mounts, safe names on Windows, `udisksctl` in the C locale, and allow-list shell quoting.

**Architecture:**
- `internal/config` loads and validates the TOML file and resolves settings: flags > preset > file > defaults.
- `internal/naming` gains a template parser and renderer that produce safe relative paths. The root `FormatName` exposes them with the disc's variables.
- The library adds `Open(ctx, path, opts ...OpenOption)` with `WithMinDuration`, and a `RipOptions.MkvmergePath` field.
- `internal/mount` records each active mount in a small JSON file, removed on detach, so `doctor` can list leftovers on every OS.
- The CLI reads the config for `info` and `rip`, and adds `doctor`.

**Tech Stack:** Go 1.27; `github.com/pelletier/go-toml/v2` (the second and last allowed third-party dependency); `cobra`; and the M1–M3 packages.

**Spec:** `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, sections 3 (`FormatName`), 6 (leftover mounts and `doctor`), 7 (`rip` flags, `doctor`, exit codes), 8 (config and naming) and 11 (milestone 4).

## Global Constraints

- Module `github.com/chad3814/zenvik`, `go 1.27`. No cgo: `CGO_ENABLED=0 go build ./...` must succeed.
- Allowed direct dependencies are exactly `github.com/spf13/cobra` and `github.com/pelletier/go-toml/v2`. Only `internal/config` may import go-toml.
- `bluray`, `udf`, `internal/rank`, `internal/mux`, `internal/mount` and `internal/naming` import only the standard library, plus project packages.
- Errors are sentinel values wrapped with `%w`, checkable with `errors.Is`. Library error messages start with `zenvik:`.
- Config (spec §8), verbatim:
  - **Location:** `$XDG_CONFIG_HOME/zenvik/config.toml`, falling back to `~/.config/zenvik/config.toml` on macOS and Linux, and `%AppData%\zenvik\config.toml` on Windows. A missing file means built-in defaults.
  - **Keys:** `output_dir`, `template`, `min_duration`, `mkvmerge_path`, `preset`, and `[presets.<name>]` tables.
  - **Precedence:** CLI flags > selected preset (`--preset`, else `preset`) > top-level config > built-in defaults.
  - **Built-in defaults:** `output_dir` = current directory, `template` = `{name}[ ({year})].mkv`, `min_duration` = `2m`.
  - Presets may override only `output_dir`, `template` and `min_duration`.
  - An unknown preset name or an invalid value is an error (exit 2) that names the key.
- Templates (spec §8), verbatim:
  - **Variables:**
    - `{name}`: `--name`, else the bdmt title, else the cleaned volume label
    - `{year}`: `--year` only
    - `{label}`: the raw volume label
    - `{playlist}`: the playlist ID
  - `[...]` is an optional group, dropped if any variable inside it is empty. Groups do not nest.
  - An unknown variable is an error.
  - Characters invalid on any of macOS, Linux or Windows (`<>:"\|?*` and control characters) are replaced with `_`.
  - `/` in the template creates directories; `/` inside a variable's value is replaced.
  - Paths are relative to `output_dir`, and `~` is expanded.
  - An existing output file causes `ErrOutputExists` unless `--overwrite` is passed.
- CLI (spec §7):
  - `zenvik rip <path> [-p|--playlist ID] [-o|--output-dir DIR] [--name NAME] [--year YYYY] [--template TMPL] [--preset NAME] [--overwrite] [--dry-run]`
  - `zenvik doctor` checks that mkvmerge is found and its version, whether ISO mounting is possible on this OS, the config path and whether it parses, and any leftover zenvik mounts.
  - Exit codes: `0` success, `1` failure, `2` usage error, `3` unsupported or encrypted source, `4` missing or too-old dependency.
- Tests never read or write the developer's real config or state. Packages that touch them set `XDG_CONFIG_HOME` and `XDG_STATE_HOME` to temp directories.
- Code passes `gofmt`, `go vet ./...` (including `GOOS=linux` and `GOOS=windows`), `golangci-lint run` (v2.13.2, with and without `--build-tags integration`), `go test -race ./...` and `go test -tags integration ./...`.
- Work in worktree `/Users/chad/Projects/zenvik/worktrees/m4-polish` on branch `feat/m4-polish`.
- Never `git push`.
- Commits are SSH-signed. If signing fails, commit with `git -c commit.gpgsign=false commit ...` and say so in the task report.

## Design Decisions (within the spec's latitude)

1. **Durations are strings validated by zenvik**, not decoded by the TOML library, so every error names its key (`min_duration` or `presets.plex.min_duration`). Negative durations are invalid.
2. **Unknown config keys are errors**, which catches typos like `outptu_dir`. The error names the key.
3. **`XDG_CONFIG_HOME` is honored on every OS**, including Windows, before the per-OS fallback. Tests use it.
4. **Rendering rules for templates:**
   - Every path component is cleaned:
     - invalid characters become `_`
     - surrounding spaces and trailing dots are trimmed
     - Windows reserved base names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`) get a `_` suffix
     - each component is cut to 247 bytes at a rune boundary, keeping the extension, so that `<name>.partial` still fits in 255 bytes
   - The final component gets `.mkv` if it doesn't already end in `.mkv`, ignoring case.
   - Empty, `.` or `..` components, and templates that render to an absolute path, are errors.
   - A `\` in template text is an invalid character, not a separator.
   - `naming.SafeFileName` uses the same component cleaning.
5. **`FormatName` returns a path relative to `output_dir`**, using the OS's separators. The CLI joins it with `output_dir`.
6. **Library options.** `Open(ctx, path, opts ...OpenOption)` with `WithMinDuration(d)` stays compatible with existing callers. `RipOptions.MkvmergePath` (empty means `PATH`) carries `mkvmerge_path`.
7. **Which commands read the config.**
   - `info` and `rip` both read it. `info` uses the resolved `min_duration` (top-level, or the config's default preset).
   - Only `rip` accepts `--preset`, `--template`, `--output-dir`, `--name` and `--year`.
   - The template is validated before the disc is opened, so config mistakes fail fast with exit 2.
8. **Mount records.**
   - `mount.Attach` writes a JSON record to `$XDG_STATE_HOME/zenvik/mounts/` (otherwise `os.UserCacheDir()/zenvik/mounts/`) with the image, mount dir, device, PID and time.
   - `Detach` removes it on success. A failed detach keeps it.
   - A record counts as leftover when its PID is no longer running.
   - Writing the record is best-effort: a write failure never fails the rip.
9. **`doctor` output and exit code.**
   - It prints one line per check: `✓` for OK, `!` for a warning, `✗` for a problem.
   - Exit `4` if mkvmerge is missing or too old; otherwise `2` if the config is invalid; otherwise `1` if there are leftover mounts; otherwise `0`.
   - "ISO mounting unavailable" is only a warning, since folders still work.
10. **`info --json` `kind` values** become the machine-friendly `"iso"` and `"bdmv"`. The table keeps the readable names.
11. **Mount commands run with `LC_ALL=C`** (including `udisksctl`), so output parsing doesn't depend on the user's language.
12. **`shellQuote` uses an allow-list.** Arguments made only of `A–Z a–z 0–9 _ . / : , @ % + = -`, and not starting with `=`, stay bare. Anything else is single-quoted.
13. **`--year` must be four digits.** Otherwise it's a usage error (exit 2).

## Review Focus

These are input classes the spec implies but no feature test exercises. Each has a test in the task that owns the code.

1. **A config file with typos, unknown keys, bad durations or an unknown preset.** It must exit 2 with a message naming the key or listing the available presets, never silently use defaults. Tests:
   - Task 1, `TestLoadErrors` and `TestResolveUnknownPreset`
   - Task 5, `TestRipConfigErrors`
2. **Hostile or odd disc names in templates:** titles containing `/`, `..`, reserved Windows names, very long titles, Unicode, or an empty year outside a group. Each yields a safe relative path or a clear template error. Never an escape from `output_dir`. Tests: Task 2, `TestRenderSafety` and `TestFormatName`.
3. **Tests must never touch the developer's real `~/.config/zenvik` or state directory.** Tests: Task 4, registry tests use a temp `XDG_STATE_HOME`; Task 5, a `TestMain` in `cmd/zenvik` sets temp `XDG_CONFIG_HOME` and `XDG_STATE_HOME` for the whole package.
4. **Leftover mounts after a crash.** A failed detach keeps the record, `doctor` reports it with the right cleanup command, and a successful detach removes it. Tests:
   - Task 4, `TestAttachRegistersAndDetachUnregisters` and `TestLeftovers`
   - Task 6, `TestDoctorReportsLeftovers`
5. **Precedence ambiguity:** flag against preset against file against default for `output_dir`, `template` and `min_duration`. Tests: Task 1, `TestResolvePrecedence`; Task 5, `TestRipPresetAndFlags`.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/config/config.go` | `ErrInvalid`, defaults, `File`, `Preset`, `DefaultPath`, `Load`, `Settings`, `Flags`, `Resolve`, `ExpandHome` |
| `internal/naming/naming.go` (modify) | Shared component cleaning; `SafeFileName` handles reserved names and length |
| `internal/naming/template.go` | `ErrTemplate`, `Template`, `Parse`, `Render` |
| `name.go` (modify) | `NameVars`, `FormatName` |
| `errors.go` (modify) | `ErrInvalidTemplate = naming.ErrTemplate` |
| `disc.go` (modify) | `OpenOption`, `WithMinDuration`, variadic `Open` |
| `rip.go` (modify) | `RipOptions.MkvmergePath` |
| `internal/mount/registry.go` | `Record`, `Cleanup`, register/unregister, `Leftovers` |
| `internal/mount/process_unix.go`, `process_windows.go` | `processAlive` |
| `internal/mount/mount.go` (modify) | `Available`, record on Attach/Detach, `LC_ALL=C` runner |
| `internal/mount/attach_*.go` (modify) | `available` tool names, `cleanupCommand`, device field (linux) |
| `cmd/zenvik/settings.go` | `loadSettings`: config → settings → template check |
| `cmd/zenvik/rip.go`, `info.go`, `main.go`, `progress.go` (modify) | New rip flags; config use; exit 2 for config/template errors; JSON kind; allow-list quoting |
| `cmd/zenvik/main_test.go` (modify) | `TestMain` isolating config and state directories |
| `cmd/zenvik/doctor.go` | `doctor` command |
| `README.md`, spec (modify) | Document config, templates, presets, doctor |

## Dependency Waves (for parallel execution)

- **A:** Task 1 (config), Task 2 (templates), Task 3 (library options), Task 4 (mount records). These are independent: no shared files.
- **B:** Task 5 (CLI config, rip and info), after Tasks 1–3.
- **C:** Task 6 (`doctor`), after Tasks 1, 4 and 5. It edits `main.go` after Task 5 does.
- **D:** Task 7 (docs and verification).

---
### Task 1: Configuration (`internal/config`)

**Files:**
- Create: `internal/config/config.go`
- Modify: `go.mod`, `go.sum` (add go-toml/v2)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing from the project.
- Produces (used by Tasks 5 and 6):
  - `var ErrInvalid = errors.New("zenvik: invalid configuration")`
  - `const DefaultTemplate = "{name}[ ({year})].mkv"`
  - `const DefaultMinDuration = 2 * time.Minute`
  - `type Preset struct { OutputDir, Template, MinDuration *string }` (TOML keys `output_dir`, `template`, `min_duration`)
  - `type File struct { Path string; Found bool; OutputDir, Template, MinDuration, MkvmergePath, Preset *string; Presets map[string]Preset }`
  - `func DefaultPath() (string, error)`
  - `func Load(path string) (*File, error)`
  - `type Settings struct { OutputDir, Template string; MinDuration time.Duration; MkvmergePath, Preset string }`
  - `type Flags struct { Preset, OutputDir, Template *string }` (nil means the flag wasn't given)
  - `func Resolve(f *File, flags Flags) (Settings, error)`
  - `func ExpandHome(p string) (string, error)`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/pelletier/go-toml/v2@latest`
Expected: `go.mod` gains a `github.com/pelletier/go-toml/v2 v2.x.y` requirement. It may be listed as indirect until Step 4 adds an import; `go mod tidy` after Step 4 fixes that.

- [ ] **Step 2: Write the failing tests**

`internal/config/config_test.go`:
```go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ptr(s string) *string { return &s }

func TestDefaultPath(t *testing.T) {
	x := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", x)
	if p, err := DefaultPath(); err != nil || p != filepath.Join(x, "zenvik", "config.toml") {
		t.Errorf("with XDG_CONFIG_HOME: %q, %v", p, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	if p, err := DefaultPath(); err != nil || p != filepath.Join(home, ".config", "zenvik", "config.toml") {
		t.Errorf("fallback: %q, %v", p, err)
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || f.Found || f.OutputDir != nil {
		t.Errorf("Load(missing) = %+v, %v", f, err)
	}
}

const sample = `
output_dir    = "~/Movies"
template      = "{name}[ ({year})].mkv"
min_duration  = "2m"
mkvmerge_path = ""
preset        = ""

[presets.plex]
output_dir = "/Volumes/Media/Movies"
template   = "{name}[ ({year})]/{name}[ ({year})].mkv"
`

func TestLoadSample(t *testing.T) {
	f, err := Load(writeConfig(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Found || *f.OutputDir != "~/Movies" || *f.MinDuration != "2m" || *f.Preset != "" {
		t.Errorf("top level = %+v", f)
	}
	p, ok := f.Presets["plex"]
	if !ok || *p.OutputDir != "/Volumes/Media/Movies" || p.MinDuration != nil {
		t.Errorf("plex preset = %+v", p)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]struct {
		body string
		want string
	}{
		"unknown key":         {`outptu_dir = "x"`, "outptu_dir"},
		"unknown preset key":  {"[presets.plex]\nmkvmerge_path = \"x\"", "mkvmerge_path"},
		"syntax":              {`output_dir = `, "line"},
		"bad duration":        {`min_duration = "two minutes"`, "min_duration"},
		"negative duration":   {`min_duration = "-5s"`, "min_duration"},
		"bad preset duration": {"[presets.plex]\nmin_duration = \"soon\"", "presets.plex.min_duration"},
		"wrong type":          {`output_dir = 5`, "output_dir"},
	}
	for name, tt := range tests {
		_, err := Load(writeConfig(t, tt.body))
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want ErrInvalid mentioning %q", name, err, tt.want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	file := &File{
		OutputDir:    ptr("~/Movies"),
		MinDuration:  ptr("90s"),
		MkvmergePath: ptr("~/bin/mkvmerge"),
		Preset:       ptr("plex"),
		Presets: map[string]Preset{
			"plex": {OutputDir: ptr("/media/plex"), Template: ptr("{name}/{name}.mkv")},
			"kodi": {MinDuration: ptr("10m")},
		},
	}
	tests := []struct {
		name  string
		file  *File
		flags Flags
		want  Settings
	}{
		{"defaults", &File{}, Flags{}, Settings{OutputDir: ".", Template: DefaultTemplate, MinDuration: 2 * time.Minute}},
		{"file default preset", file, Flags{}, Settings{OutputDir: "/media/plex", Template: "{name}/{name}.mkv", MinDuration: 90 * time.Second,
			MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), Preset: "plex"}},
		{"flag preset wins", file, Flags{Preset: ptr("kodi")}, Settings{OutputDir: filepath.Join(home, "Movies"), Template: DefaultTemplate,
			MinDuration: 10 * time.Minute, MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), Preset: "kodi"}},
		{"flags win over preset", file, Flags{OutputDir: ptr("/tmp/out"), Template: ptr("{label}.mkv")}, Settings{OutputDir: "/tmp/out",
			Template: "{label}.mkv", MinDuration: 90 * time.Second, MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), Preset: "plex"}},
		{"empty flag preset disables default preset", file, Flags{Preset: ptr("")}, Settings{OutputDir: filepath.Join(home, "Movies"),
			Template: DefaultTemplate, MinDuration: 90 * time.Second, MkvmergePath: filepath.Join(home, "bin", "mkvmerge")}},
	}
	for _, tt := range tests {
		got, err := Resolve(tt.file, tt.flags)
		if err != nil || got != tt.want {
			t.Errorf("%s: Resolve = %+v, %v\nwant %+v", tt.name, got, err, tt.want)
		}
	}
}

func TestResolveUnknownPreset(t *testing.T) {
	file := &File{Presets: map[string]Preset{"plex": {}, "kodi": {}}}
	_, err := Resolve(file, Flags{Preset: ptr("jellyfin")})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `"jellyfin"`) || !strings.Contains(err.Error(), "kodi, plex") {
		t.Errorf("err = %v", err)
	}
	if _, err := Resolve(&File{}, Flags{Preset: ptr("x")}); err == nil || !strings.Contains(err.Error(), "no presets") {
		t.Errorf("no presets: err = %v", err)
	}
	if _, err := Resolve(&File{}, Flags{Template: ptr("  ")}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "template") {
		t.Errorf("empty template: err = %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for in, want := range map[string]string{
		"~":          home,
		"~/Movies":   filepath.Join(home, "Movies"),
		"/abs/~/x":   "/abs/~/x",
		"rel":        "rel",
		"":           "",
		"~someone/x": "~someone/x",
	} {
		if got, err := ExpandHome(in); err != nil || got != want {
			t.Errorf("ExpandHome(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL to compile (`undefined: Load`, ...).

- [ ] **Step 4: Implement**

`internal/config/config.go`:
```go
// Package config loads zenvik's TOML configuration file and resolves the
// effective settings from built-in defaults, the file, a preset and
// command-line flags.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ErrInvalid reports a configuration problem; the message names the key.
var ErrInvalid = errors.New("zenvik: invalid configuration")

// Built-in defaults.
const (
	DefaultTemplate    = "{name}[ ({year})].mkv"
	DefaultMinDuration = 2 * time.Minute
)

// Preset overrides a subset of settings.
type Preset struct {
	OutputDir   *string `toml:"output_dir"`
	Template    *string `toml:"template"`
	MinDuration *string `toml:"min_duration"`
}

// File is a parsed config file. Nil fields were not set.
type File struct {
	Path         string            `toml:"-"` // where it was loaded from
	Found        bool              `toml:"-"` // false when the file does not exist
	OutputDir    *string           `toml:"output_dir"`
	Template     *string           `toml:"template"`
	MinDuration  *string           `toml:"min_duration"`
	MkvmergePath *string           `toml:"mkvmerge_path"`
	Preset       *string           `toml:"preset"`
	Presets      map[string]Preset `toml:"presets"`
}

// DefaultPath returns where the config file lives:
// $XDG_CONFIG_HOME/zenvik/config.toml when XDG_CONFIG_HOME is set (on any OS),
// otherwise %AppData%\zenvik\config.toml on Windows and
// ~/.config/zenvik/config.toml elsewhere.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "zenvik", "config.toml"), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "zenvik", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "zenvik", "config.toml"), nil
}

// Load reads the config file at path. A missing file gives an empty File
// with Found false. Syntax errors, unknown keys and invalid values wrap
// ErrInvalid and name the key or line.
func Load(path string) (*File, error) {
	f := &File{Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("zenvik: reading config %s: %w", path, err)
	}
	f.Found = true
	if err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(f); err != nil {
		return nil, fmt.Errorf("%w: %s: %s", ErrInvalid, path, describe(err))
	}
	if err := checkDuration("min_duration", f.MinDuration); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
	}
	for name, p := range f.Presets {
		if err := checkDuration("presets."+name+".min_duration", p.MinDuration); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
		}
	}
	return f, nil
}

func describe(err error) string {
	var sme *toml.StrictMissingError
	if errors.As(err, &sme) {
		var keys []string
		for _, e := range sme.Errors {
			keys = append(keys, strings.Join(e.Key(), "."))
		}
		return "unknown key(s): " + strings.Join(keys, ", ")
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		msg := fmt.Sprintf("line %d, column %d: %s", row, col, de.Error())
		if k := de.Key(); len(k) > 0 {
			msg += " (key " + strings.Join(k, ".") + ")"
		}
		return msg
	}
	return err.Error()
}

func checkDuration(key string, s *string) error {
	if s == nil {
		return nil
	}
	if _, err := parseDuration(*s); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use values like \"90s\" or \"2m\")", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("negative duration %q", s)
	}
	return d, nil
}

// Settings are the effective values after Resolve.
type Settings struct {
	OutputDir    string
	Template     string
	MinDuration  time.Duration
	MkvmergePath string // empty: search PATH
	Preset       string // preset applied, "" if none
}

// Flags holds command-line values; nil means the flag was not given.
type Flags struct {
	Preset    *string
	OutputDir *string
	Template  *string
}

// Resolve applies built-in defaults, then the file's top-level values, then
// the selected preset (flags.Preset if given, else the file's preset; an
// empty name selects none), then flags. "~" is expanded in paths.
func Resolve(f *File, flags Flags) (Settings, error) {
	s := Settings{OutputDir: ".", Template: DefaultTemplate, MinDuration: DefaultMinDuration}
	apply := func(prefix string, out, tmpl, minDur *string) error {
		if out != nil {
			s.OutputDir = *out
		}
		if tmpl != nil {
			s.Template = *tmpl
		}
		if minDur != nil {
			d, err := parseDuration(*minDur)
			if err != nil {
				return fmt.Errorf("%w: %smin_duration: %w", ErrInvalid, prefix, err)
			}
			s.MinDuration = d
		}
		return nil
	}
	if err := apply("", f.OutputDir, f.Template, f.MinDuration); err != nil {
		return Settings{}, err
	}
	if f.MkvmergePath != nil {
		s.MkvmergePath = *f.MkvmergePath
	}
	name := ""
	if f.Preset != nil {
		name = *f.Preset
	}
	if flags.Preset != nil {
		name = *flags.Preset
	}
	if name != "" {
		p, ok := f.Presets[name]
		if !ok {
			return Settings{}, fmt.Errorf("%w: unknown preset %q%s", ErrInvalid, name, presetList(f))
		}
		if err := apply("presets."+name+".", p.OutputDir, p.Template, p.MinDuration); err != nil {
			return Settings{}, err
		}
		s.Preset = name
	}
	if flags.OutputDir != nil {
		s.OutputDir = *flags.OutputDir
	}
	if flags.Template != nil {
		s.Template = *flags.Template
	}
	if strings.TrimSpace(s.Template) == "" {
		return Settings{}, fmt.Errorf("%w: template is empty", ErrInvalid)
	}
	var err error
	if s.OutputDir, err = ExpandHome(s.OutputDir); err != nil {
		return Settings{}, err
	}
	if s.MkvmergePath, err = ExpandHome(s.MkvmergePath); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func presetList(f *File) string {
	if len(f.Presets) == 0 {
		return " (no presets are defined)"
	}
	names := make([]string, 0, len(f.Presets))
	for n := range f.Presets {
		names = append(names, n)
	}
	sort.Strings(names)
	return " (available: " + strings.Join(names, ", ") + ")"
}

// ExpandHome replaces a leading "~" or "~/" (or "~\") with the user's home
// directory. Other paths, including "~user/...", are returned unchanged.
func ExpandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("zenvik: expanding %q: %w", p, err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}
```

If go-toml's error for `output_dir = 5` doesn't mention the key, check what `de.Key()` returns. If it's empty for type mismatches, add the key from the message text, or handle `*toml.DecodeError` positions so the test's `"output_dir"` expectation holds. Adjust `describe`, never the test.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go mod tidy && go test -race ./internal/config/ && go vet ./... && golangci-lint run`
Expected: PASS; `go.mod` lists go-toml/v2 as a direct requirement; 0 lint issues.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config
git commit -m "Add TOML configuration with presets and strict validation"
```

---

### Task 2: Naming templates and `FormatName`

**Files:**
- Create: `internal/naming/template.go`
- Modify: `internal/naming/naming.go`, `name.go`, `errors.go`
- Test: `internal/naming/template_test.go`, `internal/naming/naming_test.go` (add cases), `name_test.go` (add `TestFormatName`)

**Interfaces:**
- Consumes: `zenvik.Disc` (with `Label` and `Name()`), `zenvik.Title` (with `ID`), and `testdisc.SampleMovie` plus the root test helpers `openDisc`, `writeDisc` and `mustTitle` (tests).
- Produces (used by Task 5):
  - `var naming.ErrTemplate = errors.New("zenvik: invalid name template")`
  - `func naming.Parse(src string) (*naming.Template, error)`
  - `func (t *naming.Template) Render(vars map[string]string) (string, error)`: returns a relative, slash-separated path
  - `naming.SafeFileName` now also handles Windows reserved names and the 247-byte limit
  - root: `type NameVars struct { Name, Year string }`, `func FormatName(tmpl string, d *Disc, t *Title, vars NameVars) (string, error)` (relative OS path), and `var ErrInvalidTemplate = naming.ErrTemplate`

- [ ] **Step 1: Write the failing tests**

`internal/naming/template_test.go`:
```go
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
		"{title}.mkv":     "unknown variable {title}",
		"{name.mkv":       "unclosed {",
		"name}.mkv":       "unexpected }",
		"[a [b]]":         "nested [",
		"[{year}":         "unclosed [",
		"x]":              "unexpected ]",
		"":                "empty",
		"{}":              "unknown variable {}",
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
```

Append to `internal/naming/naming_test.go`:
```go
func TestSafeFileNameReservedAndLong(t *testing.T) {
	for in, want := range map[string]string{
		"CON":     "CON_",
		"nul.txt": "nul_.txt",
		"Com1":    "Com1_",
		"COM10":   "COM10",
		"console": "console",
	} {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ab", 300)
	if got := SafeFileName(long); len(got) > maxComponentBytes {
		t.Errorf("SafeFileName(long) = %d bytes", len(got))
	}
}
```
(Add `"strings"` to that file's imports.)

Append to `name_test.go` (keep its existing imports and add `"errors"`):
```go
func TestFormatName(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	main := mustTitle(t, d, "00800")
	tests := []struct {
		tmpl string
		vars zenvik.NameVars
		want string
	}{
		{"{name}[ ({year})].mkv", zenvik.NameVars{}, "Sample Movie.mkv"},
		{"{name}[ ({year})].mkv", zenvik.NameVars{Year: "2026"}, "Sample Movie (2026).mkv"},
		{"{name}[ ({year})].mkv", zenvik.NameVars{Name: "Override", Year: "1999"}, "Override (1999).mkv"},
		{"{name}[ ({year})]/{name}[ ({year})].mkv", zenvik.NameVars{Year: "2026"}, filepath.Join("Sample Movie (2026)", "Sample Movie (2026).mkv")},
		{"{label}_{playlist}", zenvik.NameVars{}, "SAMPLE_MOVIE_00800.mkv"},
	}
	for _, tt := range tests {
		got, err := zenvik.FormatName(tt.tmpl, d, main, tt.vars)
		if err != nil || got != tt.want {
			t.Errorf("FormatName(%q, %+v) = %q, %v; want %q", tt.tmpl, tt.vars, got, err, tt.want)
		}
	}
	if _, err := zenvik.FormatName("{bogus}", d, main, zenvik.NameVars{}); !errors.Is(err, zenvik.ErrInvalidTemplate) {
		t.Errorf("bad template err = %v", err)
	}
}
```
(`name_test.go` must import `"github.com/chad3814/zenvik"`, `"errors"` and `"path/filepath"`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/naming/ . -run 'Parse|Render|SafeFileName|FormatName'`
Expected: FAIL to compile (`undefined: Parse`, `undefined: maxComponentBytes`, `undefined: zenvik.FormatName`).

- [ ] **Step 3: Implement**

Replace the body of `internal/naming/naming.go` below `CleanLabel` (keep `CleanLabel` unchanged) with:
```go
// maxComponentBytes keeps "<component>.partial" within the common 255-byte
// file name limit.
const maxComponentBytes = 255 - len(".partial")

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// cleanComponent makes one path component safe on macOS, Linux and
// Windows: invalid characters (<>:"/\|?* and controls) become "_",
// surrounding spaces and trailing dots are removed, and Windows reserved
// device names get a "_" suffix. The result may be empty.
func cleanComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	base, ext, _ := strings.Cut(out, ".")
	if reservedNames[strings.ToUpper(base)] {
		out = base + "_"
		if ext != "" {
			out += "." + ext
		}
	}
	return out
}

// limitLength shortens s to at most max bytes at a rune boundary, keeping a
// short extension (such as ".mkv") intact.
func limitLength(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ext := path.Ext(s)
	if len(ext) > 16 {
		ext = ""
	}
	base := strings.TrimSuffix(s, ext)
	limit := max - len(ext)
	cut := 0
	for i := range base {
		if i > limit {
			break
		}
		cut = i
	}
	if len(base) <= limit {
		cut = len(base)
	}
	return strings.TrimRight(base[:cut], ". ") + ext
}

// SafeFileName makes s usable as a file name on macOS, Linux and Windows
// (see cleanComponent), limits it to 247 bytes, and returns "untitled" for
// an empty result.
func SafeFileName(s string) string {
	out := limitLength(cleanComponent(s), maxComponentBytes)
	if out == "" {
		return "untitled"
	}
	return out
}
```
Update the import block of `naming.go` to `"path"`, `"strings"`, `"unicode"`.

`internal/naming/template.go`:
```go
package naming

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrTemplate reports an invalid name template.
var ErrTemplate = errors.New("zenvik: invalid name template")

var knownVariables = map[string]bool{"name": true, "year": true, "label": true, "playlist": true}

type segment struct {
	lit      string
	variable string // set for {variable} references
}

type part struct {
	segs     []segment
	optional bool
}

// Template is a parsed name template.
type Template struct{ parts []part }

func errf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTemplate, fmt.Sprintf(format, args...))
}

// Parse parses a name template: literal text, {variable} references (name,
// year, label, playlist) and [optional groups], which do not nest.
func Parse(src string) (*Template, error) {
	t := &Template{}
	cur := part{}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			cur.segs = append(cur.segs, segment{lit: lit.String()})
			lit.Reset()
		}
	}
	inGroup := false
	for i := 0; i < len(src); i++ {
		switch c := src[i]; c {
		case '{':
			end := strings.IndexByte(src[i:], '}')
			if end < 0 {
				return nil, errf("unclosed { at position %d", i)
			}
			name := src[i+1 : i+end]
			if !knownVariables[name] {
				return nil, errf("unknown variable {%s} (use {name}, {year}, {label} or {playlist})", name)
			}
			flush()
			cur.segs = append(cur.segs, segment{variable: name})
			i += end
		case '}':
			return nil, errf("unexpected } at position %d", i)
		case '[':
			if inGroup {
				return nil, errf("nested [ at position %d (groups do not nest)", i)
			}
			flush()
			if len(cur.segs) > 0 {
				t.parts = append(t.parts, cur)
			}
			cur = part{optional: true}
			inGroup = true
		case ']':
			if !inGroup {
				return nil, errf("unexpected ] at position %d", i)
			}
			flush()
			t.parts = append(t.parts, cur)
			cur = part{}
			inGroup = false
		default:
			lit.WriteByte(c)
		}
	}
	if inGroup {
		return nil, errf("unclosed [")
	}
	flush()
	if len(cur.segs) > 0 {
		t.parts = append(t.parts, cur)
	}
	if len(t.parts) == 0 {
		return nil, errf("template is empty")
	}
	return t, nil
}

// Render fills in vars (missing keys are empty) and returns a relative,
// slash-separated path whose components are safe file names. An optional
// group is dropped when any variable in it is empty. "/" and "\" inside a
// value become "_", so values cannot create directories. The final
// component gets ".mkv" unless it already ends with it.
func (t *Template) Render(vars map[string]string) (string, error) {
	var b strings.Builder
	for _, p := range t.parts {
		var pb strings.Builder
		keep := true
		for _, s := range p.segs {
			if s.variable == "" {
				pb.WriteString(s.lit)
				continue
			}
			v := strings.TrimSpace(vars[s.variable])
			if v == "" && p.optional {
				keep = false
				break
			}
			v = strings.NewReplacer("/", "_", `\`, "_").Replace(v)
			if v != "" && strings.Trim(v, ".") == "" {
				v = "_" // a value of "." or ".." must not become a path component
			}
			pb.WriteString(v)
		}
		if keep {
			b.WriteString(pb.String())
		}
	}
	return finish(b.String())
}

func finish(p string) (string, error) {
	if strings.HasPrefix(p, "/") {
		return "", errf("template must produce a relative path, got %q", p)
	}
	comps := strings.Split(p, "/")
	for i, c := range comps {
		if c == "." || c == ".." {
			return "", errf("template produced a %q path component in %q", c, p)
		}
		clean := cleanComponent(c)
		if clean == "" {
			return "", errf("template produced an empty path component in %q (is a variable outside [...] empty?)", p)
		}
		comps[i] = limitLength(clean, maxComponentBytes)
	}
	last := len(comps) - 1
	if !strings.EqualFold(path.Ext(comps[last]), ".mkv") {
		comps[last] = limitLength(comps[last]+".mkv", maxComponentBytes)
	}
	return strings.Join(comps, "/"), nil
}
```

Append to `name.go` (add imports `"path/filepath"` and `"github.com/chad3814/zenvik/internal/naming"` if missing):
```go
// NameVars are caller-supplied template variables.
type NameVars struct {
	Name string // overrides the disc name for {name}
	Year string // {year}
}

// FormatName renders a name template for title t of disc d and returns a
// relative path (OS separators) to place under the output directory.
// Variables: {name} (vars.Name, else d.Name()), {year} (vars.Year),
// {label} (the volume label) and {playlist} (t.ID).
func FormatName(tmpl string, d *Disc, t *Title, vars NameVars) (string, error) {
	tp, err := naming.Parse(tmpl)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(vars.Name)
	if name == "" {
		name = d.Name()
	}
	id := ""
	if t != nil {
		id = t.ID
	}
	rel, err := tp.Render(map[string]string{"name": name, "year": vars.Year, "label": d.Label, "playlist": id})
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(rel), nil
}
```

Add to `errors.go`'s variable block (import `internal/naming`):
```go
	// ErrInvalidTemplate: a name template is malformed or renders an unsafe path.
	ErrInvalidTemplate = naming.ErrTemplate
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/naming/ . && go vet ./... && golangci-lint run`
Expected: PASS (including the existing `TestSafeFileName` and `TestCleanLabel` cases); 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add internal/naming name.go name_test.go errors.go
git commit -m "Add output naming templates and FormatName"
```

---

### Task 3: Library options: minimum duration and mkvmerge path

**Files:**
- Modify: `disc.go`, `rip.go`
- Test: `options_test.go` (package `zenvik_test`)

**Interfaces:**
- Consumes: M2/M3 `Open`, `scanTitles`, `defaultMinDuration`, `Rip`, `mux.Find`; and the test helpers `openDisc`, `writeDisc` and `mustTitle`.
- Produces (used by Task 5):
  - `type OpenOption func(*openConfig)`
  - `func WithMinDuration(d time.Duration) OpenOption`
  - `func Open(ctx context.Context, path string, opts ...OpenOption) (*Disc, error)`
  - `RipOptions.MkvmergePath string` (empty means search `PATH`)

- [ ] **Step 1: Write the failing test**

`options_test.go`:
```go
package zenvik_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestWithMinDuration(t *testing.T) {
	root := writeDisc(t, testdisc.SampleMovie())
	if menu := mustTitle(t, openDisc(t, root), "00099"); !menu.Rank.Filtered {
		t.Fatal("default: the 20 s menu title should be filtered")
	}
	d, err := zenvik.Open(context.Background(), root, zenvik.WithMinDuration(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if menu := mustTitle(t, d, "00099"); menu.Rank.Filtered {
		t.Errorf("min 10s: 00099 rank = %+v", menu.Rank)
	}
}

func TestRipMkvmergePath(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	missing := filepath.Join(t.TempDir(), "no-such-mkvmerge")
	_, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{
		OutputPath:   filepath.Join(t.TempDir(), "x.mkv"),
		MkvmergePath: missing,
	})
	if !errors.Is(err, zenvik.ErrMkvmergeNotFound) {
		t.Errorf("err = %v, want ErrMkvmergeNotFound for %s", err, missing)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run 'WithMinDuration|RipMkvmergePath'`
Expected: FAIL to compile (`undefined: zenvik.WithMinDuration`, `unknown field MkvmergePath`).

- [ ] **Step 3: Implement**

In `disc.go`, add (with `"time"` imported):
```go
// OpenOption customizes Open.
type OpenOption func(*openConfig)

type openConfig struct {
	minDuration time.Duration
}

// WithMinDuration sets the shortest title that can be the main feature
// (default 2 minutes).
func WithMinDuration(d time.Duration) OpenOption {
	return func(c *openConfig) { c.minDuration = d }
}
```
Change `Open`'s signature to `func Open(ctx context.Context, path string, opts ...OpenOption) (*Disc, error)`. At its start, build `cfg := openConfig{minDuration: defaultMinDuration}` and apply each option. Pass `cfg.minDuration` to `scanTitles` in place of `defaultMinDuration`. Update the doc comment to mention the options.

In `rip.go`, add this field to `RipOptions`:
```go
	MkvmergePath string // mkvmerge executable; empty searches PATH
```
Change `mux.Find(ctx, "")` to `mux.Find(ctx, opts.MkvmergePath)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./... && go vet ./... && golangci-lint run`
Expected: PASS; existing callers of `zenvik.Open(ctx, path)` compile unchanged.

- [ ] **Step 5: Commit**

```bash
git add disc.go rip.go options_test.go
git commit -m "Add Open options and RipOptions.MkvmergePath"
```

---

### Task 4: Mount records, tool availability, C locale (`internal/mount`)

**Files:**
- Create: `internal/mount/registry.go`, `internal/mount/process_unix.go`, `internal/mount/process_windows.go`, `internal/mount/process_other.go`
- Modify: `internal/mount/mount.go`, `internal/mount/attach_darwin.go`, `attach_linux.go`, `attach_windows.go`, `attach_other.go`, and the per-OS test files (isolate state); `rip_iso_integration_test.go` (isolate state)
- Test: `internal/mount/registry_test.go`; additions to `attach_darwin_test.go`, `attach_linux_test.go`, `attach_windows_test.go`

**Interfaces:**
- Consumes: the M3 `mount` internals (`runner`, `lookPath`, `Mount`, `attach`, `psQuote`).
- Produces (used by Task 6):
  - `type Record struct { Image, Dir, Device string; PID int; Created time.Time }` (JSON fields `image`, `dir`, `device`, `pid`, `created`) with `func (r Record) Cleanup() string`
  - `func Leftovers() ([]Record, error)`: records whose PID isn't running, sorted by `Created`
  - `func Available() (tool string, err error)`: `"hdiutil"`, `"udisksctl"` or `"PowerShell Mount-DiskImage"`; otherwise an error wrapping `ErrUnavailable`
  - **Behavior changes:**
    - `Attach` writes a record (best-effort) to the state dir: `$XDG_STATE_HOME/zenvik/mounts`, otherwise `os.UserCacheDir()/zenvik/mounts`.
    - `Detach` removes the record after a successful detach and keeps it after a failed one.
    - `runner` sets `LC_ALL=C`.
- Cleanup commands:
  - darwin: `hdiutil detach -force "<dir>"`
  - linux: `udisksctl unmount -b <dev> && udisksctl loop-delete -b <dev>`
  - windows: `Dismount-DiskImage -ImagePath '<image>'`
  - other: `""`

- [ ] **Step 1: Write the failing tests**

`internal/mount/registry_test.go`:
```go
package mount

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolateState points the record directory at a temp dir.
func isolateState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "zenvik", "mounts")
}

// deadPID returns the PID of a process that has already exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestLeftovers(t *testing.T) {
	dir := isolateState(t)
	live := Record{Image: "/i/live.iso", Dir: "/m/live", PID: os.Getpid(), Created: time.Unix(100, 0)}
	dead := Record{Image: "/i/dead.iso", Dir: "/m/dead", Device: "/dev/loop9", PID: deadPID(t), Created: time.Unix(200, 0)}
	for _, r := range []Record{live, dead} {
		if _, err := register(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Leftovers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Image != dead.Image || got[0].Dir != dead.Dir || got[0].Device != dead.Device || !got[0].Created.Equal(dead.Created) {
		t.Fatalf("Leftovers = %+v, want only %+v", got, dead)
	}
	cleanup := got[0].Cleanup()
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(cleanup, "hdiutil detach -force") || !strings.Contains(cleanup, dead.Dir) {
			t.Errorf("cleanup = %q", cleanup)
		}
	case "linux":
		if !strings.Contains(cleanup, "udisksctl unmount -b /dev/loop9") {
			t.Errorf("cleanup = %q", cleanup)
		}
	case "windows":
		if !strings.Contains(cleanup, "Dismount-DiskImage -ImagePath '/i/dead.iso'") {
			t.Errorf("cleanup = %q", cleanup)
		}
	}
}

func TestLeftoversNoDirectory(t *testing.T) {
	isolateState(t)
	if got, err := Leftovers(); err != nil || got != nil {
		t.Errorf("Leftovers = %v, %v", got, err)
	}
}
```

Add to `attach_darwin_test.go`:
1. In `fakeRunner`, as its first line after `t.Helper()`: `t.Setenv("XDG_STATE_HOME", t.TempDir())`.
2. These tests:
```go
func recordFiles(t *testing.T) []string {
	t.Helper()
	dir, err := stateDir()
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, filepath.Join(dir, e.Name()))
	}
	return names
}

func TestAttachRegistersAndDetachUnregisters(t *testing.T) {
	fakeRunner(t)
	m, err := Attach(context.Background(), "/images/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	files := recordFiles(t)
	if len(files) != 1 {
		t.Fatalf("records = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), m.Dir) || !strings.Contains(string(b), "/images/x.iso") {
		t.Errorf("record = %s", b)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	if files := recordFiles(t); len(files) != 0 {
		t.Errorf("record not removed: %v", files)
	}
}

func TestFailedDetachKeepsRecord(t *testing.T) {
	fakeRunner(t, nil, errors.New("busy"), errors.New("still busy"))
	m, err := Attach(context.Background(), "/images/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Detach(context.Background()); err == nil {
		t.Fatal("expected detach error")
	}
	if files := recordFiles(t); len(files) != 1 {
		t.Errorf("record should be kept after a failed detach: %v", files)
	}
	_ = os.RemoveAll(m.Dir)
}

func TestAvailableDarwin(t *testing.T) {
	fakeRunner(t)
	if tool, err := Available(); err != nil || tool != "hdiutil" {
		t.Errorf("Available = %q, %v", tool, err)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Available(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}
```
(Add `"path/filepath"` to its imports if it's missing.)

Add to `attach_linux_test.go`:
1. In `fakeRunner`, add `t.Setenv("XDG_STATE_HOME", t.TempDir())` as its first line after `t.Helper()`.
2. This test:
```go
func TestAttachLinuxRecordsDevice(t *testing.T) {
	fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Mounted /dev/loop9 at /media/u/DISC\n"})
	m, err := Attach(context.Background(), "/i/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if m.device != "/dev/loop9" {
		t.Errorf("device = %q", m.device)
	}
	if tool, err := Available(); err != nil || tool != "udisksctl" {
		t.Errorf("Available = %q, %v", tool, err)
	}
}
```

Add to `attach_windows_test.go`:
1. At the start of `TestAttachWindows`, add `t.Setenv("XDG_STATE_HOME", t.TempDir())`.
2. At its end, after the `lookPath` reset that sets "not found", add:
```go
	if _, err := Available(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Available without powershell: err = %v", err)
	}
```

In `integration_darwin_test.go`'s `TestAttachRealImage`, and in the root `rip_iso_integration_test.go`'s `realISO` helper, add `t.Setenv("XDG_STATE_HOME", t.TempDir())` as the first statement, so real mounts never write records into the developer's cache directory.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mount/`
Expected: FAIL to compile (`undefined: register`, `undefined: Leftovers`, `undefined: stateDir`, `undefined: Available`, `m.device undefined`).

- [ ] **Step 3: Implement**

`internal/mount/registry.go`:
```go
package mount

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Record describes a mount zenvik made. It is kept on disk from Attach
// until a successful Detach, so mounts left behind by a crash can be found.
type Record struct {
	Image   string    `json:"image"`
	Dir     string    `json:"dir"`
	Device  string    `json:"device,omitempty"`
	PID     int       `json:"pid"`
	Created time.Time `json:"created"`
}

// Cleanup returns the command that removes this mount by hand, or "" when
// mounting is not supported on this OS.
func (r Record) Cleanup() string { return cleanupCommand(r) }

// stateDir is where mount records live: $XDG_STATE_HOME/zenvik/mounts, or
// the user cache directory's zenvik/mounts.
func stateDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "zenvik", "mounts"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik", "mounts"), nil
}

// register writes r and returns the record's file path.
func register(r Record) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("%d-%d.json", r.PID, r.Created.UnixNano()))
	return p, os.WriteFile(p, b, 0o644)
}

// Leftovers returns recorded mounts whose zenvik process is no longer
// running, oldest first. Unreadable record files are skipped.
func Leftovers() ([]Record, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(b, &r) != nil || r.Dir == "" || processAlive(r.PID) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}
```

`internal/mount/process_unix.go`:
```go
//go:build unix

package mount

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with this PID exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
```

`internal/mount/process_windows.go`:
```go
//go:build windows

package mount

import "os"

// processAlive reports whether a process with this PID exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
```

`internal/mount/process_other.go`:
```go
//go:build !unix && !windows

package mount

func processAlive(pid int) bool { return false }
```

Modify `internal/mount/mount.go`:
- Add the fields `device string // block device (Linux), for cleanup instructions` and `record string // path of this mount's record file` to `Mount`.
- Change `Attach`'s tail to:
  ```go
  	m, err := attach(ctx, abs)
  	if err != nil {
  		return nil, err
  	}
  	// Best-effort: a missing record only means doctor can't report this
  	// mount if it is left behind.
  	if p, err := register(Record{Image: abs, Dir: m.Dir, Device: m.device, PID: os.Getpid(), Created: time.Now()}); err == nil {
  		m.record = p
  	}
  	return m, nil
  ```
- In `Detach`, after `err := m.detach(ctx)`, add:
  ```go
  	if err == nil && m.record != "" {
  		_ = os.Remove(m.record)
  		m.record = ""
  	}
  ```
- Add:
  ```go
  // Available reports which tool mounts images on this system, or an error
  // wrapping ErrUnavailable when none can be used.
  func Available() (string, error) { return available() }
  ```
- Change `runner` to set the C locale:
  ```go
  var runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
  	cmd := exec.CommandContext(ctx, name, args...)
  	cmd.Env = append(os.Environ(), "LC_ALL=C") // stable, parseable English output
  	return cmd.CombinedOutput()
  }
  ```
  Add `"os"` to the imports.

Add to `attach_darwin.go`:
```go
func available() (string, error) {
	if _, err := lookPath("hdiutil"); err != nil {
		return "", fmt.Errorf("%w: hdiutil not found", ErrUnavailable)
	}
	return "hdiutil", nil
}

func cleanupCommand(r Record) string { return fmt.Sprintf("hdiutil detach -force %q", r.Dir) }
```

Add to `attach_linux.go`, and set `device: dev` in the returned `&Mount{...}`:
```go
func available() (string, error) {
	if _, err := lookPath("udisksctl"); err != nil {
		return "", fmt.Errorf("%w: udisksctl not found (install udisks2)", ErrUnavailable)
	}
	return "udisksctl", nil
}

func cleanupCommand(r Record) string {
	return fmt.Sprintf("udisksctl unmount -b %s && udisksctl loop-delete -b %s", r.Device, r.Device)
}
```

Add to `attach_windows.go`:
```go
func available() (string, error) {
	if _, err := lookPath("powershell.exe"); err != nil {
		return "", fmt.Errorf("%w: powershell.exe not found", ErrUnavailable)
	}
	return "PowerShell Mount-DiskImage", nil
}

func cleanupCommand(r Record) string { return "Dismount-DiskImage -ImagePath " + psQuote(r.Image) }
```

Add to `attach_other.go`:
```go
func available() (string, error) {
	return "", fmt.Errorf("%w: no image mounting support on %s", ErrUnavailable, runtime.GOOS)
}

func cleanupCommand(Record) string { return "" }
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test -race ./internal/mount/ && go test -tags integration -run TestAttachRealImage ./internal/mount/
GOOS=linux go vet ./internal/mount/ && GOOS=windows go vet ./internal/mount/ && GOOS=freebsd go vet ./internal/mount/
GOOS=linux go test -c -o /dev/null ./internal/mount/ && GOOS=windows go test -c -o /dev/null ./internal/mount/
go test -race ./... && go vet ./... && golangci-lint run
```
Expected: PASS; `hdiutil info` shows nothing attached; nothing was written under `~/Library/Caches/zenvik`.

- [ ] **Step 5: Commit**

```bash
git add internal/mount rip_iso_integration_test.go
git commit -m "Record active mounts for leftover detection; report mount tool; run mount commands in the C locale"
```

---

### Task 5: CLI: config, naming flags, info min duration, JSON kind, quoting

**Files:**
- Create: `cmd/zenvik/settings.go`, `cmd/zenvik/config_test.go`
- Modify: `cmd/zenvik/main.go` (exit 2 for config and template errors), `cmd/zenvik/rip.go`, `cmd/zenvik/info.go`, `cmd/zenvik/progress.go`, `cmd/zenvik/main_test.go` (`TestMain`; JSON kind expectation), `cmd/zenvik/progress_test.go`

**Interfaces:**
- Consumes:
  - Task 1: `config.DefaultPath`, `Load`, `Resolve`, `Flags`, `Settings`, `ErrInvalid`
  - Task 2: `naming.Parse`, `zenvik.FormatName`, `zenvik.NameVars`, `zenvik.ErrInvalidTemplate`
  - Task 3: `zenvik.WithMinDuration`, `RipOptions.MkvmergePath`
  - M3: the CLI (`run`, `exitCode`, `usageError`, `pickTitle`, `withHint`, `closeSecond`, `progressPrinter`, `shellQuote`, and the test helpers `writeDisc`, `runCLI`, `lineWith`)
- Produces:
  - `func loadSettings(flags config.Flags) (config.Settings, error)` (package `main`, used by Task 6)
  - the `rip` flags `--name`, `--year`, `--template` and `--preset`; `-o` now defaults to the config value
  - `info` honors `min_duration`
  - `info --json` `"kind"` is `"iso"` or `"bdmv"`
  - exit 2 for `config.ErrInvalid` and `zenvik.ErrInvalidTemplate`
  - allow-list `shellQuote`
  - `TestMain` isolates `XDG_CONFIG_HOME` and `XDG_STATE_HOME`
  - test helper `writeUserConfig(t, body string)`

- [ ] **Step 1: Isolate the test environment and write the failing tests**

Add to `cmd/zenvik/main_test.go`, with imports `"os"` and `"path/filepath"` if missing:
```go
// TestMain keeps tests away from the developer's real config and state.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zenvik-cli-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
```
In the same file, change `TestInfoJSON`'s `got.Kind != "BDMV folder"` to `got.Kind != "bdmv"`.

`cmd/zenvik/config_test.go`:
```go
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
		{"bad template config", `template = "[{name}"`, []string{disc}, "template"},
		{"empty component", "", []string{"--template", "{year}/{name}", disc}, "empty path component"},
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
	if code != 0 || !strings.Contains(out, "00099") || strings.Contains(out, "hidden") {
		t.Errorf("exit %d, out:\n%s", code, out)
	}
}
```

Add to `cmd/zenvik/progress_test.go`:
```go
func TestShellQuoteAllowList(t *testing.T) {
	for in, want := range map[string]string{
		"":         "''",
		"=x":       "'=x'",
		"a=b":      "a=b",
		"--x=1,2":  "--x=1,2",
		"é":        "'é'",
		"$HOME":    "'$HOME'",
		"~":        "'~'",
		"a\nb":     "'a\nb'",
		"C:/x_y-z": "C:/x_y-z",
	} {
		if got := shellQuote([]string{in}); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/zenvik/`
Expected: FAIL. The new flags are unknown, there's no config handling, the JSON kind is still "BDMV folder", and the allow-list cases fail.

- [ ] **Step 3: Implement**

`cmd/zenvik/settings.go`:
```go
package main

import (
	"fmt"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/naming"
)

// loadSettings reads the config file and resolves the effective settings,
// validating the template so mistakes fail before any disc work.
func loadSettings(flags config.Flags) (config.Settings, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Settings{}, err
	}
	f, err := config.Load(path)
	if err != nil {
		return config.Settings{}, err
	}
	s, err := config.Resolve(f, flags)
	if err != nil {
		return config.Settings{}, err
	}
	if _, err := naming.Parse(s.Template); err != nil {
		return config.Settings{}, fmt.Errorf("%w: template %q: %w", config.ErrInvalid, s.Template, err)
	}
	return s, nil
}
```

In `cmd/zenvik/main.go`'s `exitCode`, add after the `usageError` case (import `internal/config`):
```go
	case errors.Is(err, config.ErrInvalid), errors.Is(err, zenvik.ErrInvalidTemplate):
		return 2
```

In `cmd/zenvik/rip.go`:
- Declare `var playlist, outDir, name, year, template, preset string`.
- At the top of `RunE`, before opening the disc:
  ```go
  			if year != "" && !yearRE.MatchString(year) {
  				return usageError{fmt.Errorf("--year must be four digits, got %q", year)}
  			}
  			var flags config.Flags
  			if cmd.Flags().Changed("output-dir") {
  				flags.OutputDir = &outDir
  			}
  			if cmd.Flags().Changed("template") {
  				flags.Template = &template
  			}
  			if cmd.Flags().Changed("preset") {
  				flags.Preset = &preset
  			}
  			s, err := loadSettings(flags)
  			if err != nil {
  				return err
  			}
  ```
- Open with `zenvik.Open(cmd.Context(), args[0], zenvik.WithMinDuration(s.MinDuration))`.
- Replace the output path computation with:
  ```go
  			rel, err := zenvik.FormatName(s.Template, d, t, zenvik.NameVars{Name: name, Year: year})
  			if err != nil {
  				return err
  			}
  			out := filepath.Join(s.OutputDir, rel)
  ```
- Add `MkvmergePath: s.MkvmergePath` to the `RipOptions` literal.
- Replace the flag definitions with:
  ```go
  	cmd.Flags().StringVarP(&playlist, "playlist", "p", "", "rip this playlist (e.g. 00800) instead of the main feature")
  	cmd.Flags().StringVarP(&outDir, "output-dir", "o", "", "directory for the MKV file (default: config output_dir, else the current directory)")
  	cmd.Flags().StringVar(&name, "name", "", "title for {name} (default: the disc's title or tidied volume label)")
  	cmd.Flags().StringVar(&year, "year", "", "year for {year}, e.g. 1999")
  	cmd.Flags().StringVar(&template, "template", "", `output name template, e.g. "{name}[ ({year})].mkv" (default: config template)`)
  	cmd.Flags().StringVar(&preset, "preset", "", "config preset to apply (empty for none)")
  	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing output file")
  	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the output path and mkvmerge command without ripping")
  ```
- Add at file scope: `var yearRE = regexp.MustCompile(`^\d{4}$`)`. Import `regexp` and `internal/config`, and drop the now-unused `internal/naming` import.

In `cmd/zenvik/info.go`:
- At the start of `RunE`:
  ```go
  			s, err := loadSettings(config.Flags{})
  			if err != nil {
  				return err
  			}
  			d, err := zenvik.Open(cmd.Context(), args[0], zenvik.WithMinDuration(s.MinDuration))
  ```
- In `writeJSON`, set `Kind: kindName(d.Kind)` and add:
  ```go
  // kindName is the machine-readable source kind for JSON output.
  func kindName(k zenvik.SourceKind) string {
  	if k == zenvik.ISO {
  		return "iso"
  	}
  	return "bdmv"
  }
  ```

In `cmd/zenvik/progress.go`, replace `shellQuote` with:
```go
// shellQuote renders args as a POSIX shell command line. Arguments made only
// of letters, digits and _./:,@%+=- (and not starting with "=", which zsh
// expands) stay bare; anything else is single-quoted.
func shellQuote(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.HasPrefix(a, "=") && strings.IndexFunc(a, unsafeShellRune) < 0 {
			out[i] = a
			continue
		}
		out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}

func unsafeShellRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	case strings.ContainsRune("_./:,@%+=-", r):
		return false
	}
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test -race ./cmd/zenvik/ && go test -tags integration ./cmd/zenvik/
go vet ./... && golangci-lint run && golangci-lint run --build-tags integration
ls "$HOME/.config/zenvik" 2>/dev/null; echo "real config untouched check done"
```
Expected: PASS; 0 lint issues. The tests never create `~/.config/zenvik`.

- [ ] **Step 5: Commit**

```bash
git add cmd/zenvik
git commit -m "Read config in info and rip; add naming flags, presets, JSON kind values and allow-list quoting"
```

---

### Task 6: `zenvik doctor`

**Files:**
- Create: `cmd/zenvik/doctor.go`
- Modify: `cmd/zenvik/main.go` (register `doctor`; `doctorError` exit codes)
- Test: `cmd/zenvik/doctor_test.go`, `cmd/zenvik/doctor_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes:
  - Task 1: `config.DefaultPath`, `Load`, `Resolve`, `Flags`
  - Task 2: `naming.Parse`
  - Task 4: `mount.Available`, `mount.Leftovers`, `mount.Record.Cleanup`
  - M3: `mux.Find`, `Mkvmerge.Path`, `Mkvmerge.Version`
  - Task 5 test helpers: `TestMain` isolation, `writeUserConfig`, `runCLI`
- Produces: `zenvik doctor`.
- Output: one line per check, in this order: config, mkvmerge, ISO mounting, leftover mounts.
  - `✓ config: <path> (not found; using defaults)`, or `✓ config: <path>` with ` (default preset "<name>")` appended when one is set, or `✗ config: <error>`
  - `✓ mkvmerge: <path> (v<version>)` or `✗ mkvmerge: <error>`
  - `✓ ISO mounting: <tool>` or `! ISO mounting: <error> (rip from a folder instead)`
  - `✓ no leftover mounts`, or for each leftover `✗ leftover mount: <image> at <dir> (since <RFC 3339 time>); remove with: <cleanup>`, or `! leftover mounts: could not check: <error>`
- Exit code (Design Decision 9): `func doctorCode(mkvmergeOK, configOK bool, leftovers int) int`.

- [ ] **Step 1: Write the failing tests**

`cmd/zenvik/doctor_test.go`:
```go
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDoctorCode(t *testing.T) {
	tests := []struct {
		mkOK, cfgOK bool
		leftovers   int
		want        int
	}{
		{true, true, 0, 0},
		{true, true, 2, 1},
		{true, false, 2, 2},
		{false, false, 2, 4},
		{false, true, 0, 4},
	}
	for _, tt := range tests {
		if got := doctorCode(tt.mkOK, tt.cfgOK, tt.leftovers); got != tt.want {
			t.Errorf("doctorCode(%v, %v, %d) = %d, want %d", tt.mkOK, tt.cfgOK, tt.leftovers, got, tt.want)
		}
	}
}

func TestDoctorMissingMkvmerge(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	code, out, errOut := runCLI("doctor")
	if code != 4 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"✓ config:", "not found; using defaults", "✗ mkvmerge:", "ISO mounting", "✓ no leftover mounts"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorBadConfig(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	writeUserConfig(t, `outptu_dir = "x"`)
	_, out, _ := runCLI("doctor")
	if line := lineWith(out, "config:"); !strings.HasPrefix(line, "✗") || !strings.Contains(line, "outptu_dir") {
		t.Errorf("config line = %q", line)
	}
}

func TestDoctorReportsLeftovers(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "zenvik", "mounts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dead := exec.Command(os.Args[0], "-test.run=^$")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{"image": "/i/x.iso", "dir": "/tmp/zenvik-mount-x", "device": "/dev/loop9",
		"pid": dead.Process.Pid, "created": time.Unix(1700000000, 0).UTC()}
	b, _ := json.Marshal(rec)
	p := filepath.Join(dir, "leftover.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	_, out, _ := runCLI("doctor")
	line := lineWith(out, "leftover mount:")
	if !strings.HasPrefix(line, "✗") || !strings.Contains(line, "/i/x.iso at /tmp/zenvik-mount-x") {
		t.Errorf("leftover line = %q\n%s", line, out)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if !strings.Contains(line, "remove with: ") {
			t.Errorf("no cleanup command in %q", line)
		}
	}
}

func TestDoctorArgs(t *testing.T) {
	if code, _, _ := runCLI("doctor", "extra"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}
```

`cmd/zenvik/doctor_integration_test.go`:
```go
//go:build integration

package main

import (
	"strings"
	"testing"
)

func TestDoctorHealthy(t *testing.T) {
	code, out, errOut := runCLI("doctor")
	if code != 0 || !strings.Contains(out, "✓ mkvmerge:") || !strings.Contains(out, "✓ no leftover mounts") {
		t.Errorf("exit %d, stderr %q, out:\n%s", code, errOut, out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/zenvik/ -run Doctor`
Expected: FAIL (`undefined: doctorCode`, unknown command "doctor").

- [ ] **Step 3: Implement**

`cmd/zenvik/doctor.go`:
```go
package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/naming"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check mkvmerge, ISO mounting, the config file and leftover mounts",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("doctor takes no arguments")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// doctorError carries doctor's exit code after the report is printed.
type doctorError struct{ code int }

func (e doctorError) Error() string { return "doctor found problems (see above)" }

// doctorCode picks the exit code: 4 for mkvmerge problems, else 2 for an
// invalid config, else 1 for leftover mounts, else 0.
func doctorCode(mkvmergeOK, configOK bool, leftovers int) int {
	switch {
	case !mkvmergeOK:
		return 4
	case !configOK:
		return 2
	case leftovers > 0:
		return 1
	}
	return 0
}

func runDoctor(ctx context.Context, out io.Writer) error {
	cfgMsg, mkvmergePath, cfgOK := checkConfig()
	mark := "✓"
	if !cfgOK {
		mark = "✗"
	}
	fmt.Fprintf(out, "%s %s\n", mark, cfgMsg)

	mk, err := mux.Find(ctx, mkvmergePath)
	mkOK := err == nil
	if mkOK {
		fmt.Fprintf(out, "✓ mkvmerge: %s (v%s)\n", mk.Path, mk.Version)
	} else {
		fmt.Fprintf(out, "✗ mkvmerge: %v\n", err)
	}

	if tool, err := mount.Available(); err != nil {
		fmt.Fprintf(out, "! ISO mounting: %v (rip from a folder instead)\n", err)
	} else {
		fmt.Fprintf(out, "✓ ISO mounting: %s\n", tool)
	}

	recs, err := mount.Leftovers()
	switch {
	case err != nil:
		fmt.Fprintf(out, "! leftover mounts: could not check: %v\n", err)
	case len(recs) == 0:
		fmt.Fprintln(out, "✓ no leftover mounts")
	default:
		for _, r := range recs {
			fmt.Fprintf(out, "✗ leftover mount: %s at %s (since %s); remove with: %s\n",
				r.Image, r.Dir, r.Created.Format(time.RFC3339), r.Cleanup())
		}
	}

	if code := doctorCode(mkOK, cfgOK, len(recs)); code != 0 {
		return doctorError{code: code}
	}
	return nil
}

// checkConfig describes the config file and returns the configured mkvmerge
// path; ok is false when the file is invalid.
func checkConfig() (msg, mkvmergePath string, ok bool) {
	path, err := config.DefaultPath()
	if err != nil {
		return fmt.Sprintf("config: %v", err), "", false
	}
	f, err := config.Load(path)
	if err != nil {
		return fmt.Sprintf("config: %v", err), "", false
	}
	s, err := config.Resolve(f, config.Flags{})
	if err == nil {
		if _, terr := naming.Parse(s.Template); terr != nil {
			err = fmt.Errorf("template %q: %w", s.Template, terr)
		}
	}
	if err != nil {
		return fmt.Sprintf("config: %s: %v", path, err), "", false
	}
	if !f.Found {
		return fmt.Sprintf("config: %s (not found; using defaults)", path), s.MkvmergePath, true
	}
	msg = "config: " + path
	if s.Preset != "" {
		msg += fmt.Sprintf(" (default preset %q)", s.Preset)
	}
	return msg, s.MkvmergePath, true
}
```

In `cmd/zenvik/main.go`:
- Register the command with `root.AddCommand(newDoctorCmd())`.
- At the top of `exitCode`, before the `usageError` case:
  ```go
  	var de doctorError
  	if errors.As(err, &de) {
  		return de.code
  	}
  ```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test -race ./cmd/zenvik/ && go test -tags integration -run TestDoctorHealthy -v ./cmd/zenvik/
go vet ./... && golangci-lint run && golangci-lint run --build-tags integration
```
Expected: PASS. On this Mac, `TestDoctorHealthy` shows `✓ mkvmerge` and `✓ ISO mounting: hdiutil`.

- [ ] **Step 5: Commit**

```bash
git add cmd/zenvik
git commit -m "Add zenvik doctor"
```

---

### Task 7: Documentation, spec sync and milestone verification

**Files:**
- Modify: `README.md`, `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: everything.
- Produces: user docs for config, templates, presets and doctor; a spec that matches the shipped API; a verified branch.

- [ ] **Step 1: Update the README**

1. Extend the Usage block:
````markdown
zenvik rip --name "Big Buck Bunny" --year 2008 <path>   # → ./Big Buck Bunny (2008).mkv
zenvik rip --preset plex <path>                          # use a config preset
zenvik doctor                                            # check mkvmerge, mounting, config, leftovers
````

2. Add a `## Configuration` section after Requirements:
````markdown
## Configuration

zenvik reads `$XDG_CONFIG_HOME/zenvik/config.toml` (default `~/.config/zenvik/config.toml`;
`%AppData%\zenvik\config.toml` on Windows). Every key is optional:

```toml
output_dir    = "~/Movies"               # default: current directory
template      = "{name}[ ({year})].mkv"  # default shown
min_duration  = "2m"                     # shorter titles are never the main feature
mkvmerge_path = ""                       # default: search PATH
preset        = ""                       # preset applied when --preset isn't given

[presets.plex]
output_dir = "/Volumes/Media/Movies"
template   = "{name}[ ({year})]/{name}[ ({year})].mkv"
```

Precedence: command-line flags, then the selected preset, then top-level values, then the
defaults. Presets may set `output_dir`, `template` and `min_duration`. Unknown keys, bad
durations and unknown presets are errors (exit 2), so typos don't go unnoticed.

### Name templates

| Variable | Value |
|---|---|
| `{name}` | `--name`, else the disc's title, else its volume label tidied up (`THE_MATRIX` → `The Matrix`) |
| `{year}` | `--year` |
| `{label}` | the raw volume label |
| `{playlist}` | the playlist number, e.g. `00800` |

`[...]` is dropped when a variable inside it is empty, so `{name}[ ({year})]` gives `Movie.mkv`
without a year. `/` in the template makes folders. Characters that are invalid on any OS are
replaced with `_`, names reserved on Windows (like `CON`) get a `_` suffix, and `.mkv` is added
if missing.
````

3. In "Cleaning up after a crash", add as the first line:
`Run `zenvik doctor` to list mounts a crashed rip left behind, with the command to remove each.`

- [ ] **Step 2: Sync the spec**

In `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, make these edits in place, keeping them minimal:
- §3: `func Open(ctx context.Context, path string, opts ...OpenOption) (*Disc, error)` (with `WithMinDuration`).
- §3: add `MkvmergePath string`, `DryRun bool` to `RipOptions` and `Command []string` to `RipResult`, if they're missing.
- §7: `--json` reports `"kind": "iso" | "bdmv"`.
- §6: replace "`zenvik doctor` lists any leftover zenvik mounts" with: "mounts are recorded in `$XDG_STATE_HOME/zenvik/mounts` (or the user cache dir) until detached, and `zenvik doctor` lists records whose process is gone, with the command to remove each".
- §8: note that `XDG_CONFIG_HOME` is honored on every OS, that unknown keys are errors, and that rendered names follow the reserved-name and 247-byte rules (Design Decision 4).

In `CLAUDE.md`, add one rule: `- Tests must not touch real user config/state: set XDG_CONFIG_HOME and XDG_STATE_HOME to temp dirs (cmd/zenvik has a TestMain for this).`

- [ ] **Step 3: Run the full verification suite**

```bash
test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
go vet ./... && GOOS=linux go vet ./... && GOOS=windows go vet ./... && GOOS=freebsd go vet ./...
golangci-lint run && golangci-lint run --build-tags integration
go test -race ./...
go test -tags integration ./...
CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
grep -A4 '^require' go.mod
hdiutil info | grep -c zenvik-mount || true
ls "$HOME/Library/Caches/zenvik" "$HOME/.config/zenvik" 2>/dev/null || echo "no real config/state touched"
```
Expected:
- `gofmt -l` prints nothing, and every command exits 0.
- `go.mod`'s direct requirements are exactly cobra and go-toml/v2.
- No zenvik mounts remain, and no real config or state directories were created.

- [ ] **Step 4: Smoke-test the binary**

```bash
go build -o /tmp/zenvik-m4 ./cmd/zenvik
XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d) /tmp/zenvik-m4 doctor; echo "exit $?"
/tmp/zenvik-m4 rip --help
rm -f /tmp/zenvik-m4
```
Expected: `doctor` prints four checks and exits 0 on this Mac (mkvmerge v102 is installed). `rip --help` lists `--name`, `--year`, `--template` and `--preset`.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md docs/superpowers/specs/2026-10-01-zenvik-v1-design.md
git commit -m "Document configuration, templates, presets and doctor; sync the spec"
```

- [ ] **Step 6: Review the branch**

Run: `git log --oneline origin/main..HEAD && git status`
Expected: the plan commit plus one commit per task (7), a clean tree, and nothing pushed.
