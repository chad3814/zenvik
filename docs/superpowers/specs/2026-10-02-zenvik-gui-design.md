# zenvik GUI — design

**Status:** approved in conversation on 2026-10-02 (sections 1–4). Written for review.
**Builds on:** `2026-10-01-zenvik-v1-design.md` (the library the GUI links) and M5/M6 (DVD support).

## 1. Purpose and scope

A desktop app for browsing discs and queueing rips, on the same four platforms zenvik ships for: macOS arm64, macOS amd64, Linux amd64 and Windows amd64.

- **Browse:** add ISOs and disc folders, see every title the way `zenvik info --all` lists them, and tick the titles to rip.
- **Queue:** each ticked title becomes a queue entry with its own output filename. The queue rips one entry at a time, shows progress, and survives restarts.

In v1:
- several titles per disc;
- a name you can edit for each title and each queue entry;
- a queue that persists, with history.

Not in v1 (§7):
- a settings screen, or choosing audio or subtitle tracks;
- numbering patterns for names;
- parallel rips;
- watching for inserted discs;
- code signing and notarization, which is the next milestone, using the user's Apple developer account.

## 2. Architecture

- **Shell:** Wails **v2.16.0**, the stable line. v3 is still in beta. The app is a Go backend plus a **React + TypeScript + Vite** frontend, running in the OS web view: WebKit on macOS, WebView2 on Windows, WebKitGTK 4.1 on Linux.
- **Module:** `gui/` is its own Go module, `github.com/chad3814/zenvik/gui`, with its own `go.mod`.
  - It requires `github.com/chad3814/zenvik`, with `replace github.com/chad3814/zenvik => ../` so it always builds against the code beside it, and `github.com/wailsapp/wails/v2 v2.16.0`.
  - zenvik's own `go.mod` keeps only `spf13/cobra` and `pelletier/go-toml/v2` and stays cgo-free. Only the GUI binary uses cgo.
  - The root `CLAUDE.md` rules ("No cgo", "Third-party dependencies are limited to…") apply to the root module. `gui/CLAUDE.md` states the GUI module's own rules.
- **Binary:** `zenvik-gui`. On macOS it is `Zenvik.app`.
- **Library, not CLI:** the GUI links the zenvik library. It never runs the `zenvik` CLI.
  - Discs and titles: `zenvik.Open` with the config's `min_duration` (the same as the CLI; `Open` still keeps filtered titles, so every title is listed), `Disc.Titles`, `Disc.Main`, `Disc.Title(id)`.
  - Rips: `Disc.Rip` with `RipOptions{OutputPath, MkvmergePath, OnProgress}`.
  - Default names: `zenvik.FormatName`.
  - Config: zenvik's `internal/config`. Go's `internal/` rule is by import path, so `github.com/chad3814/zenvik/gui/...` may import `github.com/chad3814/zenvik/internal/...`.
  - The mkvmerge check: `internal/mux.Find(ctx, mkvmergePath)`, the same lookup and `MinVersion` check `doctor` and `Rip` use. It returns `ErrMkvmergeNotFound` or `ErrMkvmergeTooOld`.
  - The GUI uses the same config file as the CLI, so `output_dir`, `template`, the default preset and `mkvmerge_path` match.
- **Layers inside `gui/`:**
  1. `internal/queue`: plain Go with no Wails imports. It holds entries, runs one at a time, and persists. It rips through a small `Ripper` interface, so tests can supply a fake.
  2. `internal/discs`: the open disc list. It runs `Open` in the background and builds plain JSON summaries.
  3. `app.go`: the methods bound to Wails, and the events. It is thin glue over 1 and 2, and emits events through a small `Emitter` interface so it can be tested without Wails.
  4. `frontend/`: React + TypeScript. It calls the bindings Wails generates and renders the snapshots it receives as events. It keeps no state of its own beyond what is being edited.

## 3. Screens (layout "A": two panes)

```
┌ zenvik — 2 discs, 1 queued ──────────────────────────────────────────────┐
│ Discs                                   │ Queue                          │
│ ▸ Swiss Family Robinson (1960) (USA).iso│ ▶ Swiss Family Robinson.mkv    │
│ ▸ Firefly_D1 (VIDEO_TS) · DVD   ◀ sel   │   muxing [██████░░░░] 61%      │
│ ─────────────────────────────────────── │   ETA 2m40s   [Cancel]         │
│ Titles — Firefly_D1   folder ~/Movies [Change…]                          │
│ ☑ 01 · 0:42:10 · 8 ch                   │ · Firefly – Serenity.mkv       │
│     [Firefly – Serenity.mkv          ]  │                                │
│ ☑ 02 · 0:42:58 · 7 ch                   │ [Pause queue]                  │
│     [Firefly – The Train Job.mkv     ]  │                                │
│ ☐ 04 · 0:03:11 · 1 ch                   │                                │
│ ⊘ 05 · interleaved angle block (not supported yet)                       │
│ [Add 2 titles to queue]                 │                                │
│ ┄ Drop ISOs or disc folders here · [Add ISO…] [Add folder…] ┄            │
└──────────────────────────────────────────────────────────────────────────┘
```

### 3.1 Discs pane

- **Adding.** Drop ISOs, `BDMV`/`VIDEO_TS` folders, or a disc's parent folder onto the window. Alternatively, two buttons open native dialogs: **Add ISO…** (files) and **Add folder…** (folders). Wails v2 has no dialog that accepts both.
- **Opening.**
  - Each disc opens in the background with `zenvik.Open(ctx, path, zenvik.WithMinDuration(<config min_duration>))`, the same as the CLI. `Open` keeps filtered titles, so every title is still listed, and the main-title pick matches the CLI's.
  - The row shows a spinner, then the format: `DVD` or `Blu-ray`.
  - A disc that fails to open stays in the list in red, showing zenvik's error text (for example `encrypted (AACS)`).
- **Duplicates and removal.**
  - Adding a path already in the list selects the existing row.
  - **Remove** closes the disc (`Disc.Close`). It doesn't affect queue entries.
- **Titles of the selected disc.** These are listed in zenvik's rank order. Each row shows:
  - the title ID (a DVD number or Blu-ray playlist), duration, chapter count and size;
  - a `main` marker on `Disc.Main()`;
  - a warning icon, with skipped-cell notes, when `SkippedCells` is non-empty.
- **Unsupported titles** are greyed out and can't be ticked. Their unsupported reason is shown inline, for example `interleaved angle block (not supported yet)`.
- **Default ticks.**
  - When the disc has an unambiguous main title, it is ticked by default.
  - When the ranking is ambiguous (`Disc.Main().Rank.Ambiguous`), nothing is pre-ticked and a hint reads: *"No clear main title — tick the titles you want."*
- **Name fields.** Ticking a title adds a filename field under its row.
  - The field holds a relative path: `FormatName(settings.Template, disc, title, vars)`'s output, the same template and default preset the CLI uses, so the template may add subfolders. The template variable for the title is `{playlist}`.
  - Absolute paths and `..` are rejected. `.mkv` is added when the name has no extension or a different one.
  - When names from the same disc collide (because the template has no `{playlist}`), the later ones get ` (2)`, ` (3)`… before the extension.
  - The names are editable, and unticking keeps the typed name for that title.
- **Output folder** is set per disc.
  - The default is the config's `output_dir`. The CLI's default `.` (the current directory, meaningless for an app) becomes `~/Movies` on macOS when that folder exists, otherwise the home folder. A relative `output_dir` is taken relative to the home folder.
  - **Change…** opens a native folder dialog.
- **Add N titles to queue** appends one entry per ticked title. The disc's ticks then clear, and its names are kept.
  - The entry is rejected with an inline red field if its full output path equals another entry's that is waiting or running.
  - It is also rejected if the name is empty, is an absolute path, or contains `..`. When any item fails, nothing is added.

### 3.2 Queue pane

- **Entry states** and what each shows:

  | State | Shows |
  |---|---|
  | `waiting` | the output name |
  | `ripping` | phase, a bar, %, bytes, ETA, and **Cancel** |
  | `done` | duration, and **Show in Finder** / **Show in Explorer** / **Open folder** (Linux) |
  | `failed` | zenvik's message, and **Retry** |
  | `canceled` | **Retry** |

- **Editing waiting entries:** they can be renamed in place (the same validation as §3.1), reordered by drag-and-drop or with ↑/↓ buttons, or removed. Rename edits the file name only, never the folder.
- **Start/Pause queue.**
  - Pausing lets the current rip finish and starts nothing new.
  - The queue runs automatically whenever it isn't paused and an entry is waiting.
  - The paused state persists.
- **Cancel** cancels the running rip's context. zenvik stops mkvmerge and deletes `<output>.partial`. The entry becomes `canceled`.
- **Retry** moves a `failed` or `canceled` entry back to `waiting`, at the end of the queue.
- **Clear finished** removes `done`, `failed` and `canceled` entries.
- **Checks just before an entry runs:**
  - The output file must not exist: if it does, the entry fails with `<name> exists`.
  - Its folder is created if needed.
  - The disc must open and the title ID must still exist. Otherwise the entry fails with `title <id> not found on <disc>`, or with zenvik's open error.
- **Quitting while a rip runs** asks "Cancel the current rip and quit?". If the user confirms, the rip is canceled cleanly. Before the queue is saved, that entry is set back to `waiting`, so the rip restarts on the next launch.

### 3.3 Banners

- **mkvmerge.** If mkvmerge is missing or older than zenvik's minimum (the same check `doctor` runs), a banner shows the problem and the fix. The queue won't start until a recheck passes; the banner has a **Recheck** button.
- **Config.** If the config file is invalid, a banner shows zenvik's error, and the built-in defaults are used.

## 4. Backend

### 4.1 Disc list (`internal/discs`)

- The list holds a `*zenvik.Disc` per path, keyed by `filepath.Abs`.
- `Open` runs in a goroutine per disc, and the result is published as a `discs:changed` event carrying the whole list.
- The summary sent to the frontend is plain JSON. Go pointers never cross the bridge:

```ts
interface DiscSummary {
  path: string; state: 'opening' | 'ready' | 'error'; error?: string;
  format?: 'DVD' | 'Blu-ray'; label?: string; name?: string;
  ambiguous?: boolean; outputDir: string;
  titles: TitleSummary[];
}
interface TitleSummary {
  id: string; durationSeconds: number; chapters: number; sizeBytes: number;
  main: boolean; rippable: boolean; reason?: string; skippedCells: string[];
  defaultName: string;
}
```

(The Go structs mirror these; Wails generates the TypeScript models from them.)

### 4.2 Queue (`internal/queue`)

- **Entry:**

```go
type Entry struct {
    ID         string // random, stable across restarts
    DiscPath   string
    TitleID    string
    OutputPath string // absolute
    State      State  // waiting | ripping | done | failed | canceled
    Message    string
    AddedAt, StartedAt, EndedAt time.Time
}
```

- **Live progress** (phase, fraction, bytes) is held in memory beside the entry and isn't saved.
- **Runner:** one goroutine.
  1. It takes the first `waiting` entry, if the queue isn't paused and the mkvmerge check passed.
  2. It runs the pre-run checks (§3.2).
  3. It always re-opens the disc with a fresh `Open`, closed after the rip, instead of reusing the window's open `*Disc`. This removes any sharing between the disc list and a running rip, at the cost of about a second per rip.
  4. It looks up `Disc.Title(TitleID)` and calls `Rip` with a cancelable context.
  - Discs are only ripped one at a time, which satisfies "A Disc must not be ripped from concurrently".
  - `Ripper` is the interface around steps 3–4, so tests can swap it.
- **Persistence:** `<state dir>/zenvik/gui-queue.json`, holding entries and the paused flag.
  - The state dir is `$XDG_STATE_HOME`, or else `os.UserCacheDir()`. These are the same roots as `internal/mount`.
  - The file is written after every state change: write to a temp file in the same directory, then rename it into place.
  - On load, `ripping` entries become `waiting`.
  - An unreadable or corrupt file is renamed to `gui-queue.json.bad-<unix time>`, and the app starts with an empty queue and a banner.

### 4.3 Events and bindings (`app.go`)

**Bound methods** take and return only primitives, strings slices and `error`; Wails generates the TypeScript for them:
- `AddPaths(paths []string)`, `PickISO()` and `PickFolder()` (native dialogs), `RemoveDisc(path)`;
- `SetOutputDir(path, dir)`, `PickOutputDir(path)`;
- `Enqueue(path string, titleIDs []string, names []string) []string`, which returns nil on success, or one message per item (an empty string where that item was fine). When any item fails, nothing is added;
- `Rename(id, name) string`, `Move(id string, index int)`, `Remove(id)`;
- `Cancel(id)`, `Retry(id)`, `ClearFinished()`, `SetPaused(bool)`;
- `RecheckMkvmerge()`, `Reveal(id)`, `Version() string`;
- `Ready()`, called once the page has loaded; it re-emits every event below.

**Events** are full snapshots:
- `discs:changed` with the whole `DiscSummary[]`;
- `discs:select` with a path string, telling the window which disc to select after an add;
- `queue:changed` with the whole `QueueSnapshot`;
- `queue:progress` with a `Progress` (`{id, phase, fraction, bytesDone, bytesTotal}`), at most one per 100 ms per entry, though phase changes and 100% always cross;
- `banners:changed` with the whole `Banner[]`.

**Config** is loaded at startup, and again when the window regains focus.

### 4.4 Errors

- Messages are zenvik's own error text.
- Known sentinels get a short label in the row, with the full text in a tooltip: `ErrEncrypted` → "encrypted", `ErrNoSpace` → "not enough space", `ErrUnsupportedTitle` and `ErrUnsupportedSource` → "unsupported", `ErrOutputExists` → "exists", `ErrMkvmergeNotFound` and `ErrMkvmergeTooOld` → the mkvmerge banner.

## 5. Testing

- **`internal/queue` unit tests**, with a fake `Ripper`. The fake can report progress, succeed, fail, or block until canceled. The tests cover:
  - ordering, pause/resume, cancel, retry, move, remove and clear;
  - rejecting duplicate outputs;
  - the output-exists failure;
  - the title-not-found failure;
  - save/load, including `ripping` → `waiting` and the corrupt-file rename.
- **`internal/discs` tests.** These use `internal/testdisc` discs: a Blu-ray, the DVD episodes disc, an encrypted disc and an ambiguous disc. They cover summaries, default ticks, unsupported reasons, default names and collision suffixes.
- **Integration test** (tag `integration`): the real queue with the real ripper rips testdisc fixtures with mkvmerge. It checks the MKVs exist, and that a canceled rip leaves no `.partial`.
- **`app.go` tests** use a fake `Emitter`. They cover event payloads, the config-error fallback, and the mkvmerge banner.
- **Frontend tests** use Vitest + React Testing Library, with mocked bindings and events. They cover:
  - the titles list: default ticks, greyed-out unsupported rows, the name field appearing on tick, and the edited name surviving an untick;
  - adding to the queue, and showing field errors;
  - queue rendering for every state, with rename, reorder, cancel and retry;
  - banners.
- **Frontend code rules:** TypeScript `strict`, ESLint with `@typescript-eslint/no-explicit-any` as an error.
- **There is no automated end-to-end UI test.** Wails has no reliable driver. The plan includes a manual checklist on macOS instead:
  - drag-and-drop the Swiss Family Robinson ISO and a testdisc Blu-ray;
  - rip, cancel, and quit during a rip;
  - relaunch and check the queue was restored;
  - delete the MKVs afterwards. The ISO is never copied, committed or uploaded.

## 6. Build, CI and release

- **Toolchain.**
  - The Wails CLI is pinned and run as `go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`. No global install is needed.
  - The frontend is installed with `npm ci` from a committed `package-lock.json`.
  - The node version is pinned in `gui/frontend/.nvmrc`.
- **Targets:**

  | Target | Builds on | Notes |
  |---|---|---|
  | `darwin/arm64` | `macos-latest` (Apple Silicon) | separate `.app` |
  | `darwin/amd64` | `macos-latest` | separate `.app`; clang cross-compiles; kept separate so the Intel build can be dropped on its own |
  | `windows/amd64` | `windows-latest` | WebView2, already part of Windows 10/11 |
  | `linux/amd64` | `ubuntu-latest` | `-tags webkit2_41`; installs `libgtk-3-dev libwebkit2gtk-4.1-dev` |

- **Version.** The GUI gets the same version string as the CLI through `-ldflags "-X main.version=<tag>"`, and shows it in an in-app About dialog.
- **CI.** A new `gui` job runs on `macos-latest`, `ubuntu-latest` and `windows-latest`. It runs `go vet` and `go test ./...` in `gui/`, `npm ci && npm run lint && npm test` in `gui/frontend`, then `wails build` to prove it compiles. The existing CLI jobs are unchanged.
- **Release.** The release workflow becomes per-OS build jobs, followed by one publish job.
  - The CLI archives are built as today.
  - The GUI artifacts are built natively on each runner:
    - `zenvik-gui_<ver>_darwin_arm64.zip` and `zenvik-gui_<ver>_darwin_amd64.zip`, each containing `Zenvik.app`;
    - `zenvik-gui_<ver>_windows_amd64.zip`;
    - `zenvik-gui_<ver>_linux_amd64.tar.gz`. Its README states that it needs WebKitGTK 4.1.
  - The publish job downloads every artifact, writes one combined `SHA256SUMS`, and creates the GitHub release with the same flags as today: `--generate-notes --verify-tag`, and `--prerelease` for tags containing `-`.
  - The macOS and Windows GUI builds are **unsigned** in v1.

## 7. Out of scope, and next

- **Next milestone:** macOS code signing and notarization, using the user's Apple developer account (Developer ID Application certificate, `notarytool`, stapling). It may later include Windows signing.
- **Later, if wanted:**
  - a settings screen;
  - choosing audio or subtitle tracks for each title;
  - a numbering pattern for names, e.g. `Firefly – S01E{n}`;
  - parallel rips across different discs;
  - Linux packaging (AppImage, .deb).
