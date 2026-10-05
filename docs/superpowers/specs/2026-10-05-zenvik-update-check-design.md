# zenvik update check — design

Status: approved in conversation 2026-10-05; this document records it.

## Goal

People who installed zenvik find out when a newer release exists, the way
`brew`, `gh` and `npm` tell them: a one-line notice after a CLI command, a
banner when the desktop app opens, and an explicit check in `zenvik doctor`
and the app's About dialog. The notice is quiet, cached, easy to turn off,
and never changes what a command does or returns.

## Decisions

| Question | Decision |
|---|---|
| Where it shows | Passive: CLI stderr after a command, GUI banner at launch. Explicit: `zenvik doctor`, About dialog |
| Source of truth | GitHub Releases API, `GET https://api.github.com/repos/chad3814/zenvik/releases/latest` (final releases only; pre-releases are never offered) |
| What the notice says | Version numbers and the releases page URL. No install-method hints |
| How often | At most one request per 24 hours per machine, shared by the CLI and the app through one cache file |
| Opt-out | Config `update_check = false`, or env `ZENVIK_NO_UPDATE_CHECK` set. Either disables the passive check; neither disables `doctor` or About |
| Development builds | Never check. Keeps `go test` offline and `dev` builds quiet |
| Shared code | One package, `internal/update`, in the root module, standard library only |
| Failures | Silent for passive checks, shown as text in `doctor` and About. Never affect exit codes |

Out of scope, each a possible later change:
- hints that depend on how zenvik was installed (`brew upgrade`, `scoop update`);
- a pre-release channel;
- downloading or installing the update;
- a dismiss control on the GUI banner;
- telemetry of any kind. The request carries only a `zenvik/<version>` user
  agent, which GitHub requires.

## Package `internal/update`

Root module, imports only the standard library (the root module's rules
allow `cobra` and `go-toml`, but this package needs neither). Both
`cmd/zenvik` and `gui/` import it.

### Versions

```go
// Version is a release tag: vMAJOR.MINOR.PATCH with an optional -PRERELEASE.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // "" for a final release; "rc2" for v1.1.0-rc2
}

var ErrBadVersion = errors.New("zenvik: not a release version")

func Parse(tag string) (Version, error)   // "v1.2.0", "v1.1.0-rc3"; anything else wraps ErrBadVersion
func (v Version) String() string          // "v1.2.0", "v1.1.0-rc3"
func (v Version) Less(o Version) bool     // semver precedence
```

`Less` orders by major, minor, patch; a pre-release sorts below the final
with the same numbers; two pre-releases compare identifier by identifier,
split on `.`, numeric identifiers numerically and before alphanumeric ones,
and a shorter list of equal identifiers sorts first. This is semver 2.0
precedence, which is what the existing tags (`v1.0.0-rc2`, `v1.1.0-rc1`
through `-rc3`, `v1.1.0`, `v1.1.1`, `v1.1.2`, `v1.2.0`) already follow.
Build metadata (`+…`) is rejected; no zenvik tag has carried it.

`Parse` accepts a leading `v` only. The CLI and the app both receive the tag
verbatim via `-X main.version=<tag>`, so a release build's version always
parses, and `dev`, `dev (abc123, modified)` and the GUI's default `dev` never
do.

### Checker

```go
// Checker finds the newest final release of zenvik.
type Checker struct {
	Client    *http.Client     // nil: http.Client with a 5 s timeout
	URL       string           // "": the GitHub releases/latest endpoint
	CachePath string           // "": DefaultCachePath()
	Now       func() time.Time // nil: time.Now
	UserAgent string           // "": "zenvik"
}

// Result is what a check learned.
type Result struct {
	Current   Version
	Latest    Version
	URL       string    // the release's page (html_url)
	Newer     bool      // Current.Less(Latest)
	CheckedAt time.Time // when Latest was fetched (zero when it never was)
}

var (
	ErrDevBuild = errors.New("zenvik: update check skipped in development builds")
	ErrFetch    = errors.New("zenvik: update check failed")
)

// Check returns the cached result when it is less than 24 hours old,
// otherwise fetches, records the attempt in the cache and returns.
func (c *Checker) Check(ctx context.Context, current string) (Result, error)

// Force always fetches, then records the attempt in the cache.
func (c *Checker) Force(ctx context.Context, current string) (Result, error)

// DefaultCachePath is $XDG_STATE_HOME/zenvik/update-check.json when
// XDG_STATE_HOME is absolute, else <os.UserCacheDir()>/zenvik/update-check.json
// (the roots the mount records and the GUI queue already use).
func DefaultCachePath() (string, error)
```

Behaviour:

- `current` that does not `Parse` returns `ErrDevBuild` before anything else
  is read or sent. This is the only error that bypasses the cache.
- The request is `GET` on `URL` with `Accept: application/vnd.github+json`,
  `X-GitHub-Api-Version: 2022-11-28` and `User-Agent: <UserAgent>`. Any
  status other than 200, a body that does not decode, or a `tag_name` that
  does not `Parse` is an error wrapping `ErrFetch` with the detail (status
  line, decode error, or the tag) in the message. Only `tag_name` and
  `html_url` are decoded; everything else in the response is ignored. The
  body read is capped at 1 MiB.
- `Check` reads the cache first. A cache whose `checked_at` is within 24
  hours of `Now()` answers without a request, whether or not that attempt
  succeeded: a failed attempt is remembered so a machine without network
  access does not retry every command. A missing, unreadable or malformed
  cache counts as absent. `Check` never fails because of the cache.
- After a fetch, `Check` and `Force` write the cache atomically (temp file
  in the same directory, then rename), creating the directory with mode
  0755 and the file with mode 0644. A write failure is ignored: the result
  is still returned, and the next run fetches again.
- Only a cancellation whose cause is a plain `context.Canceled` (Ctrl-C) is
  not recorded: it says nothing about the network. A cancellation whose cause
  wraps `context.DeadlineExceeded` is recorded as a failure like any timeout.
- A cached failure answers `Check` with the cached error (`ErrFetch` with
  the recorded detail) so callers treat it the same as a live failure.

### Cache file

`update-check.json`:

```json
{
  "checked_at": "2026-10-05T17:04:11Z",
  "latest": "v1.2.0",
  "url": "https://github.com/chad3814/zenvik/releases/tag/v1.2.0",
  "error": ""
}
```

`latest` and `url` are empty and `error` holds the detail when the attempt
failed. `Newer` is never cached; it is recomputed against the running
version, so a cache written by the CLI serves the app and the other way
round, and an upgrade is noticed on the next run without a new request.

## Config

`internal/config`:

- `File` gains a field `UpdateCheck *bool` with the TOML key `update_check`.
- `Settings` gains `UpdateCheck bool`, default `true`. Presets cannot set it;
  it is not a per-rip setting.
- `DefaultFileContent` gains, after `mkvmerge_path`:

```toml
# Tell me when a newer zenvik is released (checks GitHub at most once a day).
update_check = true
```

The environment variable `ZENVIK_NO_UPDATE_CHECK`, when set to anything
non-empty, disables the passive check in both binaries without touching the
config file. It exists for scripts, containers and people who want the
decision outside their dotfiles.

## CLI (`cmd/zenvik`)

### Passive notice

The root command gains a `PersistentPreRun` that starts a goroutine running
`Checker.Check` when every condition holds:

1. the build is a release (`version != ""`; `Check` rejects dev strings too,
   but the goroutine is not started at all);
2. the command is `info`, `rip` or `repair-udf` (`doctor` runs its own
   explicit check; `help`, `completion` and `--version` do not check);
3. `--jsonl` is not in effect (rip's machine-readable mode keeps stderr clean
   of unexpected lines);
4. neither `ZENVIK_NO_UPDATE_CHECK` nor `CI` is set to a non-empty value;
5. the config loads and `update_check` is true (a config error here is
   ignored: the command itself reports it, if it matters to that command);
6. stderr is a terminal (`os.Stderr.Stat()` reports `os.ModeCharDevice`).

The goroutine's context is the command's context with a 5 second timeout,
and its result is sent on a buffered channel of size one. `run` creates the
channel, `newRootCmd` receives it as a parameter, and `PersistentPreRun`
closes over it, so nothing is package-level and parallel tests do not share
state.

After the command returns, `run` in `main.go` waits up to 2 seconds for that
channel (nothing, when no check was started). On `Newer` it writes one line
to stderr, after any error message the command produced:

```
zenvik: v1.3.0 is available (you have v1.2.0): https://github.com/chad3814/zenvik/releases/latest
```

The URL is the fixed latest-release page rather than the cached `html_url`,
so the line is identical across runs and short enough for one terminal
width. Any error from `Check`, or a timeout, prints nothing. The exit code
is whatever the command produced.

Rationale for 2 seconds: with a daily cache the wait happens once a day, and
a fast command such as `info` otherwise exits before the first request
completes, so a cache would never be written on a machine that only runs
fast commands.

When the wait expires the CLI cancels the request with a cause wrapping
`context.DeadlineExceeded` and waits briefly for the attempt to be recorded,
so a hung network is remembered like any other failure instead of costing
every later fast command the full wait.

### `zenvik doctor`

After the leftover-mount lines, `doctor` calls `Checker.Force` with a 5 second
timeout and prints exactly one of:

```
✓ zenvik v1.2.0 is the latest release
! zenvik v1.3.0 is available: https://github.com/chad3814/zenvik/releases/latest
! update check: <error detail>
- update check: skipped in development builds
```

This never changes `doctorCode`. `doctor` is excluded from the passive
notice so an available update is reported once.

### Testing seams

The checker is reached through a package-level `newUpdateChecker func()
*update.Checker` that tests replace with one pointing at an `httptest`
server and a temporary cache path, plus `isTerminal func(*os.File) bool`
and the `version` variable, which tests set and restore. `run` keeps its
`stdout, stderr io.Writer` signature, so a test reads the notice from a
buffer.

## GUI (`gui/`)

### Dependencies

`Deps` gains:

```go
CheckUpdate func(ctx context.Context, current string) (update.Result, error) // passive, cached
ForceUpdate func(ctx context.Context, current string) (update.Result, error) // About dialog
```

`defaultDeps` sets both from one `update.Checker` whose `UserAgent` is
`zenvik-gui/<version>`. Tests inject fakes; the default `App` in tests never
reaches the network because `version` is `dev`.

### Banner at launch

In `startup`, after settings are loaded (the same place the config banner is
decided), when `version` parses, `Settings.UpdateCheck` is true and
`ZENVIK_NO_UPDATE_CHECK` is unset, a goroutine calls `CheckUpdate` with a 10
second timeout. On `Newer` it sets:

```go
Banner{ID: "update", Message: "Zenvik v1.3.0 is available.", Action: "download",
	URL: "https://github.com/chad3814/zenvik/releases/latest"}
```

Errors set no banner. `Banner` gains `URL string` (JSON `url`, omitted when empty);
the TypeScript `Banner` gains `url?: string` and `action?: 'recheck' |
'download'`. `Banners.tsx` renders a **Download** button for
`action === 'download'` that calls `api.openURL(b.url)`. The banner stays for
the session; it is not dismissable (out of scope above), and it disappears
for good once the user upgrades.

### About dialog

New bound method:

```go
// CheckForUpdate asks GitHub now and returns the newer release's tag, or ""
// when this build is the latest. Development builds and network problems
// return an error whose message is shown as is.
func (a *App) CheckForUpdate() (string, error)
```

It calls `ForceUpdate` with a 10 second timeout and `errs.Message` for the
error text. The About dialog gets a **Check for updates** link under the
version line. While waiting it reads "Checking…"; then one of "You have the
latest version", "v1.3.0 is available" followed by a link to the releases
page (via `api.openURL`), or the error text. Bindings are regenerated with
`wails generate module`.

## Testing

`internal/update`:
- `Parse`: accepted tags, rejected strings (`1.2.0`, `v1.2`, `dev`, `v1.2.0+1`,
  `v01.2.0`), round trip through `String`.
- `Less`: the ordering rules above, including `v1.1.0-rc3 < v1.1.0`,
  `v1.1.0-rc2 < v1.1.0-rc10`, `v1.1.0-alpha < v1.1.0-alpha.1`,
  `v1.1.0-alpha.1 < v1.1.0-beta`.
- `Check`/`Force` against an `httptest` server with `Now` injected: first
  call fetches and writes the cache; second call within 24 h does not hit
  the server; a call after 24 h does; `Force` always does; a 404, a 500,
  invalid JSON and a bad `tag_name` return `ErrFetch` and are cached as
  failures; a cached failure is returned without a request; a malformed cache
  file is treated as absent; a dev `current` returns `ErrDevBuild` with no
  request and no cache; the request headers are present; `Newer` is true,
  false and false for older, equal and newer running versions; an
  unwritable cache directory still returns the result.

`internal/config`: `update_check` true, false, absent (default true), and
non-boolean (error names the key); `DefaultFileContent` contains the key
and still loads with `update_check` true.

`cmd/zenvik`: with `version` set to a release tag and the checker pointed at
an `httptest` server: `info` prints the notice to stderr with exit code
unchanged; each skip condition (dev build, `--jsonl`, `CI`,
`ZENVIK_NO_UPDATE_CHECK`, `update_check = false`, non-terminal stderr,
`doctor`, `--version`) prints nothing and makes no request; a server that
never responds prints nothing and the command still returns within the 2 s
budget plus slack; `doctor` prints each of its four lines in the matching
situation and its exit code does not change. The existing `TestMain`
already points XDG dirs at temp directories, so the cache lands there.

`gui`: `app_test.go` cases where a fake `CheckUpdate` returns newer (banner
with `download` action and the URL), equal (no banner), an error (no
banner), `update_check = false` (fake never called), and env var set (fake
never called); `CheckForUpdate` returns the tag, `""`, or the error message.
Frontend: `Banners.test.tsx` renders the Download button and calls
`openURL` with the banner's URL; `About.test.tsx` (new) covers the three
outcomes of the check link.

CI is unchanged. No test reaches the network.

## Documentation

- `README.md`: a short "Update notifications" paragraph with the opt-outs.
- `CLAUDE.md` (root and `gui/`): one line each pointing at `internal/update`
  and the skip conditions.
- `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md` is not edited; this
  document stands alone.
