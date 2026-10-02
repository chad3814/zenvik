# zenvik GUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Wails v2 desktop app, `zenvik-gui`, for browsing Blu-ray/DVD images and folders, ticking titles, naming each output file, and queueing rips that run one at a time through the zenvik library.

**Architecture:** `gui/` is a separate Go module (`github.com/chad3814/zenvik/gui`), so zenvik itself stays cgo-free and keeps its two dependencies. It has three plain-Go packages and Wails glue:
- `internal/errs`: error labels;
- `internal/discs`: the open disc list and title summaries;
- `internal/queue`: entries, persistence, the runner and the library ripper;
- `app.go`: the bound methods, the events and a `Shell` interface around the Wails runtime.

The React + TypeScript frontend renders the snapshots the backend pushes as events, and calls bound methods that take and return only primitives.

**Tech Stack:**
- Go (the root module's `go` version) and Wails v2.16.0 (cgo, OS web view).
- React 19, TypeScript 5 and Vite 7, from Wails' `react-ts` template.
- Vitest, React Testing Library and jsdom for frontend tests; ESLint 9 with typescript-eslint.
- GitHub Actions for CI and release.

**Spec:** `docs/superpowers/specs/2026-10-02-zenvik-gui-design.md`. Task 1 brings the spec in line with the Plan decisions below.

## Global Constraints

- **Root module unchanged:**
  - The root module (`github.com/chad3814/zenvik`) gets no new dependencies and no cgo.
  - Nothing outside `gui/` imports anything under `gui/`.
  - `go build ./...`, `go vet ./...` and `go test -race ./...` at the repo root must still pass, and they ignore `gui/` because it is its own module.
- **GUI module:**
  - `gui/go.mod`: `module github.com/chad3814/zenvik/gui`, with the same `go` directive as the root `go.mod` (`go 1.27`).
  - It requires `github.com/wailsapp/wails/v2 v2.16.0`, and `github.com/chad3814/zenvik v0.0.0` with `replace github.com/chad3814/zenvik => ../`.
  - The Wails CLI is always run as `go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 …` (written `$WAILS` below). It is never installed globally.
- **TypeScript:**
  - `strict` stays on.
  - Never use `any`. ESLint's `@typescript-eslint/no-explicit-any` is an error.
  - The generated `frontend/wailsjs/` is excluded from lint and never edited by hand.
- **Go conventions:**
  - Sentinel errors are wrapped with `%w`.
  - Go tests never touch real user config or state: `gui/main_test.go` has a `TestMain` that sets `XDG_CONFIG_HOME` and `XDG_STATE_HOME` to temp dirs.
- **Fixtures:**
  - Never commit disc data. Build fixtures with `internal/testdisc`.
  - Never copy, commit or upload `Swiss Family Robinson (1960) (USA).iso`, which sits in the project root outside the repo.
- **Event names**, exact:

  | Event | Payload |
  |---|---|
  | `discs:changed` | `DiscSummary[]` |
  | `discs:select` | path string |
  | `queue:changed` | `QueueSnapshot` |
  | `queue:progress` | `Progress` |
  | `banners:changed` | `Banner[]` |

- **State file:** `$XDG_STATE_HOME/zenvik/gui-queue.json` when `XDG_STATE_HOME` is absolute, else `<os.UserCacheDir()>/zenvik/gui-queue.json`.
  - Its format is `{"version":1,"paused":bool,"entries":[…]}`.
  - A corrupt file is renamed to `gui-queue.json.bad-<unix seconds>`.
- **Release artifact names:**
  - `zenvik-gui_<ver>_darwin_arm64.zip` and `zenvik-gui_<ver>_darwin_amd64.zip`, each holding `zenvik-gui_<ver>_darwin_<arch>/Zenvik.app`;
  - `zenvik-gui_<ver>_windows_amd64.zip`;
  - `zenvik-gui_<ver>_linux_amd64.tar.gz`, with `-tags webkit2_41`.
  - GUI builds are unsigned in this plan.
- **App name, binary and version:**
  - The app is named `Zenvik`, and the binary is `zenvik-gui`. On macOS that gives `Zenvik.app/Contents/MacOS/zenvik-gui`.
  - The version comes from `var version = "dev"` in `gui/main.go`, set with `-ldflags "-X main.version=<tag>"`.
- **Commits:** descriptive sentence subjects, as in the repo's history. Commits are signed; if signing fails, commit unsigned and say so.

## Plan decisions (refinements of the spec; Task 1 writes them into the spec)

1. **The ripper always re-opens the disc** for each entry, instead of reusing the window's open `*Disc`. This removes any sharing between the disc list and a running rip, at the cost of about a second per rip.
2. **Events are full snapshots.**
   - `discs:changed` carries the whole list, `queue:changed` the whole queue, and `banners:changed` the whole banner list.
   - `discs:select` tells the window which disc to select after an add.
   - `Ready()` re-emits all of them once the page has loaded.
3. **Two add buttons, "Add ISO…" and "Add folder…".** Wails v2 has no dialog that accepts both files and folders.
4. **A title's name field holds a relative path** (`FormatName`'s output, so the template may add subfolders).
   - Absolute paths and `..` are rejected.
   - `.mkv` is added when the name has no extension or a different one.
   - **Rename** in the queue edits the file name only, never the folder.
5. **The default output folder** is the config's `output_dir`.
   - The CLI's default `.` (the current directory, meaningless for an app) becomes `~/Movies` on macOS when that folder exists, otherwise the home folder.
   - A relative `output_dir` is taken relative to the home folder.
6. **Discs open with the config's `min_duration`**, the same as the CLI, instead of `WithMinDuration(0)`. Every title is still listed (`Open` keeps filtered titles), and the main-title pick matches the CLI's.
7. **Bound methods take and return only primitives.**
   - `Enqueue(path string, titleIDs []string, names []string) []string` returns nil on success, or one message per item (an empty string where that item was fine).
   - When any item fails, nothing is added.
8. **Reordering** uses drag-and-drop and also ↑/↓ buttons.
9. **About** is an in-app dialog showing the version.
10. **The template variable is `{playlist}`** (the spec's `{title}` was wrong). Name collisions within a disc get ` (2)`, ` (3)`… before the extension.

## Review Focus

1. **A disc that's gone or moved by the time its entry runs** (an unplugged drive, a renamed folder). The entry fails with zenvik's open error, and the queue moves on. Covered by `TestLibRipperMissingDisc` (Task 5) and `TestRunnerFailureContinues` (Task 4).
2. **A quit or crash during a rip.** The entry comes back as `waiting`, and a leftover `<output>.partial` doesn't count as "exists". Covered by `TestOpenRestoresRippingAsWaiting` (Task 3) and `TestRunnerIgnoresPartialFile` (Task 4).
3. **The same title or file name queued twice.** The second is rejected when added. Queuing the same path again after the first one finished fails at run time with `<name> exists`. Covered by `TestAddRejectsDuplicateOutputs` (Task 3) and `TestRunnerOutputExists` (Task 4).
4. **Dropped paths with spaces, parentheses or non-ASCII characters** (`Swiss Family Robinson (1960) (USA).iso`). They open and are named intact. Covered by `TestAddPathsWithUnusualNames` (Task 6).
5. **A flood of progress events.** At most one per 100 ms per entry crosses the bridge, but phase changes and 100% always do. Covered by `TestProgressThrottle` (Task 4). In addition, an edited name survives unticking and a config reload: `keeps an edited name when unticked and re-ticked` (Task 7).

## Execution waves

- **A:** T1.
- **B:** T2 ∥ T3 (different packages).
- **C:** T4 ∥ T5 (`runner.go` vs `libripper.go`; both only add files to `internal/queue`).
- **D:** T6.
- **E:** T7 ∥ T9.
- **F:** T8.
- **G:** T10, run with the user.

## File structure

```
gui/
  go.mod, go.sum, wails.json, .gitignore, CLAUDE.md, README.md
  main.go            wails.Run wiring, version var, embed
  app.go             App: bound methods, banners, startup/beforeClose/shutdown
  shell.go           Shell interface + wailsShell (runtime events/dialogs)
  settings.go        loadSettings, stateFile, findMkvmerge, defaultDeps
  names.go           outputPath, withMKV, fileNameError
  reveal.go          revealArgs, reveal
  main_test.go, app_test.go, names_test.go, reveal_test.go
  internal/errs/     errs.go (+test): Message, Label
  internal/discs/    summary.go (Titles, DefaultOutputDir), discs.go (List) (+tests)
  internal/queue/    queue.go (types, Open, mutations), store.go (load/save),
                     runner.go (Start/Stop/Cancel/progress), libripper.go (+tests,
                     libripper_integration_test.go)
  build/             Wails scaffold (icons, Info.plist, windows manifest)
  frontend/
    package.json, package-lock.json, .nvmrc, vite.config.ts, eslint.config.js,
    tsconfig.json, tsconfig.node.json, index.html
    wailsjs/         generated bindings (committed, regenerated by $WAILS generate module)
    src/main.tsx, App.tsx, style.css, types.ts, api.ts, store.ts, picks.ts, format.ts
    src/components/  DiscList.tsx, TitlesPanel.tsx, QueuePane.tsx, Banners.tsx, About.tsx
    src/__tests__/   setup.ts and *.test.ts(x)
scripts/release-gui.sh
.github/workflows/ci.yml (gui job), release.yml (cli + gui matrix + publish)
```

---

### Task 1: Scaffold the `gui/` module and frontend tooling

**Files:**
- Create: `gui/` from the Wails `react-ts` template, then edit `gui/go.mod`, `gui/wails.json`, `gui/.gitignore`, `gui/main.go`, `gui/app.go`, `gui/frontend/src/App.tsx`, `gui/frontend/src/style.css`, `gui/frontend/package.json`, `gui/frontend/vite.config.ts`
- Create: `gui/CLAUDE.md`, `gui/README.md`, `gui/main_test.go`, `gui/version_test.go`, `gui/frontend/.nvmrc`, `gui/frontend/eslint.config.js`, `gui/frontend/src/format.ts`, `gui/frontend/src/__tests__/setup.ts`, `gui/frontend/src/__tests__/format.test.ts`, `gui/frontend/dist/gitkeep`
- Delete: `gui/frontend/src/App.css`, `gui/frontend/src/assets/` (fonts and logo), and the scaffold `gui/README.md` contents
- Modify: `.gitignore` (root), `docs/superpowers/specs/2026-10-02-zenvik-gui-design.md`

**Interfaces:**
- Produces:
  - Go: package `main` in `gui/` with `var version = "dev"`, `type App struct`, `NewApp`, `(*App).startup(ctx)`, `(*App).Version() string`. Task 6 rewrites `App`.
  - TS: `src/format.ts` exports `formatClock(seconds: number): string`, `formatSpan(seconds: number): string`, `formatBytes(bytes: number): string`, `clamp01(f: number): number`, `eta(fraction: number, phaseStartedAt: number, now: number): string | null`, `basename(p: string): string`.
  - Commands: `npm test` (vitest run), `npm run lint` (eslint), `npm run build` (tsc + vite build).

- [ ] **Step 1: Generate the scaffold**

```bash
cd /Users/chad/Projects/zenvik/worktrees/gui-design
WAILS="go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0"
$WAILS init -n zenvik-gui -t react-ts -d gui -g=false
rm -rf gui/frontend/src/assets gui/frontend/src/App.css
mkdir -p gui/frontend/dist && touch gui/frontend/dist/gitkeep
```

Expected: `Initialised project 'zenvik-gui'`. `gui/` now holds `main.go`, `app.go`, `wails.json`, `build/`, `frontend/` and `go.mod`.

- [ ] **Step 2: Point the module at the repo and pin its config**

Replace `gui/go.mod`'s first lines so that it starts with the following. Keep the `require` block Wails generated:

```
module github.com/chad3814/zenvik/gui

go 1.27
```

Replace `gui/wails.json`:

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "Zenvik",
  "outputfilename": "zenvik-gui",
  "frontend:install": "npm ci",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  "frontend:dev:serverUrl": "auto",
  "author": {
    "name": "Chad Walker",
    "email": "chad@cwalker.dev"
  }
}
```

Replace `gui/.gitignore`:

```
build/bin
node_modules
frontend/dist/*
!frontend/dist/gitkeep
```

Append to the root `.gitignore`:

```
.superpowers/
```

Create `gui/frontend/.nvmrc`:

```
24
```

- [ ] **Step 3: Replace the scaffold Go code with a minimal app**

`gui/main.go`:

```go
// Command zenvik-gui is the desktop front end for zenvik: browse disc images
// and folders, tick titles, and queue rips that run one at a time.
package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// version is set at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:       "Zenvik",
		Width:       1200,
		Height:      800,
		MinWidth:    900,
		MinHeight:   560,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   app.startup,
		Bind:        []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}
```

`gui/app.go`:

```go
package main

import "context"

// App is the object bound to the frontend.
type App struct {
	ctx context.Context
}

// NewApp returns an App; Wails calls startup once the window exists.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// Version is the build's version string.
func (a *App) Version() string { return version }
```

`gui/main_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points XDG config and state at temp dirs so no test touches the
// user's real zenvik config or saved queue.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zenvik-gui-test-")
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

`gui/version_test.go`:

```go
package main

import "testing"

func TestVersionDefaultsToDev(t *testing.T) {
	if got := NewApp().Version(); got != "dev" {
		t.Errorf("Version() = %q, want dev", got)
	}
}
```

- [ ] **Step 4: Run the Go test and the build**

```bash
cd gui && go mod tidy && go vet ./... && go test ./... && cd ..
```

Expected: `ok  github.com/chad3814/zenvik/gui`.

- [ ] **Step 5: Add frontend test and lint tooling**

```bash
cd gui/frontend
npm install
npm install -D vitest jsdom @testing-library/react @testing-library/dom @testing-library/user-event @testing-library/jest-dom eslint @eslint/js typescript-eslint eslint-plugin-react-hooks globals
```

Expected: `package-lock.json` is written and there are no peer-dependency errors. vitest 5 accepts Vite 7. If npm reports a peer conflict anyway, pin that package one major lower and record a ruling.

Edit `gui/frontend/package.json` `"scripts"` to:

```json
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "lint": "eslint ."
  },
```

`gui/frontend/vite.config.ts`:

```ts
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/__tests__/setup.ts'],
  },
});
```

`gui/frontend/eslint.config.js`:

```js
import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';

export default tseslint.config(
  { ignores: ['dist', 'wailsjs'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { globals: globals.browser },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
      '@typescript-eslint/no-explicit-any': 'error',
    },
  },
);
```

`gui/frontend/src/__tests__/setup.ts`:

```ts
import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';

afterEach(() => cleanup());
```

Replace `gui/frontend/src/App.tsx` (Task 8 writes the real one):

```tsx
export default function App() {
  return <div className="app">Zenvik</div>;
}
```

Replace `gui/frontend/src/style.css`:

```css
html, body, #root { height: 100%; margin: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif; }
```

Remove any `import './App.css'` or asset import left in `src/main.tsx`. The template's `main.tsx` imports only `./style.css` and `./App`.

- [ ] **Step 6: Write the failing formatter tests**

`gui/frontend/src/__tests__/format.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { basename, clamp01, eta, formatBytes, formatClock, formatSpan } from '../format';

describe('format', () => {
  it('formats clock durations', () => {
    expect(formatClock(7581)).toBe('2:06:21');
    expect(formatClock(59)).toBe('0:00:59');
    expect(formatClock(-4)).toBe('0:00:00');
  });
  it('formats spans', () => {
    expect(formatSpan(5)).toBe('5s');
    expect(formatSpan(252)).toBe('4m12s');
    expect(formatSpan(3725)).toBe('1h02m');
  });
  it('formats sizes', () => {
    expect(formatBytes(6.2e9)).toBe('6.2 GB');
    expect(formatBytes(700e6)).toBe('700 MB');
    expect(formatBytes(12_300)).toBe('12 kB');
  });
  it('clamps fractions', () => {
    expect(clamp01(1.5)).toBe(1);
    expect(clamp01(-1)).toBe(0);
    expect(clamp01(Number.NaN)).toBe(0);
  });
  it('estimates time left from the phase start', () => {
    expect(eta(0.01, 0, 10_000)).toBeNull();
    expect(eta(0.5, 0, 60_000)).toBe('1m00s');
  });
  it('takes base names of both path styles', () => {
    expect(basename('/a/b/Movie.mkv')).toBe('Movie.mkv');
    expect(basename('C:\\a\\Movie.mkv')).toBe('Movie.mkv');
  });
});
```

Run: `cd gui/frontend && npm test`
Expected: FAIL with `Failed to resolve import "../format"`.

- [ ] **Step 7: Implement the formatters**

`gui/frontend/src/format.ts`:

```ts
const two = (n: number): string => String(n).padStart(2, '0');

export function clamp01(f: number): number {
  return Number.isFinite(f) ? Math.min(1, Math.max(0, f)) : 0;
}

/** formatClock shows a title's length as h:mm:ss. */
export function formatClock(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  return `${Math.floor(s / 3600)}:${two(Math.floor((s % 3600) / 60))}:${two(s % 60)}`;
}

/** formatSpan shows an elapsed or remaining time compactly: 5s, 4m12s, 1h02m. */
export function formatSpan(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (h > 0) return `${h}h${two(m)}m`;
  if (m > 0) return `${m}m${two(s % 60)}s`;
  return `${s}s`;
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${Math.round(bytes / 1e6)} MB`;
  return `${Math.round(bytes / 1e3)} kB`;
}

/** eta estimates the time left in the current phase; null until 2% is done. */
export function eta(fraction: number, phaseStartedAt: number, now: number): string | null {
  const f = clamp01(fraction);
  if (f < 0.02) return null;
  const elapsed = (now - phaseStartedAt) / 1000;
  return formatSpan((elapsed * (1 - f)) / f);
}

export function basename(p: string): string {
  return p.slice(Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\')) + 1);
}
```

- [ ] **Step 8: Run tests, lint, generate bindings and build**

```bash
cd gui/frontend && npm test && npm run lint && cd ..
$WAILS generate module
$WAILS build -platform darwin/arm64 -clean
ls build/bin && cd ..
```

Expected:
- `npm test` passes 6 tests, and lint prints nothing.
- `frontend/wailsjs/go/main/App.d.ts` declares `Version():Promise<string>`.
- The build prints `Built '…/build/bin/Zenvik.app/Contents/MacOS/zenvik-gui'`.

If you're on Linux instead, use `-platform linux/amd64 -tags webkit2_41`.

- [ ] **Step 9: Write `gui/CLAUDE.md` and `gui/README.md`, and update the spec**

`gui/CLAUDE.md`:

```markdown
# zenvik GUI

Go module `github.com/chad3814/zenvik/gui` (Wails v2 + React/TypeScript). Design: `../docs/superpowers/specs/2026-10-02-zenvik-gui-design.md`.

## Commands (from `gui/`)

- Wails CLI (never install globally): `WAILS="go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0"`
- Dev window with hot reload: `$WAILS dev`
- Build: `$WAILS build` (Linux: add `-tags webkit2_41`; needs `libgtk-3-dev libwebkit2gtk-4.1-dev`)
- Regenerate frontend bindings after changing bound methods: `$WAILS generate module`
- Go tests: `go test ./...` (needs `frontend/dist` to contain at least `gitkeep`); integration: `go test -tags integration ./...` (needs mkvmerge and ffmpeg)
- Frontend (from `gui/frontend`): `npm ci`, `npm test`, `npm run lint`, `npm run build`

## Rules

- This module may use cgo and Wails; the root module may not, and must never import `gui/`.
- Bound App methods take and return only primitives, strings slices and `error`; data reaches the frontend as event snapshots (`discs:changed`, `discs:select`, `queue:changed`, `queue:progress`, `banners:changed`).
- `internal/discs`, `internal/queue` and `internal/errs` never import Wails.
- TypeScript: strict, never `any`; don't edit `frontend/wailsjs/` by hand.
- Tests must not touch real config/state (TestMain sets XDG dirs); disc fixtures come from the root module's `internal/testdisc`.
```

`gui/README.md`:

```markdown
# Zenvik (desktop app)

Zenvik is the desktop front end for [zenvik](https://github.com/chad3814/zenvik): add unencrypted Blu-ray and DVD images (`.iso`) or folders (`BDMV`, `VIDEO_TS`, or a folder containing one), tick the titles you want, name each file, and queue rips. Rips run one at a time with mkvmerge and the queue is kept between launches.

Requirements: [MKVToolNix](https://mkvtoolnix.download) (mkvmerge). On Linux, WebKitGTK 4.1 (`libwebkit2gtk-4.1-0` on Debian/Ubuntu).

Zenvik reads the same config file as the `zenvik` command (`zenvik doctor` shows where it is): `output_dir`, `template`, `preset`, `min_duration` and `mkvmerge_path` apply here too.

The macOS app is not signed yet: the first time, right-click `Zenvik.app` and choose Open (or run `xattr -dr com.apple.quarantine Zenvik.app`).
```

In the spec `docs/superpowers/specs/2026-10-02-zenvik-gui-design.md`, apply Plan decisions 1–10:
- §2 and §4.1 (the opener and its min duration);
- §3.1 (the two add buttons, relative names with subfolders, `{playlist}`, the default output folder);
- §3.2 (rename edits the file name; reorder by drag or ↑/↓);
- §4.2 (the ripper re-opens the disc);
- §4.3 (the method list and the event names from Global Constraints);
- §6 (the About dialog).

Keep the rest of the spec unchanged.

- [ ] **Step 10: Commit**

```bash
cd /Users/chad/Projects/zenvik/worktrees/gui-design
git add .gitignore gui docs/superpowers/specs/2026-10-02-zenvik-gui-design.md
git status --short | grep -v '^A ' ; git commit -m "Scaffold the zenvik GUI module (Wails v2, React/TypeScript) with frontend test and lint tooling"
```

Expected: `git status` shows nothing unstaged, and `gui/frontend/node_modules` and `gui/build/bin` are not staged.

---

### Task 2: Error labels and the disc list (`internal/errs`, `internal/discs`)

**Files:**
- Create: `gui/internal/errs/errs.go`, `gui/internal/errs/errs_test.go`
- Create: `gui/internal/discs/summary.go`, `gui/internal/discs/discs.go`, `gui/internal/discs/summary_test.go`, `gui/internal/discs/discs_test.go`
- Modify: `gui/go.mod` (adds `require github.com/chad3814/zenvik v0.0.0` and `replace github.com/chad3814/zenvik => ../`), `gui/go.sum`

**Interfaces:**
- Consumes:
  - `zenvik.Disc`: `Titles`, `Main()`, `Name()`, `Format.String()`, `Label`, `Close()`.
  - `zenvik.Title`: `ID`, `Duration`, `Chapters`, `Size`, `Encrypted`, `Unsupported`, `SkippedCells`, `Rank.IsMain`, `Rank.Ambiguous`.
  - `zenvik.FormatName`, `config.DefaultTemplate`.
- Produces:
  - `errs.Message(err error) string` and `errs.Label(err error) string`.
  - `discs.TitleSummary`, `discs.Summary` (JSON as in this task).
  - `discs.Titles(d *zenvik.Disc, template string) []TitleSummary`.
  - `discs.DefaultOutputDir(configured, goos, home string, exists func(string) bool) string`.
  - `type discs.Opener func(ctx context.Context, path string) (*zenvik.Disc, error)`.
  - `type discs.Config struct{ Template, OutputDir string }`.
  - `discs.New(ctx context.Context, open Opener, cfg Config, changed func([]Summary)) *List`.
  - `(*List).Add(paths []string) []string`, `Remove(path string)`, `SetOutputDir(path, dir string)`, `Configure(cfg Config)`, `Summaries() []Summary`, `Summary(path string) (Summary, bool)`, `Close()`.

- [ ] **Step 1: Write the failing `errs` tests**

`gui/internal/errs/errs_test.go`:

```go
package errs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chad3814/zenvik"
)

func TestLabel(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: title 00800", zenvik.ErrEncrypted), "encrypted"},
		{zenvik.ErrNoSpace, "not enough space"},
		{fmt.Errorf("x: %w", zenvik.ErrUnsupportedTitle), "unsupported"},
		{zenvik.ErrUnsupportedSource, "unsupported"},
		{zenvik.ErrOutputExists, "exists"},
		{zenvik.ErrMkvmergeNotFound, "mkvmerge not found"},
		{zenvik.ErrMkvmergeTooOld, "mkvmerge too old"},
		{zenvik.ErrMuxFailed, "mkvmerge failed"},
		{context.Canceled, "canceled"},
		{errors.New("boom"), "failed"},
	}
	for _, c := range cases {
		if got := Label(c.err); got != c.want {
			t.Errorf("Label(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestMessageDropsLibraryPrefix(t *testing.T) {
	if got := Message(zenvik.ErrNoSpace); got != "not enough free space" {
		t.Errorf("Message = %q", got)
	}
	if got := Message(errors.New("plain")); got != "plain" {
		t.Errorf("Message = %q", got)
	}
}
```

Run: `cd gui && go test ./internal/errs/`
Expected: FAIL, `undefined: Label`.

- [ ] **Step 2: Implement `errs`**

`gui/internal/errs/errs.go`:

```go
// Package errs turns zenvik errors into the short labels and messages the
// GUI shows.
package errs

import (
	"context"
	"errors"
	"strings"

	"github.com/chad3814/zenvik"
)

// Message is err's text without the "zenvik: " prefix the library adds.
func Message(err error) string {
	return strings.TrimPrefix(err.Error(), "zenvik: ")
}

// Label is a one- or two-word summary of err for a list row; the full
// Message goes in its tooltip.
func Label(err error) string {
	switch {
	case errors.Is(err, zenvik.ErrEncrypted):
		return "encrypted"
	case errors.Is(err, zenvik.ErrNoSpace):
		return "not enough space"
	case errors.Is(err, zenvik.ErrUnsupportedTitle), errors.Is(err, zenvik.ErrUnsupportedSource):
		return "unsupported"
	case errors.Is(err, zenvik.ErrOutputExists):
		return "exists"
	case errors.Is(err, zenvik.ErrMkvmergeNotFound):
		return "mkvmerge not found"
	case errors.Is(err, zenvik.ErrMkvmergeTooOld):
		return "mkvmerge too old"
	case errors.Is(err, zenvik.ErrMuxFailed):
		return "mkvmerge failed"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "failed"
}
```

```bash
cd gui && go mod edit -require=github.com/chad3814/zenvik@v0.0.0 -replace=github.com/chad3814/zenvik=../ && go mod tidy && go test ./internal/errs/
```

Expected: `ok`.

- [ ] **Step 3: Write the failing summary tests**

`gui/internal/discs/summary_test.go`:

```go
package discs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func writeBluray(t *testing.T, d *testdisc.Disc, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeDVD(t *testing.T, d *testdisc.DVD) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func open(t *testing.T, path string) *zenvik.Disc {
	t.Helper()
	d, err := zenvik.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestTitlesNamesAndOrder(t *testing.T) {
	d := open(t, writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE"))
	ts := Titles(d, config.DefaultTemplate)
	if len(ts) < 3 {
		t.Fatalf("titles = %+v", ts)
	}
	if ts[0].ID != "00800" || !ts[0].Main || !ts[0].Rippable {
		t.Errorf("first = %+v, want main 00800", ts[0])
	}
	if ts[0].DefaultName != "Sample Movie.mkv" || ts[1].DefaultName != "Sample Movie (2).mkv" || ts[2].DefaultName != "Sample Movie (3).mkv" {
		t.Errorf("names = %q %q %q", ts[0].DefaultName, ts[1].DefaultName, ts[2].DefaultName)
	}
	if ts[0].DurationSeconds != 6000 || ts[0].Chapters == 0 || ts[0].SizeBytes == 0 {
		t.Errorf("main facts = %+v", ts[0])
	}
}

func TestTitlesUsePlaylistInTemplate(t *testing.T) {
	d := open(t, writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE"))
	ts := Titles(d, "{name} - {playlist}.mkv")
	if ts[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name = %q", ts[0].DefaultName)
	}
}

func TestTitlesMarkEncryptedAndUnsupported(t *testing.T) {
	enc := testdisc.SampleMovie()
	enc.ClipData["00030"] = testdisc.ScrambledM2TS(1) // the trailer's clip
	d := open(t, writeBluray(t, enc, "PARTLY_ENCRYPTED"))
	var trailer TitleSummary
	for _, ts := range Titles(d, config.DefaultTemplate) {
		if ts.ID == "00010" {
			trailer = ts
		}
	}
	if trailer.Rippable || trailer.Reason != "encrypted" {
		t.Errorf("trailer = %+v", trailer)
	}

	dvd := open(t, writeDVD(t, testdisc.SampleDVD()))
	for _, ts := range Titles(dvd, config.DefaultTemplate) {
		if ts.ID == "08" && (ts.Rippable || ts.Reason == "") {
			t.Errorf("title 08 = %+v, want unsupported with a reason", ts)
		}
	}
}

func TestTitlesListSkippedCells(t *testing.T) {
	d := open(t, writeDVD(t, testdisc.StrayCellDVD()))
	for _, ts := range Titles(d, config.DefaultTemplate) {
		if ts.ID == "01" {
			if len(ts.SkippedCells) != 1 || ts.SkippedCells[0] == "" {
				t.Errorf("skipped = %q", ts.SkippedCells)
			}
			return
		}
	}
	t.Fatal("no title 01")
}

func TestDefaultOutputDir(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cases := []struct {
		configured, goos string
		exists           func(string) bool
		want             string
	}{
		{".", "darwin", yes, "/home/u/Movies"},
		{".", "darwin", no, "/home/u"},
		{".", "linux", yes, "/home/u"},
		{"", "windows", yes, "/home/u"},
		{"/srv/rips", "darwin", yes, "/srv/rips"},
		{"rips", "linux", yes, "/home/u/rips"},
	}
	for _, c := range cases {
		got := DefaultOutputDir(c.configured, c.goos, "/home/u", c.exists)
		if got != filepath.FromSlash(c.want) {
			t.Errorf("DefaultOutputDir(%q, %s) = %q, want %q", c.configured, c.goos, got, c.want)
		}
	}
}
```

Run: `cd gui && go test ./internal/discs/`
Expected: FAIL, `undefined: Titles`.

Notes:
- `"00030"` is the trailer clip, per the `SampleMovie` doc comment ("00010: one 2m30s segment on clip 00030").
- The main title is 100 minutes, which is the 6000 seconds asserted above.
- If the fixture's IDs differ when you run it, read `internal/testdisc/sample.go` and use its documented IDs. Don't weaken the assertions.

- [ ] **Step 4: Implement the summaries**

`gui/internal/discs/summary.go`:

```go
// Package discs keeps the GUI's list of open discs and turns each into the
// plain summary the frontend renders.
package discs

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
)

// TitleSummary is one title as the titles list shows it.
type TitleSummary struct {
	ID              string   `json:"id"`
	DurationSeconds float64  `json:"durationSeconds"`
	Chapters        int      `json:"chapters"`
	SizeBytes       int64    `json:"sizeBytes"`
	Main            bool     `json:"main"`
	Rippable        bool     `json:"rippable"`
	Reason          string   `json:"reason,omitempty"` // why it can't be ripped
	SkippedCells    []string `json:"skippedCells"`
	DefaultName     string   `json:"defaultName"` // relative path from the template
}

// Summary is one disc in the list.
type Summary struct {
	Path       string         `json:"path"`
	State      string         `json:"state"` // opening | ready | error
	Error      string         `json:"error,omitempty"`
	ErrorLabel string         `json:"errorLabel,omitempty"`
	Format     string         `json:"format,omitempty"`
	Label      string         `json:"label,omitempty"`
	Name       string         `json:"name,omitempty"`
	Ambiguous  bool           `json:"ambiguous"`
	OutputDir  string         `json:"outputDir"`
	Titles     []TitleSummary `json:"titles"`
}

// Titles summarizes d's titles in rank order. Each gets a default name from
// template; names that collide on this disc get " (2)", " (3)"… before the
// extension.
func Titles(d *zenvik.Disc, template string) []TitleSummary {
	used := map[string]bool{}
	out := make([]TitleSummary, 0, len(d.Titles))
	for _, t := range d.Titles {
		ts := TitleSummary{
			ID:              t.ID,
			DurationSeconds: t.Duration.Seconds(),
			Chapters:        len(t.Chapters),
			SizeBytes:       t.Size,
			Main:            t.Rank.IsMain,
			Rippable:        !t.Encrypted && t.Unsupported == "",
			SkippedCells:    make([]string, 0, len(t.SkippedCells)),
			DefaultName:     unique(defaultName(template, d, t), used),
		}
		switch {
		case t.Encrypted:
			ts.Reason = "encrypted"
		case t.Unsupported != "":
			ts.Reason = t.Unsupported
		}
		for _, c := range t.SkippedCells {
			ts.SkippedCells = append(ts.SkippedCells, fmt.Sprintf("skipped cell %d (%.1f s at sectors %d–%d)",
				c.Cell, c.Duration.Seconds(), c.FirstSector, c.LastSector))
		}
		out = append(out, ts)
	}
	return out
}

func defaultName(template string, d *zenvik.Disc, t *zenvik.Title) string {
	if name, err := zenvik.FormatName(template, d, t, zenvik.NameVars{}); err == nil {
		return name
	}
	if name, err := zenvik.FormatName(config.DefaultTemplate, d, t, zenvik.NameVars{}); err == nil {
		return name
	}
	return t.ID + ".mkv"
}

func unique(name string, used map[string]bool) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	cand := name
	for n := 2; used[cand]; n++ {
		cand = fmt.Sprintf("%s (%d)%s", stem, n, ext)
	}
	used[cand] = true
	return cand
}

// DefaultOutputDir is where a disc's MKVs go until the user picks a folder:
// the config's output_dir, except that the CLI's "." default (the current
// directory, meaningless for an app) becomes ~/Movies on macOS when it exists,
// else the home folder, and a relative output_dir is under the home folder.
func DefaultOutputDir(configured, goos, home string, exists func(string) bool) string {
	if configured != "" && configured != "." {
		if filepath.IsAbs(configured) {
			return configured
		}
		return filepath.Join(home, configured)
	}
	if goos == "darwin" {
		if m := filepath.Join(home, "Movies"); exists(m) {
			return m
		}
	}
	return filepath.FromSlash(home)
}
```

Run: `cd gui && go test ./internal/discs/`
Expected: PASS for the summary tests. `TestDefaultOutputDir` compares with `filepath.FromSlash`, so it also holds on Windows.

- [ ] **Step 5: Write the failing `List` tests**

`gui/internal/discs/discs_test.go`:

```go
package discs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type recorder struct {
	mu   sync.Mutex
	last []Summary
	n    int
}

func (r *recorder) changed(s []Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last, r.n = s, r.n+1
}

func (r *recorder) snapshot() []Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func opener(ctx context.Context, path string) (*zenvik.Disc, error) {
	return zenvik.Open(ctx, path)
}

func newList(t *testing.T, open Opener) (*List, *recorder) {
	t.Helper()
	r := &recorder{}
	l := New(context.Background(), open, Config{Template: config.DefaultTemplate, OutputDir: "/out"}, r.changed)
	t.Cleanup(l.Close)
	return l, r
}

func readyState(r *recorder, path string) bool {
	for _, s := range r.snapshot() {
		if s.Path == path && s.State != "opening" {
			return true
		}
	}
	return false
}

func TestListAddOpensInBackground(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	abs := l.Add([]string{dir + string(os.PathSeparator)})
	if len(abs) != 1 || abs[0] != dir {
		t.Fatalf("Add = %q, want [%q]", abs, dir)
	}
	if s := r.snapshot(); len(s) != 1 || s[0].State != "opening" {
		t.Fatalf("first event = %+v", s)
	}
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	s, _ := l.Summary(dir)
	if s.State != "ready" || s.Format != "Blu-ray" || s.Name != "Sample Movie" || s.OutputDir != "/out" || len(s.Titles) == 0 {
		t.Errorf("summary = %+v", s)
	}
}

func TestListAddExistingPathOnlyReturnsIt(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	if abs := l.Add([]string{dir}); len(abs) != 1 || abs[0] != dir {
		t.Errorf("Add again = %q", abs)
	}
	if n := len(l.Summaries()); n != 1 {
		t.Errorf("%d discs, want 1", n)
	}
}

func TestListOpenError(t *testing.T) {
	l, r := newList(t, opener)
	enc := testdisc.SampleMovie()
	for id := range enc.Clips {
		enc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	dir := writeBluray(t, enc, "ENCRYPTED")
	l.Add([]string{dir})
	waitFor(t, "error", func() bool { return readyState(r, dir) })
	s, _ := l.Summary(dir)
	if s.State != "error" || s.ErrorLabel != "encrypted" || s.Error == "" {
		t.Errorf("summary = %+v", s)
	}
}

func TestListAmbiguous(t *testing.T) {
	l, r := newList(t, opener)
	d := testdisc.SampleMovie()
	d.Titles, d.MovieObjects = nil, nil // no title-1 signal
	var segs []testdisc.Segment
	for i := 201; i <= 220; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 297 * time.Second})
	}
	d.Playlists["00802"] = testdisc.SimplePlaylist(segs...)
	d.AddClipsFor()
	dir := writeBluray(t, d, "AMBIGUOUS")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	if s, _ := l.Summary(dir); !s.Ambiguous {
		t.Errorf("summary = %+v, want ambiguous", s)
	}
}

func TestListRemoveWhileOpeningClosesLater(t *testing.T) {
	release := make(chan struct{})
	var closed sync.WaitGroup
	closed.Add(1)
	slow := func(ctx context.Context, path string) (*zenvik.Disc, error) {
		<-release
		defer closed.Done()
		return nil, errors.New("too late")
	}
	l, _ := newList(t, slow)
	abs := l.Add([]string{filepath.Join(t.TempDir(), "DISC")})
	l.Remove(abs[0])
	close(release)
	closed.Wait()
	time.Sleep(20 * time.Millisecond)
	if n := len(l.Summaries()); n != 0 {
		t.Errorf("%d discs after remove, want 0", n)
	}
}

func TestListOutputDirAndConfigure(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	other := writeBluray(t, testdisc.SampleMovie(), "OTHER")
	l.Add([]string{other})
	waitFor(t, "ready", func() bool { return readyState(r, other) })

	l.SetOutputDir(dir, "/picked")
	l.Configure(Config{Template: "{name} - {playlist}.mkv", OutputDir: "/new"})
	a, _ := l.Summary(dir)
	b, _ := l.Summary(other)
	if a.OutputDir != "/picked" || b.OutputDir != "/new" {
		t.Errorf("dirs = %q, %q", a.OutputDir, b.OutputDir)
	}
	if a.Titles[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name after Configure = %q", a.Titles[0].DefaultName)
	}
}
```

Add `"fmt"` to that file's imports (`TestListAmbiguous` uses it).

Run: `cd gui && go test ./internal/discs/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 6: Implement `List`**

`gui/internal/discs/discs.go`:

```go
package discs

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/errs"
)

// Opener opens the disc at path.
type Opener func(ctx context.Context, path string) (*zenvik.Disc, error)

// Config is what summaries depend on: the name template and the default
// output folder for discs whose folder the user hasn't picked.
type Config struct {
	Template  string
	OutputDir string
}

// List is the window's discs, in the order added. Each disc opens in its own
// goroutine; every change is reported to changed with a full snapshot.
type List struct {
	ctx     context.Context
	open    Opener
	changed func([]Summary)

	mu     sync.Mutex
	cfg    Config
	order  []string
	byPath map[string]*item
}

type item struct {
	disc      *zenvik.Disc
	summary   Summary
	customDir bool
}

// New returns an empty list. changed is called without the list's lock held.
func New(ctx context.Context, open Opener, cfg Config, changed func([]Summary)) *List {
	return &List{ctx: ctx, open: open, changed: changed, cfg: cfg, byPath: map[string]*item{}}
}

// Add starts opening each path that isn't listed yet and returns every
// path's absolute form, in order, so the caller can select the last one.
func (l *List) Add(paths []string) []string {
	var abs []string
	l.mu.Lock()
	for _, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		abs = append(abs, a)
		if _, ok := l.byPath[a]; ok {
			continue
		}
		l.byPath[a] = &item{summary: Summary{
			Path: a, State: "opening", Name: filepath.Base(a),
			OutputDir: l.cfg.OutputDir, Titles: []TitleSummary{},
		}}
		l.order = append(l.order, a)
		go l.load(a)
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
	return abs
}

func (l *List) load(path string) {
	d, err := l.open(l.ctx, path)
	l.mu.Lock()
	it, ok := l.byPath[path]
	if !ok { // removed while opening
		l.mu.Unlock()
		if d != nil {
			d.Close()
		}
		return
	}
	if err != nil {
		it.summary.State = "error"
		it.summary.Error = errs.Message(err)
		it.summary.ErrorLabel = errs.Label(err)
	} else {
		it.disc = d
		it.fill(l.cfg)
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
}

func (it *item) fill(cfg Config) {
	d := it.disc
	it.summary.State = "ready"
	it.summary.Format = d.Format.String()
	it.summary.Label = d.Label
	it.summary.Name = d.Name()
	m := d.Main()
	it.summary.Ambiguous = m != nil && m.Rank.Ambiguous
	if !it.customDir {
		it.summary.OutputDir = cfg.OutputDir
	}
	it.summary.Titles = Titles(d, cfg.Template)
}

// Remove closes and drops the disc at path; queue entries are unaffected.
func (l *List) Remove(path string) {
	l.mu.Lock()
	it, ok := l.byPath[path]
	if !ok {
		l.mu.Unlock()
		return
	}
	delete(l.byPath, path)
	for i, p := range l.order {
		if p == path {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	if it.disc != nil {
		it.disc.Close()
	}
	l.changed(s)
}

// SetOutputDir sets the folder for path's MKVs; Configure leaves it alone.
func (l *List) SetOutputDir(path, dir string) {
	l.mu.Lock()
	it, ok := l.byPath[path]
	if !ok {
		l.mu.Unlock()
		return
	}
	it.summary.OutputDir, it.customDir = dir, true
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
}

// Configure applies a new template and default output folder (after the
// config file changed), re-rendering every open disc's default names.
func (l *List) Configure(cfg Config) {
	l.mu.Lock()
	l.cfg = cfg
	for _, it := range l.byPath {
		switch {
		case it.disc != nil:
			it.fill(cfg)
		case !it.customDir:
			it.summary.OutputDir = cfg.OutputDir
		}
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
}

// Summaries returns every disc's summary in list order.
func (l *List) Summaries() []Summary {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshotLocked()
}

// Summary returns the summary of the disc at path.
func (l *List) Summary(path string) (Summary, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	it, ok := l.byPath[path]
	if !ok {
		return Summary{}, false
	}
	return it.summary, true
}

// Close closes every open disc.
func (l *List) Close() {
	l.mu.Lock()
	items := l.byPath
	l.byPath, l.order = map[string]*item{}, nil
	l.mu.Unlock()
	for _, it := range items {
		if it.disc != nil {
			it.disc.Close()
		}
	}
}

func (l *List) snapshotLocked() []Summary {
	out := make([]Summary, 0, len(l.order))
	for _, p := range l.order {
		out = append(out, l.byPath[p].summary)
	}
	return out
}
```

- [ ] **Step 7: Run the tests**

Run: `cd gui && go test -race ./internal/... && go vet ./...`
Expected: PASS for `errs` (2 tests) and `discs` (11 tests).

- [ ] **Step 8: Commit**

```bash
git add gui/go.mod gui/go.sum gui/internal/errs gui/internal/discs
git commit -m "Add the GUI's error labels and disc list with title summaries and default names"
```

---

### Task 3: Queue entries and persistence (`internal/queue`)

**Files:**
- Create: `gui/internal/queue/queue.go`, `gui/internal/queue/store.go`, `gui/internal/queue/queue_test.go`, `gui/internal/queue/store_test.go`

**Interfaces:**
- Consumes: nothing from Task 2, and the `zenvik` module only for `zenvik.Progress` in the `Ripper` signature.
- Produces:
  - `type State string`, with the constants `Waiting`, `Ripping`, `Done`, `Failed`, `Canceled`.
  - `Entry` (JSON: `id`, `discPath`, `titleId`, `outputPath`, `state`, `message`, `label`, `addedAt`, `startedAt`, `endedAt`).
  - `NewEntry{DiscPath, TitleID, OutputPath string}`.
  - `Snapshot{Entries []Entry; Paused, Ready bool; SaveError string}` (JSON `entries`, `paused`, `ready`, `saveError`).
  - `Progress{ID, Phase string; Fraction float64; BytesDone, BytesTotal int64}` (JSON `id`, `phase`, `fraction`, `bytesDone`, `bytesTotal`).
  - `type Ripper interface { Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error }`.
  - `Hooks{Changed func(Snapshot); Progress func(Progress)}`.
  - `Open(path string, r Ripper, h Hooks) (*Queue, error)`.
  - Methods: `Warning() string`, `Add([]NewEntry) []string`, `Rename(id, outputPath string) string`, `Move(id string, index int)`, `Remove(id string)`, `Retry(id string)`, `ClearFinished()`, `SetPaused(bool)`, `SetReady(bool)`, `Snapshot() Snapshot`.
  - Unexported, for Task 4: `update(func() bool)`, `indexLocked(id) int`, `poke()`, and the fields `wake chan struct{}`, `run *running`, `stopping bool`, `now func() time.Time`.

- [ ] **Step 1: Write the failing tests**

`gui/internal/queue/queue_test.go`:

```go
package queue

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
)

type nopRipper struct{}

func (nopRipper) Rip(context.Context, Entry, func(zenvik.Progress)) error { return nil }

type hookLog struct {
	mu    sync.Mutex
	snaps []Snapshot
}

func (h *hookLog) changed(s Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snaps = append(h.snaps, s)
}

func (h *hookLog) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.snaps)
}

func newQueue(t *testing.T) (*Queue, *hookLog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zenvik", "gui-queue.json")
	h := &hookLog{}
	q, err := Open(path, nopRipper{}, Hooks{Changed: h.changed})
	if err != nil {
		t.Fatal(err)
	}
	return q, h, path
}

func abs(name string) string { return filepath.Join(string(filepath.Separator)+"out", name) }

func ids(s Snapshot) []string {
	var out []string
	for _, e := range s.Entries {
		out = append(out, e.TitleID)
	}
	return out
}

func TestAddAppendsWaitingEntries(t *testing.T) {
	q, h, _ := newQueue(t)
	if msgs := q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}, {"/d", "02", abs("b.mkv")}}); msgs != nil {
		t.Fatalf("Add = %q", msgs)
	}
	s := q.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].State != Waiting || s.Entries[0].ID == "" || s.Entries[0].ID == s.Entries[1].ID {
		t.Fatalf("entries = %+v", s.Entries)
	}
	if s.Entries[0].AddedAt.IsZero() || h.count() != 1 {
		t.Errorf("addedAt %v, %d change events", s.Entries[0].AddedAt, h.count())
	}
}

func TestAddRejectsDuplicateOutputs(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	msgs := q.Add([]NewEntry{{"/d", "02", abs("b.mkv")}, {"/d", "03", abs("a.mkv")}, {"/e", "01", abs("b.mkv")}, {"/e", "02", "relative.mkv"}})
	want := []string{"", "another queue entry already writes this file", "another queue entry already writes this file", "the output path must be absolute"}
	if len(msgs) != len(want) {
		t.Fatalf("msgs = %q", msgs)
	}
	for i := range want {
		if msgs[i] != want[i] {
			t.Errorf("msgs[%d] = %q, want %q", i, msgs[i], want[i])
		}
	}
	if n := len(q.Snapshot().Entries); n != 1 {
		t.Errorf("%d entries after a rejected batch, want 1", n)
	}
}

func TestFinishedEntriesDontBlockTheirOutput(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	q.mu.Lock()
	q.entries[0].State = Done
	q.mu.Unlock()
	if msgs := q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}}); msgs != nil {
		t.Errorf("Add after done = %q", msgs)
	}
}

func TestRename(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}, {"/d", "02", abs("b.mkv")}})
	s := q.Snapshot()
	a, b := s.Entries[0].ID, s.Entries[1].ID
	if msg := q.Rename(a, abs("b.mkv")); msg != "another queue entry already writes this file" {
		t.Errorf("rename onto b = %q", msg)
	}
	if msg := q.Rename(a, abs("a.mkv")); msg != "" {
		t.Errorf("rename to itself = %q", msg)
	}
	if msg := q.Rename(b, abs("c.mkv")); msg != "" || q.Snapshot().Entries[1].OutputPath != abs("c.mkv") {
		t.Errorf("rename = %q, %+v", msg, q.Snapshot().Entries[1])
	}
	q.mu.Lock()
	q.entries[0].State = Done
	q.mu.Unlock()
	if msg := q.Rename(a, abs("z.mkv")); msg != "only waiting entries can be renamed" {
		t.Errorf("rename done = %q", msg)
	}
	if msg := q.Rename("nope", abs("z.mkv")); msg != "no such queue entry" {
		t.Errorf("rename missing = %q", msg)
	}
}

func TestMoveRemoveRetryClear(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("1.mkv")}, {"/d", "02", abs("2.mkv")}, {"/d", "03", abs("3.mkv")}})
	s := q.Snapshot()
	q.Move(s.Entries[2].ID, 0)
	if got := ids(q.Snapshot()); got[0] != "03" || got[1] != "01" || got[2] != "02" {
		t.Errorf("after move = %v", got)
	}
	q.Move(s.Entries[2].ID, 99)
	if got := ids(q.Snapshot()); got[2] != "03" {
		t.Errorf("after move to end = %v", got)
	}

	q.mu.Lock()
	q.entries[0].State, q.entries[0].Message = Failed, "boom"
	q.entries[1].State = Done
	q.mu.Unlock()
	q.Retry(s.Entries[0].ID) // "01", failed: back to waiting at the end
	got := q.Snapshot()
	last := got.Entries[len(got.Entries)-1]
	if last.TitleID != "01" || last.State != Waiting || last.Message != "" {
		t.Errorf("after retry = %+v", got.Entries)
	}
	q.ClearFinished()
	if got := ids(q.Snapshot()); len(got) != 2 {
		t.Errorf("after clear = %v", got)
	}
	q.Remove(q.Snapshot().Entries[0].ID)
	if n := len(q.Snapshot().Entries); n != 1 {
		t.Errorf("after remove: %d entries", n)
	}
}

func TestRemoveIgnoresRippingEntry(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("1.mkv")}})
	q.mu.Lock()
	q.entries[0].State = Ripping
	q.mu.Unlock()
	q.Remove(q.Snapshot().Entries[0].ID)
	if n := len(q.Snapshot().Entries); n != 1 {
		t.Errorf("ripping entry removed")
	}
}

func TestPausedAndReadyAreReported(t *testing.T) {
	q, _, _ := newQueue(t)
	q.SetPaused(true)
	q.SetReady(true)
	if s := q.Snapshot(); !s.Paused || !s.Ready {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestSnapshotEntriesNeverNil(t *testing.T) {
	q, _, _ := newQueue(t)
	if q.Snapshot().Entries == nil {
		t.Error("Entries is nil; the frontend expects []")
	}
}

var _ = time.Second
```

`gui/internal/queue/store_test.go`:

```go
package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndReopen(t *testing.T) {
	q, _, path := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	q.SetPaused(true)
	q2, err := Open(path, nopRipper{}, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	s := q2.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].OutputPath != abs("a.mkv") || !s.Paused || s.Ready {
		t.Errorf("reopened = %+v", s)
	}
	if q2.Warning() != "" {
		t.Errorf("warning = %q", q2.Warning())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".gui-queue-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left: %v", leftovers)
	}
}

func TestOpenRestoresRippingAsWaiting(t *testing.T) {
	q, _, path := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	q.mu.Lock()
	q.entries[0].State = Ripping
	now := q.now()
	q.entries[0].StartedAt = &now
	q.persistLocked()
	q.mu.Unlock()
	q2, err := Open(path, nopRipper{}, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	e := q2.Snapshot().Entries[0]
	if e.State != Waiting || e.StartedAt != nil {
		t.Errorf("restored = %+v", e)
	}
}

func TestOpenMissingFileIsEmpty(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "none.json"), nopRipper{}, Hooks{})
	if err != nil || len(q.Snapshot().Entries) != 0 {
		t.Errorf("q = %+v, err = %v", q, err)
	}
}

func TestOpenCorruptFileIsMovedAside(t *testing.T) {
	for name, content := range map[string]string{
		"not json":      "{nope",
		"wrong version": `{"version":7,"entries":[]}`,
		"bad entry":     `{"version":1,"entries":[{"id":"","state":"waiting","outputPath":"/a.mkv"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gui-queue.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			q, err := Open(path, nopRipper{}, Hooks{})
			if err != nil {
				t.Fatal(err)
			}
			if len(q.Snapshot().Entries) != 0 || !strings.Contains(q.Warning(), "gui-queue.json.bad-") {
				t.Errorf("entries %d, warning %q", len(q.Snapshot().Entries), q.Warning())
			}
			bad, _ := filepath.Glob(path + ".bad-*")
			if len(bad) != 1 {
				t.Errorf("moved-aside files = %v", bad)
			}
		})
	}
}

func TestSaveErrorIsReported(t *testing.T) {
	q, _, _ := newQueue(t)
	blocker := filepath.Join(t.TempDir(), "zenvik")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil { // a file where the folder should be
		t.Fatal(err)
	}
	q.path = filepath.Join(blocker, "gui-queue.json")
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	if q.Snapshot().SaveError == "" {
		t.Error("SaveError empty after a failed save")
	}
}
```

Run: `cd gui && go test ./internal/queue/`
Expected: FAIL, `undefined: Open`.

- [ ] **Step 2: Implement the store**

`gui/internal/queue/store.go`:

```go
package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type stored struct {
	Version int     `json:"version"`
	Paused  bool    `json:"paused"`
	Entries []Entry `json:"entries"`
}

var errCorrupt = errors.New("corrupt queue file")

func load(path string) (stored, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return stored{}, err
	}
	var st stored
	if err := json.Unmarshal(b, &st); err != nil {
		return stored{}, fmt.Errorf("%w: %v", errCorrupt, err)
	}
	if st.Version != 1 {
		return stored{}, fmt.Errorf("%w: version %d", errCorrupt, st.Version)
	}
	for _, e := range st.Entries {
		if e.ID == "" || !validState(e.State) || !filepath.IsAbs(e.OutputPath) {
			return stored{}, fmt.Errorf("%w: bad entry %q", errCorrupt, e.ID)
		}
	}
	return st, nil
}

// save writes st to a temp file beside path and renames it into place, so a
// crash never leaves a half-written queue.
func save(path string, st stored) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gui-queue-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func validState(s State) bool {
	switch s {
	case Waiting, Ripping, Done, Failed, Canceled:
		return true
	}
	return false
}
```

- [ ] **Step 3: Implement the queue model**

`gui/internal/queue/queue.go`:

```go
// Package queue holds the GUI's rip queue: entries that rip one at a time,
// saved to disk after every change so the queue survives restarts.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/chad3814/zenvik"
)

// State is where an entry is in its life.
type State string

const (
	Waiting  State = "waiting"
	Ripping  State = "ripping"
	Done     State = "done"
	Failed   State = "failed"
	Canceled State = "canceled"
)

// Entry is one title to rip to one file.
type Entry struct {
	ID         string     `json:"id"`
	DiscPath   string     `json:"discPath"`
	TitleID    string     `json:"titleId"`
	OutputPath string     `json:"outputPath"` // absolute
	State      State      `json:"state"`
	Message    string     `json:"message,omitempty"` // why it failed
	Label      string     `json:"label,omitempty"`   // short form of Message
	AddedAt    time.Time  `json:"addedAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
}

// NewEntry is what Add needs to queue a title.
type NewEntry struct {
	DiscPath, TitleID, OutputPath string
}

// Snapshot is the whole queue as the frontend renders it.
type Snapshot struct {
	Entries   []Entry `json:"entries"`
	Paused    bool    `json:"paused"`
	Ready     bool    `json:"ready"` // mkvmerge was found
	SaveError string  `json:"saveError,omitempty"`
}

// Progress is a running entry's progress, as reported to Hooks.Progress.
type Progress struct {
	ID         string  `json:"id"`
	Phase      string  `json:"phase"`
	Fraction   float64 `json:"fraction"`
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
}

// Ripper rips one entry, reporting progress; it must return promptly with
// ctx's error once ctx is canceled.
type Ripper interface {
	Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error
}

// Hooks are called without the queue's lock held.
type Hooks struct {
	Changed  func(Snapshot)
	Progress func(Progress)
}

// Queue is the rip queue. Its methods are safe for concurrent use.
type Queue struct {
	path   string
	ripper Ripper
	hooks  Hooks
	now    func() time.Time

	mu       sync.Mutex
	entries  []Entry
	paused   bool
	ready    bool
	warning  string
	saveErr  string
	run      *running // the entry being ripped; nil when idle
	stopping bool     // Stop was called: start nothing new

	wake chan struct{}
}

// Open loads the queue saved at path (a missing file is an empty queue). An
// unreadable file is moved aside to path.bad-<unix time> and reported by
// Warning; entries that were ripping when the app stopped are waiting again.
func Open(path string, r Ripper, h Hooks) (*Queue, error) {
	q := &Queue{path: path, ripper: r, hooks: h, now: time.Now, wake: make(chan struct{}, 1)}
	if q.hooks.Changed == nil {
		q.hooks.Changed = func(Snapshot) {}
	}
	if q.hooks.Progress == nil {
		q.hooks.Progress = func(Progress) {}
	}
	st, err := load(path)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
	case errors.Is(err, errCorrupt):
		bad := fmt.Sprintf("%s.bad-%d", path, q.now().Unix())
		if rerr := os.Rename(path, bad); rerr != nil {
			return nil, rerr
		}
		q.warning = fmt.Sprintf("The saved queue couldn't be read and was moved to %s.", filepath.Base(bad))
	default:
		return nil, err
	}
	for _, e := range st.Entries {
		if e.State == Ripping {
			e.State, e.StartedAt = Waiting, nil
		}
		q.entries = append(q.entries, e)
	}
	q.paused = st.Paused
	return q, nil
}

// Warning describes a problem found when the queue was opened, or "".
func (q *Queue) Warning() string { return q.warning }

const msgTaken = "another queue entry already writes this file"

// Add queues items at the end. If any item is invalid (a relative output
// path, or one another waiting or ripping entry already writes) nothing is
// added and the result has a message for each item ("" where it was fine);
// on success it is nil.
func (q *Queue) Add(items []NewEntry) []string {
	var msgs []string
	q.update(func() bool {
		msgs = make([]string, len(items))
		taken := q.activeOutputsLocked("")
		bad := false
		for i, it := range items {
			switch {
			case !filepath.IsAbs(it.OutputPath):
				msgs[i] = "the output path must be absolute"
			case taken[it.OutputPath]:
				msgs[i] = msgTaken
			default:
				taken[it.OutputPath] = true
				continue
			}
			bad = true
		}
		if bad {
			return false
		}
		for _, it := range items {
			q.entries = append(q.entries, Entry{
				ID: newID(), DiscPath: it.DiscPath, TitleID: it.TitleID,
				OutputPath: it.OutputPath, State: Waiting, AddedAt: q.now(),
			})
		}
		msgs = nil
		return true
	})
	return msgs
}

// Rename changes a waiting entry's output path; it returns "" or why not.
func (q *Queue) Rename(id, outputPath string) string {
	var msg string
	q.update(func() bool {
		i := q.indexLocked(id)
		switch {
		case i < 0:
			msg = "no such queue entry"
		case q.entries[i].State != Waiting:
			msg = "only waiting entries can be renamed"
		case !filepath.IsAbs(outputPath):
			msg = "the output path must be absolute"
		case q.activeOutputsLocked(id)[outputPath]:
			msg = msgTaken
		}
		if msg != "" || q.entries[i].OutputPath == outputPath {
			return false
		}
		q.entries[i].OutputPath = outputPath
		return true
	})
	return msg
}

// Move puts the entry at index (clamped to the list).
func (q *Queue) Move(id string, index int) {
	q.update(func() bool {
		i := q.indexLocked(id)
		if i < 0 {
			return false
		}
		index = max(0, min(index, len(q.entries)-1))
		if i == index {
			return false
		}
		e := q.entries[i]
		q.entries = slices.Delete(q.entries, i, i+1)
		q.entries = slices.Insert(q.entries, index, e)
		return true
	})
}

// Remove drops an entry that isn't ripping (cancel it first).
func (q *Queue) Remove(id string) {
	q.update(func() bool {
		i := q.indexLocked(id)
		if i < 0 || q.entries[i].State == Ripping {
			return false
		}
		q.entries = slices.Delete(q.entries, i, i+1)
		return true
	})
}

// Retry moves a failed or canceled entry to the end of the queue, waiting.
func (q *Queue) Retry(id string) {
	q.update(func() bool {
		i := q.indexLocked(id)
		if i < 0 || (q.entries[i].State != Failed && q.entries[i].State != Canceled) {
			return false
		}
		e := q.entries[i]
		e.State, e.Message, e.Label, e.StartedAt, e.EndedAt = Waiting, "", "", nil, nil
		q.entries = append(slices.Delete(q.entries, i, i+1), e)
		return true
	})
}

// ClearFinished drops done, failed and canceled entries.
func (q *Queue) ClearFinished() {
	q.update(func() bool {
		n := len(q.entries)
		q.entries = slices.DeleteFunc(q.entries, func(e Entry) bool {
			return e.State == Done || e.State == Failed || e.State == Canceled
		})
		return len(q.entries) != n
	})
}

// SetPaused stops (or resumes) starting new rips; a running rip finishes.
func (q *Queue) SetPaused(p bool) {
	q.update(func() bool {
		changed := q.paused != p
		q.paused = p
		return changed
	})
}

// SetReady records whether mkvmerge was found; nothing starts until it is.
func (q *Queue) SetReady(r bool) {
	q.update(func() bool {
		changed := q.ready != r
		q.ready = r
		return changed
	})
}

// Snapshot returns the whole queue.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.snapshotLocked()
}

// update runs f under the lock. When f reports a change, the queue is saved,
// Hooks.Changed gets a snapshot, and the runner is woken.
func (q *Queue) update(f func() bool) {
	q.mu.Lock()
	if !f() {
		q.mu.Unlock()
		return
	}
	q.persistLocked()
	s := q.snapshotLocked()
	q.mu.Unlock()
	q.hooks.Changed(s)
	q.poke()
}

func (q *Queue) persistLocked() {
	if err := save(q.path, stored{Version: 1, Paused: q.paused, Entries: q.entries}); err != nil {
		q.saveErr = "The queue couldn't be saved: " + err.Error()
	} else {
		q.saveErr = ""
	}
}

func (q *Queue) snapshotLocked() Snapshot {
	return Snapshot{
		Entries: append([]Entry{}, q.entries...),
		Paused:  q.paused, Ready: q.ready, SaveError: q.saveErr,
	}
}

func (q *Queue) indexLocked(id string) int {
	return slices.IndexFunc(q.entries, func(e Entry) bool { return e.ID == id })
}

func (q *Queue) activeOutputsLocked(except string) map[string]bool {
	m := map[string]bool{}
	for _, e := range q.entries {
		if e.ID != except && (e.State == Waiting || e.State == Ripping) {
			m[e.OutputPath] = true
		}
	}
	return m
}

func (q *Queue) poke() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// running is the entry being ripped; its flags are guarded by Queue.mu.
type running struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	canceled bool // Cancel: the entry ends canceled
	stopped  bool // Stop: the entry goes back to waiting
	done     chan struct{}
}
```

`TestRename`'s "rename to itself" case expects `""`. With `msg` empty and the path unchanged, `update` returns `false`, so no event fires. That is intended.

- [ ] **Step 4: Run the tests**

Run: `cd gui && go test -race ./internal/queue/ && go vet ./...`
Expected: PASS (13 tests).

- [ ] **Step 5: Commit**

```bash
git add gui/internal/queue
git commit -m "Add the GUI queue model with validation and crash-safe persistence"
```

---

### Task 4: The queue runner

**Files:**
- Create: `gui/internal/queue/runner.go`, `gui/internal/queue/runner_test.go`

**Interfaces:**
- Consumes (Task 3): the `Queue` fields `run`, `stopping`, `wake`, `now`, `ripper`, `hooks`; `update`, `indexLocked` and `poke`; `type running`; `Entry`, `Progress`, `State`. From Task 2: `errs.Message`, `errs.Label`.
- Produces: `(*Queue).Start(ctx context.Context)`, `Cancel(id string)`, `Stop(timeout time.Duration)`, `Running() bool`.

- [ ] **Step 1: Write the failing tests**

`gui/internal/queue/runner_test.go`:

```go
package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
)

// scriptRipper rips by title ID: block (until ctx is done or release is
// closed), fail, or emit progress; it writes the output on success.
type scriptRipper struct {
	mu       sync.Mutex
	order    []string
	active   atomic.Int32
	maxSeen  atomic.Int32
	release  chan struct{}
	failWith map[string]error
	emit     func(progress func(zenvik.Progress))
}

func (r *scriptRipper) Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error {
	n := r.active.Add(1)
	defer r.active.Add(-1)
	for {
		m := r.maxSeen.Load()
		if n <= m || r.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	r.mu.Lock()
	r.order = append(r.order, e.TitleID)
	r.mu.Unlock()
	if r.emit != nil {
		r.emit(progress)
	}
	if err := r.failWith[e.TitleID]; err != nil {
		return err
	}
	if r.release != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
		}
	}
	return os.WriteFile(e.OutputPath, []byte("mkv"), 0o644)
}

func runQueue(t *testing.T, r Ripper, h Hooks) (*Queue, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := Open(filepath.Join(dir, "state", "gui-queue.json"), r, h)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.SetReady(true)
	q.Start(ctx)
	return q, dir
}

func waitState(t *testing.T, q *Queue, id string, want State) Entry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, e := range q.Snapshot().Entries {
			if e.ID == id && e.State == want {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("entry %s never reached %s: %+v", id, want, q.Snapshot().Entries)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func entryIDs(q *Queue) []string {
	var out []string
	for _, e := range q.Snapshot().Entries {
		out = append(out, e.ID)
	}
	return out
}

func TestRunnerRipsInOrderOneAtATime(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{"/d", "01", filepath.Join(dir, "a.mkv")}, {"/d", "02", filepath.Join(dir, "b.mkv")}, {"/d", "03", filepath.Join(dir, "c.mkv")}})
	for _, id := range entryIDs(q) {
		e := waitState(t, q, id, Done)
		if e.StartedAt == nil || e.EndedAt == nil {
			t.Errorf("times not set: %+v", e)
		}
	}
	if got := r.order; len(got) != 3 || got[0] != "01" || got[2] != "03" {
		t.Errorf("order = %v", got)
	}
	if m := r.maxSeen.Load(); m != 1 {
		t.Errorf("%d rips ran at once", m)
	}
}

func TestRunnerWaitsForReadyAndUnpause(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	q.SetReady(false)
	q.SetPaused(true)
	q.Add([]NewEntry{{"/d", "01", filepath.Join(dir, "a.mkv")}})
	time.Sleep(50 * time.Millisecond)
	if q.Snapshot().Entries[0].State != Waiting {
		t.Fatal("ripped while paused and not ready")
	}
	q.SetReady(true)
	time.Sleep(50 * time.Millisecond)
	if q.Snapshot().Entries[0].State != Waiting {
		t.Fatal("ripped while paused")
	}
	q.SetPaused(false)
	waitState(t, q, entryIDs(q)[0], Done)
}

func TestRunnerFailureContinues(t *testing.T) {
	r := &scriptRipper{failWith: map[string]error{"01": fmt.Errorf("title 01: %w", zenvik.ErrEncrypted)}}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{"/d", "01", filepath.Join(dir, "a.mkv")}, {"/d", "02", filepath.Join(dir, "b.mkv")}})
	ids := entryIDs(q)
	e := waitState(t, q, ids[0], Failed)
	if e.Label != "encrypted" || e.Message == "" {
		t.Errorf("failed entry = %+v", e)
	}
	waitState(t, q, ids[1], Done)
}

func TestRunnerOutputExists(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	out := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	q.Add([]NewEntry{{"/d", "01", out}})
	e := waitState(t, q, entryIDs(q)[0], Failed)
	if e.Message != "a.mkv exists" || e.Label != "exists" || len(r.order) != 0 {
		t.Errorf("entry = %+v, ripper calls %v", e, r.order)
	}
}

func TestRunnerIgnoresPartialFile(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	out := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(out+".partial", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	q.Add([]NewEntry{{"/d", "01", out}})
	waitState(t, q, entryIDs(q)[0], Done)
}

func TestRunnerCreatesOutputFolder(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	out := filepath.Join(dir, "Show", "Season 1", "e1.mkv")
	q.Add([]NewEntry{{"/d", "01", out}})
	waitState(t, q, entryIDs(q)[0], Done)
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}
}

func TestRunnerCancel(t *testing.T) {
	r := &scriptRipper{release: make(chan struct{})}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{"/d", "01", filepath.Join(dir, "a.mkv")}, {"/d", "02", filepath.Join(dir, "b.mkv")}})
	ids := entryIDs(q)
	waitState(t, q, ids[0], Ripping)
	if !q.Running() {
		t.Error("Running() = false during a rip")
	}
	q.Cancel(ids[0])
	e := waitState(t, q, ids[0], Canceled)
	if e.Message != "" || e.EndedAt == nil {
		t.Errorf("canceled entry = %+v", e)
	}
	waitState(t, q, ids[1], Ripping)
	close(r.release)
	waitState(t, q, ids[1], Done)
}

func TestRunnerStopReturnsEntryToWaiting(t *testing.T) {
	r := &scriptRipper{release: make(chan struct{})}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{"/d", "01", filepath.Join(dir, "a.mkv")}, {"/d", "02", filepath.Join(dir, "b.mkv")}})
	ids := entryIDs(q)
	waitState(t, q, ids[0], Ripping)
	q.Stop(5 * time.Second)
	s := q.Snapshot()
	if s.Entries[0].State != Waiting || s.Entries[0].StartedAt != nil || s.Entries[1].State != Waiting {
		t.Errorf("after Stop = %+v", s.Entries)
	}
	time.Sleep(50 * time.Millisecond)
	if q.Running() {
		t.Error("a rip started after Stop")
	}
	reopened, err := Open(q.path, r, Hooks{})
	if err != nil || reopened.Snapshot().Entries[0].State != Waiting {
		t.Errorf("saved state = %+v, %v", reopened.Snapshot().Entries, err)
	}
}

func TestProgressThrottle(t *testing.T) {
	var mu sync.Mutex
	var got []Progress
	r := &scriptRipper{emit: func(progress func(zenvik.Progress)) {
		for i := 0; i < 200; i++ {
			progress(zenvik.Progress{Phase: zenvik.PhaseMounting, Fraction: float64(i) / 1000})
		}
		progress(zenvik.Progress{Phase: zenvik.PhaseMounting + 1, Fraction: 0.5})
		progress(zenvik.Progress{Phase: zenvik.PhaseMounting + 1, Fraction: 1})
	}}
	q, dir := runQueue(t, r, Hooks{Progress: func(p Progress) { mu.Lock(); got = append(got, p); mu.Unlock() }})
	q.Add([]NewEntry{{"/d", "01", filepath.Join(dir, "a.mkv")}})
	waitState(t, q, entryIDs(q)[0], Done)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("%d progress events, want 3 (first, phase change, 100%%): %+v", len(got), got)
	}
	if got[0].Phase != zenvik.PhaseMounting.String() || got[2].Fraction != 1 || got[0].ID == "" {
		t.Errorf("events = %+v", got)
	}
}

var _ = errors.New
```

Add `"fmt"` to the imports (`TestRunnerFailureContinues` uses it).

Run: `cd gui && go test ./internal/queue/ -run 'Runner|Progress'`
Expected: FAIL, `q.Start undefined`.

- [ ] **Step 2: Implement the runner**

`gui/internal/queue/runner.go`:

```go
package queue

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/errs"
)

// Start rips in the background until ctx is done: whenever the queue is
// ready, not paused and idle, it rips the first waiting entry.
func (q *Queue) Start(ctx context.Context) { go q.loop(ctx) }

func (q *Queue) loop(ctx context.Context) {
	for {
		e, r, ok := q.next(ctx)
		if !ok {
			select {
			case <-q.wake:
				continue
			case <-ctx.Done():
				return
			}
		}
		err := prerun(e)
		if err == nil {
			err = q.ripper.Rip(r.ctx, e, q.progress(e.ID))
		}
		q.finish(r, err)
	}
}

func (q *Queue) next(ctx context.Context) (Entry, *running, bool) {
	var e Entry
	var r *running
	q.update(func() bool {
		if q.paused || !q.ready || q.stopping || q.run != nil || ctx.Err() != nil {
			return false
		}
		i := slices.IndexFunc(q.entries, func(e Entry) bool { return e.State == Waiting })
		if i < 0 {
			return false
		}
		now := q.now()
		q.entries[i].State, q.entries[i].StartedAt, q.entries[i].EndedAt = Ripping, &now, nil
		q.entries[i].Message, q.entries[i].Label = "", ""
		rctx, cancel := context.WithCancel(ctx)
		r = &running{id: q.entries[i].ID, ctx: rctx, cancel: cancel, done: make(chan struct{})}
		q.run = r
		e = q.entries[i]
		return true
	})
	return e, r, r != nil
}

// existsError reports a finished output already in the way; zenvik's own
// "<output>.partial" doesn't count.
type existsError struct{ name string }

func (e existsError) Error() string { return e.name + " exists" }
func (e existsError) Unwrap() error { return zenvik.ErrOutputExists }

func prerun(e Entry) error {
	if _, err := os.Stat(e.OutputPath); err == nil {
		return existsError{filepath.Base(e.OutputPath)}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.MkdirAll(filepath.Dir(e.OutputPath), 0o755)
}

func (q *Queue) finish(r *running, err error) {
	r.cancel()
	q.update(func() bool {
		q.run = nil
		i := q.indexLocked(r.id)
		if i < 0 {
			return true
		}
		e := &q.entries[i]
		now := q.now()
		switch {
		case r.stopped:
			e.State, e.StartedAt = Waiting, nil
		case err == nil:
			e.State, e.EndedAt = Done, &now
		case r.canceled:
			e.State, e.EndedAt = Canceled, &now
		default:
			e.State, e.EndedAt = Failed, &now
			e.Message, e.Label = errs.Message(err), errs.Label(err)
		}
		return true
	})
	close(r.done)
}

// Cancel stops the entry's rip if it is the one running; zenvik removes the
// partial file and the entry ends canceled.
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.run != nil && q.run.id == id {
		q.run.canceled = true
		q.run.cancel()
	}
}

// Stop is for quitting: it starts nothing new, cancels the running rip (which
// goes back to waiting, so it restarts next launch) and waits up to timeout
// for it to wind down.
func (q *Queue) Stop(timeout time.Duration) {
	q.mu.Lock()
	q.stopping = true
	r := q.run
	if r != nil {
		r.stopped = true
		r.cancel()
	}
	q.mu.Unlock()
	if r != nil {
		select {
		case <-r.done:
		case <-time.After(timeout):
		}
	}
}

// Running reports whether a rip is in progress.
func (q *Queue) Running() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.run != nil
}

// progress forwards a rip's progress to Hooks.Progress, at most once per
// 100 ms within a phase; a new phase and completion always get through.
func (q *Queue) progress(id string) func(zenvik.Progress) {
	var last time.Time
	var lastPhase zenvik.Phase
	return func(p zenvik.Progress) {
		now := q.now()
		if p.Phase == lastPhase && p.Fraction < 1 && now.Sub(last) < 100*time.Millisecond {
			return
		}
		last, lastPhase = now, p.Phase
		q.hooks.Progress(Progress{ID: id, Phase: p.Phase.String(), Fraction: p.Fraction, BytesDone: p.BytesDone, BytesTotal: p.BytesTotal})
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `cd gui && go test -race -count=3 ./internal/queue/ && go vet ./...`
Expected: PASS three times in a row (22 tests, none flaky).

- [ ] **Step 4: Commit**

```bash
git add gui/internal/queue/runner.go gui/internal/queue/runner_test.go
git commit -m "Run the GUI queue one rip at a time with cancel, stop-for-quit and throttled progress"
```

---

### Task 5: The library ripper and an integration test

**Files:**
- Create: `gui/internal/queue/libripper.go`, `gui/internal/queue/libripper_test.go`, `gui/internal/queue/libripper_integration_test.go`

**Interfaces:**
- Consumes (Task 3): `Entry`, `Ripper`, `Open`, `Hooks`, `NewEntry`. Task 4's `Start` and `waitState` are used only in the integration test. Run that test after Task 4 is merged; the unit tests don't need Task 4.
- Consumes from zenvik: `zenvik.Open`, `Disc.Title`, `Disc.Rip`, `RipOptions`, `Disc.Close`.
- Produces: `type LibRipper struct { Open func(ctx context.Context, path string) (*zenvik.Disc, error); MkvmergePath func() string }` and `(LibRipper).Rip`.

- [ ] **Step 1: Write the failing unit tests**

`gui/internal/queue/libripper_test.go`:

```go
package queue

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func libOpen(ctx context.Context, path string) (*zenvik.Disc, error) { return zenvik.Open(ctx, path) }

func TestLibRipperMissingDisc(t *testing.T) {
	r := LibRipper{Open: libOpen, MkvmergePath: func() string { return "" }}
	err := r.Rip(context.Background(), Entry{DiscPath: filepath.Join(t.TempDir(), "gone.iso"), TitleID: "00800", OutputPath: "/x.mkv"}, nil)
	if err == nil {
		t.Fatal("want an error for a missing disc")
	}
}

func TestLibRipperUnknownTitle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	r := LibRipper{Open: libOpen, MkvmergePath: func() string { return "" }}
	err := r.Rip(context.Background(), Entry{DiscPath: root, TitleID: "99999", OutputPath: "/x.mkv"}, nil)
	if err == nil || !strings.Contains(err.Error(), "title 99999 not found on SAMPLE_MOVIE") {
		t.Errorf("err = %v", err)
	}
}
```

Run: `cd gui && go test ./internal/queue/ -run LibRipper`
Expected: FAIL, `undefined: LibRipper`.

- [ ] **Step 2: Implement `LibRipper`**

`gui/internal/queue/libripper.go`:

```go
package queue

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/chad3814/zenvik"
)

// LibRipper rips with the zenvik library. It opens the disc afresh for each
// entry, so a queue restored after a restart doesn't depend on what the
// window has open, and finds the title by ID.
type LibRipper struct {
	Open         func(ctx context.Context, path string) (*zenvik.Disc, error)
	MkvmergePath func() string // the config's mkvmerge_path, read when the rip starts
}

// Rip implements Ripper.
func (r LibRipper) Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error {
	d, err := r.Open(ctx, e.DiscPath)
	if err != nil {
		return err
	}
	defer d.Close()
	t, err := d.Title(e.TitleID)
	if err != nil {
		return fmt.Errorf("title %s not found on %s: %w", e.TitleID, filepath.Base(e.DiscPath), err)
	}
	_, err = d.Rip(ctx, t, zenvik.RipOptions{OutputPath: e.OutputPath, MkvmergePath: r.MkvmergePath(), OnProgress: progress})
	return err
}
```

Run: `cd gui && go test ./internal/queue/ -run LibRipper`
Expected: PASS (2 tests).

- [ ] **Step 3: Write the integration test**

`gui/internal/queue/libripper_integration_test.go`:

```go
//go:build integration

package queue

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
)

// TestLibRipperRipsRealDisc runs the real queue and ripper against a short
// Blu-ray folder authored with ffmpeg; it needs mkvmerge and ffmpeg.
func TestLibRipperRipsRealDisc(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	disc, err := testdisc.RealMovieDisc(context.Background(), t.TempDir(), 7)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "REAL MOVIE (2001) é")
	if err := disc.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	d, err := libOpen(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	main := d.Main().ID
	d.Close()

	var mu sync.Mutex
	var phases []string
	r := LibRipper{Open: libOpen, MkvmergePath: func() string { return "" }}
	q, dir := runQueue(t, r, Hooks{Progress: func(p Progress) { mu.Lock(); phases = append(phases, p.Phase); mu.Unlock() }})
	out := filepath.Join(dir, "Real Movie.mkv")
	if msgs := q.Add([]NewEntry{{root, main, out}}); msgs != nil {
		t.Fatal(msgs)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		e := q.Snapshot().Entries[0]
		if e.State == Done {
			break
		}
		if e.State == Failed || time.Now().After(deadline) {
			t.Fatalf("entry = %+v", e)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("output: %v", err)
	}
	if _, err := os.Stat(out + ".partial"); err == nil {
		t.Error(".partial left behind")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(phases) == 0 {
		t.Error("no progress reported")
	}
}
```

- [ ] **Step 4: Run all queue tests, including the integration test** (this needs mkvmerge and ffmpeg, which the macOS dev machine has)

Run: `cd gui && go test -race ./internal/queue/ && go test -tags integration -run RealDisc -v ./internal/queue/`
Expected: unit tests PASS. `TestLibRipperRipsRealDisc` passes in under a minute.

- [ ] **Step 5: Commit**

```bash
git add gui/internal/queue/libripper*.go
git commit -m "Rip GUI queue entries with the zenvik library, reopening each disc by path"
```

---

### Task 6: App bindings, shell, settings and wiring (package `main`)

**Files:**
- Modify: `gui/app.go` (rewrite), `gui/main.go`, `gui/version_test.go` (uses `NewApp(Deps{})`)
- Create: `gui/shell.go`, `gui/settings.go`, `gui/names.go`, `gui/reveal.go`, `gui/app_test.go`, `gui/names_test.go`, `gui/reveal_test.go`
- Regenerate: `gui/frontend/wailsjs/go/main/App.{js,d.ts}` (and `models.ts` if Wails writes one)

**Interfaces:**
- Consumes:
  - Task 2: `discs.New`, `List` methods, `discs.Config`, `discs.DefaultOutputDir`, `discs.Summary`, `errs.Message`.
  - Tasks 3–5: `queue.Open`, `Hooks`, `NewEntry`, `Snapshot`, `Progress`, `Start`, `Stop`, `Running`, `Add`, `Rename`, `Move`, `Remove`, `Cancel`, `Retry`, `ClearFinished`, `SetPaused`, `SetReady`, `Warning`, `LibRipper`.
  - Root module: `config.DefaultPath`, `config.Load`, `config.Resolve`, `config.Settings`, `config.File`, `config.Flags`, `naming.Parse`, `mux.Find`, `zenvik.ErrMkvmergeNotFound`, `zenvik.ErrMkvmergeTooOld`, `zenvik.Open`, `zenvik.WithMinDuration`.
- Produces:
  - Bound methods: `Ready()`, `Version() string`, `AddPaths(paths []string)`, `PickISOs() error`, `PickFolder() error`, `RemoveDisc(path string)`, `PickOutputDir(path string) error`, `Enqueue(path string, titleIDs []string, names []string) []string`, `Rename(id, name string) string`, `Move(id string, index int)`, `Remove(id string)`, `Cancel(id string)`, `Retry(id string)`, `ClearFinished()`, `SetPaused(paused bool)`, `RecheckMkvmerge()`, `ReloadConfig()`, `Reveal(id string) string`.
  - The events listed in Global Constraints.
  - `type Banner struct{ ID, Message, Action string }` (JSON `id`, `message`, `action`). IDs are `config`, `mkvmerge`, `queue` and `save`, and `Action` is `"recheck"` for `mkvmerge`.

- [ ] **Step 1: Write the failing name and reveal tests**

`gui/names_test.go`:

```go
package main

import (
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	dir := filepath.FromSlash("/out")
	cases := []struct {
		name, want, msg string
	}{
		{"Movie.mkv", "/out/Movie.mkv", ""},
		{"  Movie  ", "/out/Movie.mkv", ""},
		{"Movie.avi", "/out/Movie.avi.mkv", ""},
		{"Movie.MKV", "/out/Movie.MKV", ""},
		{"Show/S01/e1.mkv", "/out/Show/S01/e1.mkv", ""},
		{"a/../b.mkv", "/out/b.mkv", ""},
		{"", "", "the name is empty"},
		{".", "", "the name is empty"},
		{"../escape.mkv", "", "the name must stay inside the output folder"},
		{"/abs/file.mkv", "", "use a name, not a full path"},
	}
	for _, c := range cases {
		got, msg := outputPath(dir, c.name)
		want := ""
		if c.want != "" {
			want = filepath.FromSlash(c.want)
		}
		if got != want || msg != c.msg {
			t.Errorf("outputPath(%q) = %q, %q; want %q, %q", c.name, got, msg, want, c.msg)
		}
	}
}

func TestFileNameError(t *testing.T) {
	for name, want := range map[string]string{
		"New name":  "",
		"":          "the name is empty",
		"  ":        "the name is empty",
		"..":        "the name is empty",
		"a/b.mkv":   "a file name can't contain a folder",
		`a\b.mkv`:   "a file name can't contain a folder",
	} {
		if got := fileNameError(name); got != want {
			t.Errorf("fileNameError(%q) = %q, want %q", name, got, want)
		}
	}
}
```

`gui/reveal_test.go`:

```go
package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRevealArgs(t *testing.T) {
	p := filepath.FromSlash("/out/a.mkv")
	cases := map[string][]string{
		"darwin":  {"open", "-R", p},
		"windows": {"explorer", "/select," + p},
		"linux":   {"xdg-open", filepath.Dir(p)},
	}
	for goos, want := range cases {
		if got := revealArgs(goos, p); !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}
```

Run: `cd gui && go test -run 'OutputPath|FileName|Reveal' .`
Expected: FAIL, `undefined: outputPath`.

- [ ] **Step 2: Implement names and reveal**

`gui/names.go`:

```go
package main

import (
	"path/filepath"
	"strings"
)

// outputPath joins a disc's output folder and a name from the titles list.
// The name is a relative path (the template may add subfolders) that must
// stay inside the folder; ".mkv" is added unless the name already ends in it.
// It returns the path, or "" and why the name can't be used.
func outputPath(dir, name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "the name is empty"
	}
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.HasPrefix(name, "/") {
		return "", "use a name, not a full path"
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	switch {
	case clean == ".":
		return "", "the name is empty"
	case clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)):
		return "", "the name must stay inside the output folder"
	}
	return filepath.Join(dir, withMKV(clean)), ""
}

func withMKV(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".mkv") {
		return name
	}
	return name + ".mkv"
}

// fileNameError checks a queue entry's new file name (the folder stays).
func fileNameError(name string) string {
	n := strings.TrimSpace(name)
	switch {
	case n == "" || n == "." || n == "..":
		return "the name is empty"
	case strings.ContainsAny(n, `/\`):
		return "a file name can't contain a folder"
	}
	return ""
}
```

`gui/reveal.go`:

```go
package main

import (
	"os/exec"
	"path/filepath"
)

// revealArgs is the command that shows path in the OS file manager: selected
// in Finder or Explorer, or its folder opened on Linux.
func revealArgs(goos, path string) []string {
	switch goos {
	case "darwin":
		return []string{"open", "-R", path}
	case "windows":
		return []string{"explorer", "/select," + path}
	}
	return []string{"xdg-open", filepath.Dir(path)}
}

func reveal(goos, path string) error {
	args := revealArgs(goos, path)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // explorer exits 1 even on success
	return nil
}
```

Run: `cd gui && go test -run 'OutputPath|FileName|Reveal' .`
Expected: PASS.

- [ ] **Step 3: Write the failing App tests**

`gui/app_test.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/discs"
	"github.com/chad3814/zenvik/gui/internal/queue"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type fakeShell struct {
	mu      sync.Mutex
	events  map[string][]any
	confirm bool
	dir     string
	files   []string
}

func (s *fakeShell) Emit(event string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events == nil {
		s.events = map[string][]any{}
	}
	s.events[event] = append(s.events[event], data)
}

func (s *fakeShell) last(event string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := s.events[event]
	if len(ev) == 0 {
		return nil
	}
	return ev[len(ev)-1]
}

func (s *fakeShell) PickFiles(string, string, string) ([]string, error) { return s.files, nil }
func (s *fakeShell) PickDir(string) (string, error)                    { return s.dir, nil }
func (s *fakeShell) Confirm(string, string, string, string) (bool, error) {
	return s.confirm, nil
}

// blockRipper blocks until ctx is done or release is closed.
type blockRipper struct{ release chan struct{} }

func (r blockRipper) Rip(ctx context.Context, e queue.Entry, _ func(zenvik.Progress)) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return os.WriteFile(e.OutputPath, []byte("mkv"), 0o644)
	}
}

type testOpts struct {
	settingsErr error
	mkvmerge    error
	template    string
}

func newTestApp(t *testing.T, o testOpts) (*App, *fakeShell, string) {
	t.Helper()
	home := t.TempDir()
	tmpl := o.template
	if tmpl == "" {
		tmpl = config.DefaultTemplate
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	deps := Deps{
		StatePath: filepath.Join(t.TempDir(), "zenvik", "gui-queue.json"),
		LoadSettings: func() (config.Settings, error) {
			if o.settingsErr != nil {
				return config.Settings{}, o.settingsErr
			}
			return config.Settings{OutputDir: ".", Template: tmpl, MinDuration: config.DefaultMinDuration}, nil
		},
		FindMkvmerge: func(context.Context, string) error { return o.mkvmerge },
		Ripper:       func(discs.Opener, func() string) queue.Ripper { return blockRipper{release} },
		Home:         home,
		GOOS:         "linux",
	}
	sh := &fakeShell{confirm: true}
	a := NewApp(deps)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.init(ctx, sh); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.shutdown(ctx) })
	return a, sh, home
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func banners(sh *fakeShell) map[string]Banner {
	m := map[string]Banner{}
	if bs, ok := sh.last("banners:changed").([]Banner); ok {
		for _, b := range bs {
			m[b.ID] = b
		}
	}
	return m
}

func sampleDisc(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func readyDisc(t *testing.T, a *App, path string) discs.Summary {
	t.Helper()
	var s discs.Summary
	waitFor(t, "disc ready", func() bool {
		var ok bool
		s, ok = a.discs.Summary(path)
		return ok && s.State == "ready"
	})
	return s
}

func TestStartupBanners(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{
		settingsErr: fmt.Errorf("%w: unknown key bogus", config.ErrInvalid),
		mkvmerge:    zenvik.ErrMkvmergeNotFound,
	})
	a.Ready()
	b := banners(sh)
	if b["config"].Message == "" || b["mkvmerge"].Action != "recheck" {
		t.Errorf("banners = %+v", b)
	}
	if snap, ok := sh.last("queue:changed").(queue.Snapshot); !ok || snap.Ready {
		t.Errorf("queue snapshot = %+v", snap)
	}
	if a.settings.Template != config.DefaultTemplate {
		t.Errorf("fallback template = %q", a.settings.Template)
	}
}

func TestRecheckClearsBanner(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{mkvmerge: zenvik.ErrMkvmergeTooOld})
	if _, ok := banners(sh)["mkvmerge"]; !ok {
		t.Fatal("no mkvmerge banner")
	}
	a.deps.FindMkvmerge = func(context.Context, string) error { return nil }
	a.RecheckMkvmerge()
	if _, ok := banners(sh)["mkvmerge"]; ok || !a.queue.Snapshot().Ready {
		t.Errorf("banners = %+v, ready = %v", banners(sh), a.queue.Snapshot().Ready)
	}
}

func TestAddPathsWithUnusualNames(t *testing.T) {
	a, sh, home := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "Swiss Family Robinson (1960) (USA) é")
	a.AddPaths([]string{dir})
	if got := sh.last("discs:select"); got != dir {
		t.Errorf("select = %v, want %q", got, dir)
	}
	s := readyDisc(t, a, dir)
	if s.OutputDir != home || s.Titles[0].DefaultName != "Sample Movie.mkv" {
		t.Errorf("summary = %+v", s)
	}
	a.AddPaths([]string{dir})
	if n := len(a.discs.Summaries()); n != 1 {
		t.Errorf("%d discs after re-adding", n)
	}
}

func TestEnqueueValidatesEveryName(t *testing.T) {
	a, _, home := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	ids := []string{s.Titles[0].ID, s.Titles[1].ID, s.Titles[2].ID}
	msgs := a.Enqueue(dir, ids, []string{"", "../x", "Fine"})
	if len(msgs) != 3 || msgs[0] != "the name is empty" || msgs[1] != "the name must stay inside the output folder" || msgs[2] != "" {
		t.Fatalf("msgs = %q", msgs)
	}
	if n := len(a.queue.Snapshot().Entries); n != 0 {
		t.Fatalf("%d entries after a rejected batch", n)
	}
	if msgs := a.Enqueue(dir, ids[:2], []string{"Movie", "Extras/Trailer.mkv"}); msgs != nil {
		t.Fatalf("msgs = %q", msgs)
	}
	got := a.queue.Snapshot().Entries
	if got[0].OutputPath != filepath.Join(home, "Movie.mkv") || got[1].OutputPath != filepath.Join(home, "Extras", "Trailer.mkv") {
		t.Errorf("outputs = %q, %q", got[0].OutputPath, got[1].OutputPath)
	}
	if msgs := a.Enqueue("/not/open", []string{"01"}, []string{"x"}); len(msgs) != 1 || msgs[0] != "the disc is no longer open" {
		t.Errorf("unknown disc = %q", msgs)
	}
}

func TestEnqueueRejectsUnrippableTitle(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{})
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := testdisc.SampleDVD().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	a.AddPaths([]string{root})
	readyDisc(t, a, root)
	if msgs := a.Enqueue(root, []string{"08"}, []string{"Angles"}); len(msgs) != 1 || msgs[0] != "this title can't be ripped" {
		t.Errorf("msgs = %q", msgs)
	}
}

func TestRenameKeepsFolder(t *testing.T) {
	a, _, home := newTestApp(t, testOpts{})
	a.queue.SetPaused(true)
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	a.Enqueue(dir, []string{s.Titles[0].ID}, []string{"Show/Old.mkv"})
	id := a.queue.Snapshot().Entries[0].ID
	if msg := a.Rename(id, "New name"); msg != "" {
		t.Fatalf("rename = %q", msg)
	}
	if got := a.queue.Snapshot().Entries[0].OutputPath; got != filepath.Join(home, "Show", "New name.mkv") {
		t.Errorf("output = %q", got)
	}
	if msg := a.Rename(id, "a/b"); msg != "a file name can't contain a folder" {
		t.Errorf("rename = %q", msg)
	}
}

func TestBeforeCloseDuringRip(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	a.Enqueue(dir, []string{s.Titles[0].ID}, []string{"Movie"})
	waitFor(t, "ripping", a.queue.Running)

	sh.confirm = false
	if prevent := a.beforeClose(context.Background()); !prevent || !a.queue.Running() {
		t.Fatalf("declined quit: prevent=%v running=%v", prevent, a.queue.Running())
	}
	sh.confirm = true
	if prevent := a.beforeClose(context.Background()); prevent {
		t.Fatal("confirmed quit was prevented")
	}
	if e := a.queue.Snapshot().Entries[0]; e.State != queue.Waiting {
		t.Errorf("entry after quit = %+v", e)
	}
}

func TestReloadConfigUpdatesDefaultNames(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	readyDisc(t, a, dir)
	a.deps.LoadSettings = func() (config.Settings, error) {
		return config.Settings{OutputDir: ".", Template: "{name} - {playlist}.mkv"}, nil
	}
	a.ReloadConfig()
	s, _ := a.discs.Summary(dir)
	if s.Titles[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name = %q", s.Titles[0].DefaultName)
	}
}

var _ = errors.New
```

Update `gui/version_test.go` to call `NewApp(Deps{}).Version()`.

Run: `cd gui && go test .`
Expected: FAIL, `undefined: Deps` (or `a.init`).

- [ ] **Step 4: Implement the shell, settings and app**

`gui/shell.go`:

```go
package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Shell is what App needs from the window: events and dialogs. wailsShell is
// the real one; tests use a fake.
type Shell interface {
	Emit(event string, data any)
	PickFiles(title, filterName, pattern string) ([]string, error)
	PickDir(title string) (string, error)
	Confirm(title, message, yes, no string) (bool, error)
}

type wailsShell struct{ ctx context.Context }

func (s wailsShell) Emit(event string, data any) { runtime.EventsEmit(s.ctx, event, data) }

func (s wailsShell) PickFiles(title, filterName, pattern string) ([]string, error) {
	return runtime.OpenMultipleFilesDialog(s.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: []runtime.FileFilter{{DisplayName: filterName, Pattern: pattern}},
	})
}

func (s wailsShell) PickDir(title string) (string, error) {
	return runtime.OpenDirectoryDialog(s.ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
}

// Confirm asks a yes/no question. Windows dialogs ignore custom buttons and
// answer "Yes" or "No", so "Yes" counts as yes too.
func (s wailsShell) Confirm(title, message, yes, no string) (bool, error) {
	r, err := runtime.MessageDialog(s.ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: title, Message: message,
		Buttons: []string{yes, no}, DefaultButton: no, CancelButton: no,
	})
	return r == yes || r == "Yes", err
}
```

`gui/settings.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/chad3814/zenvik/gui/internal/discs"
	"github.com/chad3814/zenvik/gui/internal/queue"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/naming"
)

// loadSettings reads zenvik's config file the way the CLI does, without
// flags, and checks the template.
func loadSettings() (config.Settings, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Settings{}, err
	}
	f, err := config.Load(path)
	if err != nil {
		return config.Settings{}, err
	}
	s, err := config.Resolve(f, config.Flags{})
	if err != nil {
		return config.Settings{}, err
	}
	if _, err := naming.Parse(s.Template); err != nil {
		return config.Settings{}, f.Invalid("template %q: %w", s.Template, err)
	}
	return s, nil
}

// stateFile is where the queue is saved: under $XDG_STATE_HOME when it is
// absolute, else the user cache folder (the same roots zenvik uses for its
// mount records).
func stateFile() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "zenvik", "gui-queue.json"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik", "gui-queue.json"), nil
}

func findMkvmerge(ctx context.Context, path string) error {
	_, err := mux.Find(ctx, path)
	return err
}

func defaultDeps() (Deps, error) {
	state, err := stateFile()
	if err != nil {
		return Deps{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Deps{}, err
	}
	return Deps{
		StatePath:    state,
		LoadSettings: loadSettings,
		FindMkvmerge: findMkvmerge,
		Ripper: func(open discs.Opener, mkvmerge func() string) queue.Ripper {
			return queue.LibRipper{Open: open, MkvmergePath: mkvmerge}
		},
		Home: home,
		GOOS: goruntime.GOOS,
	}, nil
}
```

`gui/app.go`:

```go
package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/discs"
	"github.com/chad3814/zenvik/gui/internal/errs"
	"github.com/chad3814/zenvik/gui/internal/queue"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Deps are App's outside dependencies, replaced in tests.
type Deps struct {
	StatePath    string
	LoadSettings func() (config.Settings, error)
	FindMkvmerge func(ctx context.Context, path string) error
	Ripper       func(open discs.Opener, mkvmergePath func() string) queue.Ripper
	Home         string
	GOOS         string
}

// Banner is a message across the top of the window.
type Banner struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"` // "recheck": show a Recheck button
}

// App is the object bound to the frontend. Its exported methods are the
// frontend's API; data flows back as events (see emit calls).
type App struct {
	deps  Deps
	ctx   context.Context
	shell Shell
	discs *discs.List
	queue *queue.Queue

	mu       sync.Mutex
	settings config.Settings
	banners  []Banner
}

// NewApp returns an App; Wails calls startup once the window exists.
func NewApp(deps Deps) *App { return &App{deps: deps} }

func (a *App) startup(ctx context.Context) {
	if err := a.init(ctx, wailsShell{ctx}); err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{Type: runtime.ErrorDialog, Title: "Zenvik can't start", Message: err.Error()})
		runtime.Quit(ctx)
	}
}

func (a *App) init(ctx context.Context, sh Shell) error {
	a.ctx, a.shell = ctx, sh
	a.reloadSettings()
	a.discs = discs.New(ctx, a.open, a.discConfig(), func(s []discs.Summary) { a.shell.Emit("discs:changed", s) })
	q, err := queue.Open(a.deps.StatePath, a.deps.Ripper(a.open, a.mkvmergePath), queue.Hooks{
		Changed: func(s queue.Snapshot) {
			a.shell.Emit("queue:changed", s)
			if s.SaveError != "" {
				a.setBanner(Banner{ID: "save", Message: s.SaveError})
			} else {
				a.clearBanner("save")
			}
		},
		Progress: func(p queue.Progress) { a.shell.Emit("queue:progress", p) },
	})
	if err != nil {
		return err
	}
	a.queue = q
	if w := q.Warning(); w != "" {
		a.setBanner(Banner{ID: "queue", Message: w})
	}
	a.RecheckMkvmerge()
	q.Start(ctx)
	return nil
}

// open opens a disc with the config's min_duration, as the CLI does; every
// title is still listed.
func (a *App) open(ctx context.Context, path string) (*zenvik.Disc, error) {
	a.mu.Lock()
	minDur := a.settings.MinDuration
	a.mu.Unlock()
	return zenvik.Open(ctx, path, zenvik.WithMinDuration(minDur))
}

func (a *App) mkvmergePath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.MkvmergePath
}

func (a *App) reloadSettings() {
	s, err := a.deps.LoadSettings()
	if err != nil {
		s, _ = config.Resolve(&config.File{}, config.Flags{})
		a.setBanner(Banner{ID: "config", Message: errs.Message(err) + " Using the built-in defaults."})
	} else {
		a.clearBanner("config")
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
}

func (a *App) discConfig() discs.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return discs.Config{
		Template:  a.settings.Template,
		OutputDir: discs.DefaultOutputDir(a.settings.OutputDir, a.deps.GOOS, a.deps.Home, dirExists),
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func (a *App) setBanner(b Banner) {
	a.mu.Lock()
	i := slices.IndexFunc(a.banners, func(x Banner) bool { return x.ID == b.ID })
	if i >= 0 {
		a.banners[i] = b
	} else {
		a.banners = append(a.banners, b)
	}
	a.mu.Unlock()
	a.emitBanners()
}

func (a *App) clearBanner(id string) {
	a.mu.Lock()
	n := len(a.banners)
	a.banners = slices.DeleteFunc(a.banners, func(x Banner) bool { return x.ID == id })
	changed := len(a.banners) != n
	a.mu.Unlock()
	if changed {
		a.emitBanners()
	}
}

func (a *App) emitBanners() {
	a.mu.Lock()
	bs := append([]Banner{}, a.banners...)
	sh := a.shell
	a.mu.Unlock()
	if sh != nil {
		sh.Emit("banners:changed", bs)
	}
}

// Ready re-sends every snapshot; the frontend calls it once it is listening.
func (a *App) Ready() {
	a.shell.Emit("discs:changed", a.discs.Summaries())
	a.shell.Emit("queue:changed", a.queue.Snapshot())
	a.emitBanners()
}

// Version is the build's version string.
func (a *App) Version() string { return version }

// AddPaths adds discs (dropped or picked) and selects the last one.
func (a *App) AddPaths(paths []string) {
	if abs := a.discs.Add(paths); len(abs) > 0 {
		a.shell.Emit("discs:select", abs[len(abs)-1])
	}
}

// PickISOs lets the user choose disc images to add.
func (a *App) PickISOs() error {
	paths, err := a.shell.PickFiles("Add disc images", "Disc images (*.iso)", "*.iso")
	if err != nil {
		return err
	}
	a.AddPaths(paths)
	return nil
}

// PickFolder lets the user choose a disc folder to add.
func (a *App) PickFolder() error {
	dir, err := a.shell.PickDir("Add a disc folder")
	if err != nil || dir == "" {
		return err
	}
	a.AddPaths([]string{dir})
	return nil
}

// RemoveDisc closes a disc; its queue entries stay.
func (a *App) RemoveDisc(path string) { a.discs.Remove(path) }

// PickOutputDir lets the user choose where a disc's MKVs go.
func (a *App) PickOutputDir(path string) error {
	dir, err := a.shell.PickDir("Choose the output folder")
	if err != nil || dir == "" {
		return err
	}
	a.discs.SetOutputDir(path, dir)
	return nil
}

// Enqueue queues titles of the disc at path, each with its name from the
// titles list. It returns nil on success, or a message per title ("" where it
// was fine) and queues nothing.
func (a *App) Enqueue(path string, titleIDs []string, names []string) []string {
	msgs := make([]string, len(titleIDs))
	s, ok := a.discs.Summary(path)
	if !ok || s.State != "ready" || len(titleIDs) != len(names) {
		for i := range msgs {
			msgs[i] = "the disc is no longer open"
		}
		return msgs
	}
	items := make([]queue.NewEntry, len(titleIDs))
	bad := false
	for i, id := range titleIDs {
		out, msg := outputPath(s.OutputDir, names[i])
		if msg == "" && !rippable(s, id) {
			msg = "this title can't be ripped"
		}
		msgs[i], items[i] = msg, queue.NewEntry{DiscPath: path, TitleID: id, OutputPath: out}
		bad = bad || msg != ""
	}
	if bad {
		return msgs
	}
	return a.queue.Add(items)
}

func rippable(s discs.Summary, id string) bool {
	for _, t := range s.Titles {
		if t.ID == id {
			return t.Rippable
		}
	}
	return false
}

// Rename gives a waiting entry a new file name in the same folder.
func (a *App) Rename(id, name string) string {
	if msg := fileNameError(name); msg != "" {
		return msg
	}
	for _, e := range a.queue.Snapshot().Entries {
		if e.ID == id {
			return a.queue.Rename(id, filepath.Join(filepath.Dir(e.OutputPath), withMKV(strings.TrimSpace(name))))
		}
	}
	return "no such queue entry"
}

// Move puts a queue entry at index.
func (a *App) Move(id string, index int) { a.queue.Move(id, index) }

// Remove drops a queue entry that isn't ripping.
func (a *App) Remove(id string) { a.queue.Remove(id) }

// Cancel stops the running rip.
func (a *App) Cancel(id string) { a.queue.Cancel(id) }

// Retry queues a failed or canceled entry again.
func (a *App) Retry(id string) { a.queue.Retry(id) }

// ClearFinished drops done, failed and canceled entries.
func (a *App) ClearFinished() { a.queue.ClearFinished() }

// SetPaused pauses or resumes the queue.
func (a *App) SetPaused(paused bool) { a.queue.SetPaused(paused) }

// RecheckMkvmerge looks for mkvmerge again; the queue only runs once found.
func (a *App) RecheckMkvmerge() {
	err := a.deps.FindMkvmerge(a.ctx, a.mkvmergePath())
	switch {
	case err == nil:
		a.clearBanner("mkvmerge")
	case errors.Is(err, zenvik.ErrMkvmergeNotFound):
		a.setBanner(Banner{ID: "mkvmerge", Action: "recheck", Message: "mkvmerge wasn't found. Install MKVToolNix (https://mkvtoolnix.download) or set mkvmerge_path in zenvik's config, then press Recheck."})
	case errors.Is(err, zenvik.ErrMkvmergeTooOld):
		a.setBanner(Banner{ID: "mkvmerge", Action: "recheck", Message: errs.Message(err) + " Update MKVToolNix, then press Recheck."})
	default:
		a.setBanner(Banner{ID: "mkvmerge", Action: "recheck", Message: errs.Message(err)})
	}
	a.queue.SetReady(err == nil)
}

// ReloadConfig re-reads zenvik's config (the frontend calls it when the
// window regains focus) and re-renders default names and folders.
func (a *App) ReloadConfig() {
	a.reloadSettings()
	a.discs.Configure(a.discConfig())
}

// Reveal shows a finished entry's file in the file manager; it returns "" or
// what went wrong.
func (a *App) Reveal(id string) string {
	for _, e := range a.queue.Snapshot().Entries {
		if e.ID == id {
			if err := reveal(a.deps.GOOS, e.OutputPath); err != nil {
				return err.Error()
			}
			return ""
		}
	}
	return "no such queue entry"
}

// beforeClose asks before quitting during a rip; a confirmed quit cancels the
// rip and returns its entry to waiting.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.queue == nil || !a.queue.Running() {
		return false
	}
	ok, err := a.shell.Confirm("Quit Zenvik?", "A rip is running. Cancel it and quit? It starts again the next time you open Zenvik.", "Cancel rip and quit", "Keep ripping")
	if err != nil || !ok {
		return true
	}
	a.queue.Stop(30 * time.Second)
	return false
}

func (a *App) shutdown(ctx context.Context) {
	if a.queue != nil {
		a.queue.Stop(30 * time.Second)
	}
	if a.discs != nil {
		a.discs.Close()
	}
}
```

Add `"path/filepath"` and `"strings"` to `app.go`'s imports, since `Rename` uses both.

`gui/main.go`: replace the `main` function body (keep the package doc, `version` and `assets`):

```go
func main() {
	deps, err := defaultDeps()
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
	app := NewApp(deps)
	err = wails.Run(&options.App{
		Title:         "Zenvik",
		Width:         1200,
		Height:        800,
		MinWidth:      900,
		MinHeight:     560,
		AssetServer:   &assetserver.Options{Assets: assets},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		DragAndDrop:   &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		Bind:          []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run the tests, regenerate the bindings and build**

```bash
cd gui && go vet ./... && go test -race ./...
$WAILS generate module
grep -c 'export function' frontend/wailsjs/go/main/App.d.ts
cd frontend && npm run build && cd .. && $WAILS build -platform darwin/arm64 -clean && cd ..
```

Expected:
- Every Go test passes: the new App tests, and the name and reveal tests.
- `App.d.ts` has 18 functions, including `Enqueue(arg1:string,arg2:Array<string>,arg3:Array<string>):Promise<Array<string>>`.
- The frontend and app build succeed.

- [ ] **Step 6: Commit**

```bash
git add gui
git commit -m "Wire the GUI backend: bound methods, events, banners, config and mkvmerge checks, quit handling"
```

---

### Task 7: Frontend data layer, discs list and titles panel

**Files:**
- Create: `gui/frontend/src/types.ts`, `src/api.ts`, `src/store.ts`, `src/picks.ts`, `src/components/DiscList.tsx`, `src/components/TitlesPanel.tsx`
- Test: `src/__tests__/store.test.ts`, `src/__tests__/picks.test.ts`, `src/__tests__/DiscList.test.tsx`, `src/__tests__/TitlesPanel.test.tsx`

**Interfaces:**
- Consumes: the generated `wailsjs/go/main/App` functions from Task 6, `wailsjs/runtime` (`EventsOn`, which returns an unsubscribe function, `OnFileDrop(cb, useDropTarget)` and `OnFileDropOff`), and `src/format.ts` from Task 1.
- Produces:
  - Types: `types.ts` (`TitleSummary`, `DiscSummary`, `EntryState`, `Entry`, `QueueSnapshot`, `Progress`, `Banner`).
  - The `api` object and `on<T>(event, cb): () => void`, `onFileDrop(cb)`, `offFileDrop()`.
  - `store.ts`: `LiveProgress`, `AppState`, `mergeProgress`, `useAppState()`.
  - `picks.ts`: `Pick`, `DiscPicks`, `Picks`, `isTicked`, `nameFor`, `setPick`, `tickedTitles`, `clearTicks`.
  - Components:
    - `<DiscList discs selected onSelect onRemove onPickISOs onPickFolder />`
    - `<TitlesPanel disc picks errors onTick onName onPickOutputDir onEnqueue />`

- [ ] **Step 1: Write the types and the API wrapper** (no logic, so nothing to test-drive)

`gui/frontend/src/types.ts`:

```ts
export interface TitleSummary {
  id: string;
  durationSeconds: number;
  chapters: number;
  sizeBytes: number;
  main: boolean;
  rippable: boolean;
  reason?: string;
  skippedCells: string[];
  defaultName: string;
}

export interface DiscSummary {
  path: string;
  state: 'opening' | 'ready' | 'error';
  error?: string;
  errorLabel?: string;
  format?: string;
  label?: string;
  name?: string;
  ambiguous: boolean;
  outputDir: string;
  titles: TitleSummary[];
}

export type EntryState = 'waiting' | 'ripping' | 'done' | 'failed' | 'canceled';

export interface Entry {
  id: string;
  discPath: string;
  titleId: string;
  outputPath: string;
  state: EntryState;
  message?: string;
  label?: string;
  addedAt: string;
  startedAt?: string;
  endedAt?: string;
}

export interface QueueSnapshot {
  entries: Entry[];
  paused: boolean;
  ready: boolean;
  saveError?: string;
}

export interface Progress {
  id: string;
  phase: string;
  fraction: number;
  bytesDone: number;
  bytesTotal: number;
}

export interface Banner {
  id: string;
  message: string;
  action?: 'recheck';
}
```

`gui/frontend/src/api.ts`:

```ts
import * as App from '../wailsjs/go/main/App';
import { EventsOn, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';

/** api is the backend's bound methods; data comes back through on(). */
export const api = {
  ready: (): Promise<void> => App.Ready(),
  version: (): Promise<string> => App.Version(),
  addPaths: (paths: string[]): Promise<void> => App.AddPaths(paths),
  pickISOs: (): Promise<void> => App.PickISOs(),
  pickFolder: (): Promise<void> => App.PickFolder(),
  removeDisc: (path: string): Promise<void> => App.RemoveDisc(path),
  pickOutputDir: (path: string): Promise<void> => App.PickOutputDir(path),
  enqueue: (path: string, titleIds: string[], names: string[]): Promise<string[] | null> =>
    App.Enqueue(path, titleIds, names),
  rename: (id: string, name: string): Promise<string> => App.Rename(id, name),
  move: (id: string, index: number): Promise<void> => App.Move(id, index),
  remove: (id: string): Promise<void> => App.Remove(id),
  cancel: (id: string): Promise<void> => App.Cancel(id),
  retry: (id: string): Promise<void> => App.Retry(id),
  clearFinished: (): Promise<void> => App.ClearFinished(),
  setPaused: (paused: boolean): Promise<void> => App.SetPaused(paused),
  recheckMkvmerge: (): Promise<void> => App.RecheckMkvmerge(),
  reloadConfig: (): Promise<void> => App.ReloadConfig(),
  reveal: (id: string): Promise<string> => App.Reveal(id),
};

/** on subscribes to a backend event; it returns the unsubscribe function. */
export function on<T>(event: string, cb: (value: T) => void): () => void {
  return EventsOn(event, (...data: unknown[]) => cb(data[0] as T));
}

/** onFileDrop reports paths dropped anywhere on the window. */
export function onFileDrop(cb: (paths: string[]) => void): void {
  OnFileDrop((_x: number, _y: number, paths: string[]) => cb(paths), false);
}

export function offFileDrop(): void {
  OnFileDropOff();
}
```

Run: `cd gui/frontend && npx tsc --noEmit`
Expected: no errors. That proves the generated bindings match these names and signatures. If a generated signature differs (for example, `Enqueue` returns `Promise<Array<string>>`), that is still assignable. A genuine mismatch means Task 6's Go signature is wrong; fix it there.

- [ ] **Step 2: Write the failing picks and store tests**

`gui/frontend/src/__tests__/picks.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { clearTicks, isTicked, nameFor, setPick, tickedTitles } from '../picks';
import type { DiscSummary, TitleSummary } from '../types';

const title = (id: string, extra: Partial<TitleSummary> = {}): TitleSummary => ({
  id, durationSeconds: 60, chapters: 1, sizeBytes: 1, main: false, rippable: true,
  skippedCells: [], defaultName: `Disc (${id}).mkv`, ...extra,
});
const disc = (extra: Partial<DiscSummary> = {}): DiscSummary => ({
  path: '/d', state: 'ready', ambiguous: false, outputDir: '/out',
  titles: [title('01', { main: true, defaultName: 'Disc.mkv' }), title('02'), title('03', { rippable: false, reason: 'encrypted' })],
  ...extra,
});

describe('picks', () => {
  it('ticks the clear main title by default', () => {
    const d = disc();
    expect(tickedTitles(d, undefined).map((t) => t.id)).toEqual(['01']);
  });
  it('ticks nothing on an ambiguous disc', () => {
    expect(tickedTitles(disc({ ambiguous: true }), undefined)).toEqual([]);
  });
  it('never ticks unrippable titles', () => {
    const d = disc();
    expect(isTicked(d, d.titles[2], { '03': { ticked: true } })).toBe(false);
  });
  it('keeps edited names through unticking', () => {
    const d = disc();
    let p = setPick({}, '/d', '02', { ticked: true, name: 'Extra.mkv' });
    p = setPick(p, '/d', '02', { ticked: false });
    expect(nameFor(d.titles[1], p['/d'])).toBe('Extra.mkv');
    expect(nameFor(d.titles[0], p['/d'])).toBe('Disc.mkv');
  });
  it('clears every tick but keeps names', () => {
    const d = disc();
    let p = setPick({}, '/d', '02', { ticked: true, name: 'Extra.mkv' });
    p = clearTicks(p, d);
    expect(tickedTitles(d, p['/d'])).toEqual([]);
    expect(p['/d']['02'].name).toBe('Extra.mkv');
  });
});
```

`gui/frontend/src/__tests__/store.test.ts`:

```ts
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mergeProgress, useAppState } from '../store';

const handlers = vi.hoisted(() => new Map<string, (v: unknown) => void>());
vi.mock('../api', () => ({
  api: { ready: vi.fn(() => Promise.resolve()) },
  on: (event: string, cb: (v: unknown) => void) => {
    handlers.set(event, cb);
    return () => handlers.delete(event);
  },
}));

describe('mergeProgress', () => {
  it('keeps the phase start within a phase and resets it on a new phase', () => {
    let p = mergeProgress({}, { id: 'a', phase: 'muxing', fraction: 0.1, bytesDone: 1, bytesTotal: 10 }, 1000);
    p = mergeProgress(p, { id: 'a', phase: 'muxing', fraction: 0.5, bytesDone: 5, bytesTotal: 10 }, 5000);
    expect(p.a.phaseStartedAt).toBe(1000);
    p = mergeProgress(p, { id: 'a', phase: 'finalizing', fraction: 0, bytesDone: 0, bytesTotal: 10 }, 9000);
    expect(p.a.phaseStartedAt).toBe(9000);
  });
});

describe('useAppState', () => {
  beforeEach(() => handlers.clear());
  it('subscribes, asks for snapshots and applies events', async () => {
    const { result } = renderHook(() => useAppState());
    const { api } = await import('../api');
    expect(api.ready).toHaveBeenCalled();
    act(() => handlers.get('discs:changed')?.([{ path: '/d', state: 'opening', ambiguous: false, outputDir: '/o', titles: [] }]));
    act(() => handlers.get('queue:changed')?.({ entries: [{ id: 'a', state: 'ripping', outputPath: '/o/a.mkv' }], paused: false, ready: true }));
    act(() => handlers.get('queue:progress')?.({ id: 'a', phase: 'muxing', fraction: 0.5, bytesDone: 1, bytesTotal: 2 }));
    act(() => handlers.get('banners:changed')?.([{ id: 'mkvmerge', message: 'missing' }]));
    expect(result.current.discs).toHaveLength(1);
    expect(result.current.queue.ready).toBe(true);
    expect(result.current.progress.a.fraction).toBe(0.5);
    expect(result.current.banners[0].id).toBe('mkvmerge');
    act(() => handlers.get('queue:changed')?.({ entries: [{ id: 'a', state: 'done', outputPath: '/o/a.mkv' }], paused: false, ready: true }));
    expect(result.current.progress.a).toBeUndefined();
  });
});
```

Run: `cd gui/frontend && npm test`
Expected: FAIL, `Failed to resolve import "../picks"` and `"../store"`.

- [ ] **Step 3: Implement picks and the store**

`gui/frontend/src/picks.ts`:

```ts
import type { DiscSummary, TitleSummary } from './types';

/** Pick is the user's choice for one title; unset fields mean "default". */
export interface Pick {
  ticked?: boolean;
  name?: string;
}
export type DiscPicks = Record<string, Pick>; // by title id
export type Picks = Record<string, DiscPicks>; // by disc path

export function isTicked(disc: DiscSummary, t: TitleSummary, picks: DiscPicks | undefined): boolean {
  if (!t.rippable) return false;
  const p = picks?.[t.id];
  if (p?.ticked !== undefined) return p.ticked;
  return t.main && !disc.ambiguous;
}

export function nameFor(t: TitleSummary, picks: DiscPicks | undefined): string {
  return picks?.[t.id]?.name ?? t.defaultName;
}

export function setPick(picks: Picks, path: string, id: string, change: Pick): Picks {
  const disc = picks[path] ?? {};
  return { ...picks, [path]: { ...disc, [id]: { ...disc[id], ...change } } };
}

export function tickedTitles(disc: DiscSummary, picks: DiscPicks | undefined): TitleSummary[] {
  return disc.titles.filter((t) => isTicked(disc, t, picks));
}

export function clearTicks(picks: Picks, disc: DiscSummary): Picks {
  let next = picks;
  for (const t of disc.titles) next = setPick(next, disc.path, t.id, { ticked: false });
  return next;
}
```

`gui/frontend/src/store.ts`:

```ts
import { useEffect, useState } from 'react';
import { api, on } from './api';
import type { Banner, DiscSummary, Progress, QueueSnapshot } from './types';

export interface LiveProgress extends Progress {
  phaseStartedAt: number; // ms, when this phase's first event arrived
}

export interface AppState {
  discs: DiscSummary[];
  queue: QueueSnapshot;
  progress: Record<string, LiveProgress>;
  banners: Banner[];
}

const initial: AppState = {
  discs: [],
  queue: { entries: [], paused: false, ready: false },
  progress: {},
  banners: [],
};

export function mergeProgress(prev: Record<string, LiveProgress>, p: Progress, now: number): Record<string, LiveProgress> {
  const old = prev[p.id];
  const phaseStartedAt = old && old.phase === p.phase ? old.phaseStartedAt : now;
  return { ...prev, [p.id]: { ...p, phaseStartedAt } };
}

function pruneProgress(progress: Record<string, LiveProgress>, queue: QueueSnapshot): Record<string, LiveProgress> {
  const running = new Set(queue.entries.filter((e) => e.state === 'ripping').map((e) => e.id));
  return Object.fromEntries(Object.entries(progress).filter(([id]) => running.has(id)));
}

/** useAppState mirrors the backend's snapshots. */
export function useAppState(): AppState {
  const [state, setState] = useState<AppState>(initial);
  useEffect(() => {
    const offs = [
      on<DiscSummary[]>('discs:changed', (discs) => setState((s) => ({ ...s, discs }))),
      on<QueueSnapshot>('queue:changed', (queue) =>
        setState((s) => ({ ...s, queue, progress: pruneProgress(s.progress, queue) })),
      ),
      on<Progress>('queue:progress', (p) =>
        setState((s) => ({ ...s, progress: mergeProgress(s.progress, p, Date.now()) })),
      ),
      on<Banner[]>('banners:changed', (banners) => setState((s) => ({ ...s, banners }))),
    ];
    void api.ready();
    return () => offs.forEach((off) => off());
  }, []);
  return state;
}
```

Run: `cd gui/frontend && npm test`
Expected: PASS for the picks and store tests.

- [ ] **Step 4: Write the failing component tests**

`gui/frontend/src/__tests__/DiscList.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { DiscList } from '../components/DiscList';
import type { DiscSummary } from '../types';

const d = (path: string, extra: Partial<DiscSummary>): DiscSummary => ({
  path, state: 'ready', ambiguous: false, outputDir: '/o', titles: [], name: path.slice(1), ...extra,
});

describe('DiscList', () => {
  it('shows each disc state and wires the buttons', async () => {
    const h = { onSelect: vi.fn(), onRemove: vi.fn(), onPickISOs: vi.fn(), onPickFolder: vi.fn() };
    render(
      <DiscList
        discs={[
          d('/Movie', { format: 'Blu-ray' }),
          d('/Opening', { state: 'opening' }),
          d('/Locked', { state: 'error', errorLabel: 'encrypted', error: 'disc is encrypted; zenvik only reads unencrypted discs' }),
        ]}
        selected="/Movie"
        {...h}
      />,
    );
    expect(screen.getByRole('button', { name: /^Movie/ })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('Blu-ray')).toBeInTheDocument();
    expect(screen.getByText('opening…')).toBeInTheDocument();
    expect(screen.getByText('encrypted')).toHaveAttribute('title', 'disc is encrypted; zenvik only reads unencrypted discs');
    await userEvent.click(screen.getByRole('button', { name: /^Opening/ }));
    expect(h.onSelect).toHaveBeenCalledWith('/Opening');
    await userEvent.click(screen.getByRole('button', { name: 'Remove Locked' }));
    expect(h.onRemove).toHaveBeenCalledWith('/Locked');
    await userEvent.click(screen.getByRole('button', { name: 'Add ISO…' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add folder…' }));
    expect(h.onPickISOs).toHaveBeenCalled();
    expect(h.onPickFolder).toHaveBeenCalled();
  });
});
```

`gui/frontend/src/__tests__/TitlesPanel.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { TitlesPanel } from '../components/TitlesPanel';
import { setPick, type Picks } from '../picks';
import type { DiscSummary, TitleSummary } from '../types';

const title = (id: string, extra: Partial<TitleSummary> = {}): TitleSummary => ({
  id, durationSeconds: 2520, chapters: 7, sizeBytes: 1.2e9, main: false, rippable: true,
  skippedCells: [], defaultName: `Firefly (${id}).mkv`, ...extra,
});
const disc: DiscSummary = {
  path: '/Firefly_D1', state: 'ready', name: 'Firefly', format: 'DVD', ambiguous: false, outputDir: '/Movies',
  titles: [
    title('01', { main: true, defaultName: 'Firefly.mkv', durationSeconds: 7581, chapters: 18 }),
    title('02', { skippedCells: ['skipped cell 22 (1.0 s at sectors 0–438)'] }),
    title('05', { rippable: false, reason: 'interleaved angle block (not supported yet)' }),
  ],
};

function Harness({ d = disc, errors = {}, onEnqueue = vi.fn() }: { d?: DiscSummary; errors?: Record<string, string>; onEnqueue?: () => void }) {
  const [picks, setPicks] = useState<Picks>({});
  return (
    <TitlesPanel
      disc={d}
      picks={picks[d.path]}
      errors={errors}
      onTick={(id, ticked) => setPicks((p) => setPick(p, d.path, id, { ticked }))}
      onName={(id, name) => setPicks((p) => setPick(p, d.path, id, { name }))}
      onPickOutputDir={vi.fn()}
      onEnqueue={onEnqueue}
    />
  );
}

describe('TitlesPanel', () => {
  it('ticks the main title with its default name and greys unsupported titles', () => {
    render(<Harness />);
    expect(screen.getByRole('checkbox', { name: 'Title 01' })).toBeChecked();
    expect(screen.getByRole('textbox', { name: 'Name for title 01' })).toHaveValue('Firefly.mkv');
    expect(screen.getByText('2:06:21')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Title 05' })).toBeDisabled();
    expect(screen.getByText('interleaved angle block (not supported yet)')).toBeInTheDocument();
    expect(screen.getByTitle('skipped cell 22 (1.0 s at sectors 0–438)')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add 1 title to queue' })).toBeEnabled();
    expect(screen.getByText('/Movies')).toBeInTheDocument();
  });

  it('keeps an edited name when unticked and re-ticked', async () => {
    render(<Harness />);
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    const field = screen.getByRole('textbox', { name: 'Name for title 02' });
    await userEvent.clear(field);
    await userEvent.type(field, 'The Train Job.mkv');
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    expect(screen.queryByRole('textbox', { name: 'Name for title 02' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    expect(screen.getByRole('textbox', { name: 'Name for title 02' })).toHaveValue('The Train Job.mkv');
    expect(screen.getByRole('button', { name: 'Add 2 titles to queue' })).toBeEnabled();
  });

  it('pre-ticks nothing on an ambiguous disc and says why', () => {
    render(<Harness d={{ ...disc, ambiguous: true }} />);
    expect(screen.getByText('No clear main title — tick the titles you want.')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Title 01' })).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Add 0 titles to queue' })).toBeDisabled();
  });

  it('shows field errors and calls onEnqueue', async () => {
    const onEnqueue = vi.fn();
    render(<Harness errors={{ '01': 'another queue entry already writes this file' }} onEnqueue={onEnqueue} />);
    expect(screen.getByText('another queue entry already writes this file')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Name for title 01' })).toHaveAttribute('aria-invalid', 'true');
    await userEvent.click(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(onEnqueue).toHaveBeenCalled();
  });

  it('shows opening and error states instead of titles', () => {
    const { rerender } = render(<Harness d={{ ...disc, state: 'opening', titles: [] }} />);
    expect(screen.getByText('Opening the disc…')).toBeInTheDocument();
    rerender(<Harness d={{ ...disc, state: 'error', error: 'no playlists found', titles: [] }} />);
    expect(screen.getByText('no playlists found')).toBeInTheDocument();
  });
});
```

Run: `cd gui/frontend && npm test`
Expected: FAIL, `Failed to resolve import "../components/DiscList"`.

- [ ] **Step 5: Implement the components**

`gui/frontend/src/components/DiscList.tsx`:

```tsx
import type { DiscSummary } from '../types';

interface Props {
  discs: DiscSummary[];
  selected: string | null;
  onSelect: (path: string) => void;
  onRemove: (path: string) => void;
  onPickISOs: () => void;
  onPickFolder: () => void;
}

function status(d: DiscSummary) {
  switch (d.state) {
    case 'opening':
      return <span className="muted">opening…</span>;
    case 'error':
      return <span className="error" title={d.error}>{d.errorLabel}</span>;
    default:
      return <span className="muted">{d.format}</span>;
  }
}

export function DiscList({ discs, selected, onSelect, onRemove, onPickISOs, onPickFolder }: Props) {
  return (
    <section className="discs" aria-label="Discs">
      <h2>Discs</h2>
      <ul>
        {discs.map((d) => {
          const name = d.name ?? d.path;
          return (
            <li key={d.path}>
              <button className="disc" aria-pressed={d.path === selected} onClick={() => onSelect(d.path)} title={d.path}>
                <span className="disc-name">{name}</span> {status(d)}
              </button>
              <button className="icon" aria-label={`Remove ${name}`} onClick={() => onRemove(d.path)}>
                ✕
              </button>
            </li>
          );
        })}
      </ul>
      <div className="add">
        <button onClick={onPickISOs}>Add ISO…</button>
        <button onClick={onPickFolder}>Add folder…</button>
        <span className="muted">or drop ISOs and disc folders anywhere in the window</span>
      </div>
    </section>
  );
}
```

`gui/frontend/src/components/TitlesPanel.tsx`:

```tsx
import { formatBytes, formatClock } from '../format';
import { isTicked, nameFor, type DiscPicks } from '../picks';
import type { DiscSummary } from '../types';

interface Props {
  disc: DiscSummary;
  picks: DiscPicks | undefined;
  errors: Record<string, string>;
  onTick: (id: string, ticked: boolean) => void;
  onName: (id: string, name: string) => void;
  onPickOutputDir: () => void;
  onEnqueue: () => void;
}

export function TitlesPanel({ disc, picks, errors, onTick, onName, onPickOutputDir, onEnqueue }: Props) {
  const n = disc.titles.filter((t) => isTicked(disc, t, picks)).length;
  return (
    <section className="titles" aria-label="Titles">
      <header>
        <h2>Titles — {disc.name ?? disc.path}</h2>
        <span className="folder">
          Folder <code>{disc.outputDir}</code> <button onClick={onPickOutputDir}>Change…</button>
        </span>
      </header>
      {disc.state === 'opening' && <p className="muted">Opening the disc…</p>}
      {disc.state === 'error' && <p className="error">{disc.error}</p>}
      {disc.state === 'ready' && (
        <>
          {disc.ambiguous && <p className="hint">No clear main title — tick the titles you want.</p>}
          <ul className="title-rows">
            {disc.titles.map((t) => {
              const ticked = isTicked(disc, t, picks);
              const err = errors[t.id];
              return (
                <li key={t.id} className={t.rippable ? '' : 'disabled'}>
                  <label className="title-row">
                    <input
                      type="checkbox"
                      aria-label={`Title ${t.id}`}
                      checked={ticked}
                      disabled={!t.rippable}
                      onChange={(e) => onTick(t.id, e.target.checked)}
                    />
                    <span className="id">{t.id}</span>
                    <span>{formatClock(t.durationSeconds)}</span>
                    <span className="muted">{t.chapters} ch</span>
                    <span className="muted">{formatBytes(t.sizeBytes)}</span>
                    {t.main && <span className="badge">main</span>}
                    {t.skippedCells.length > 0 && (
                      <span className="warn" title={t.skippedCells.join('\n')}>⚠</span>
                    )}
                    {t.reason && <span className="muted">{t.reason}</span>}
                  </label>
                  {ticked && (
                    <div className="name-field">
                      <input
                        type="text"
                        aria-label={`Name for title ${t.id}`}
                        aria-invalid={err ? 'true' : 'false'}
                        value={nameFor(t, picks)}
                        onChange={(e) => onName(t.id, e.target.value)}
                      />
                      {err && <span className="error">{err}</span>}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
          <button className="primary" disabled={n === 0} onClick={onEnqueue}>
            Add {n} {n === 1 ? 'title' : 'titles'} to queue
          </button>
        </>
      )}
    </section>
  );
}
```

`TitlesPanel.test.tsx`'s first test finds the skipped-cell icon with `getByTitle` on the joined text. That works because the icon's `title` is that single string.

- [ ] **Step 6: Run the tests and lint**

Run: `cd gui/frontend && npm test && npm run lint && npx tsc --noEmit`
Expected: PASS (6 format, 5 picks, 2 store, 1 DiscList and 5 TitlesPanel tests), with no lint output.

- [ ] **Step 7: Commit**

```bash
git add gui/frontend/src
git commit -m "Add the GUI frontend data layer, discs list and titles panel with per-title names"
```

---

### Task 8: Queue pane, banners, About and the app layout

**Files:**
- Create: `gui/frontend/src/components/QueuePane.tsx`, `Banners.tsx`, `About.tsx`
- Modify: `gui/frontend/src/App.tsx` (rewrite), `gui/frontend/src/style.css` (rewrite)
- Test: `src/__tests__/QueuePane.test.tsx`, `src/__tests__/Banners.test.tsx`, `src/__tests__/App.test.tsx`

**Interfaces:**
- Consumes (Task 7): `api`, `on`, `onFileDrop`, `offFileDrop`, `useAppState`, `LiveProgress`, `Picks`, `setPick`, `tickedTitles`, `nameFor`, `clearTicks`, `DiscList`, `TitlesPanel`, and the types. From Task 1: `formatSpan`, `formatBytes`, `eta`, `clamp01`, `basename`.
- Produces: `<QueuePane queue progress now />`, `<Banners banners />`, `<About />`, and the default-exported `App`.

- [ ] **Step 1: Write the failing tests**

`gui/frontend/src/__tests__/QueuePane.test.tsx`:

```tsx
import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueuePane } from '../components/QueuePane';
import type { Entry, QueueSnapshot } from '../types';

vi.mock('../api', () => ({
  api: {
    setPaused: vi.fn(() => Promise.resolve()),
    clearFinished: vi.fn(() => Promise.resolve()),
    cancel: vi.fn(() => Promise.resolve()),
    retry: vi.fn(() => Promise.resolve()),
    remove: vi.fn(() => Promise.resolve()),
    move: vi.fn(() => Promise.resolve()),
    reveal: vi.fn(() => Promise.resolve('')),
    rename: vi.fn((_id: string, name: string) => Promise.resolve(name.includes('/') ? "a file name can't contain a folder" : '')),
  },
}));

const entry = (id: string, state: Entry['state'], extra: Partial<Entry> = {}): Entry => ({
  id, discPath: '/d', titleId: '01', outputPath: `/Movies/${id}.mkv`, state, addedAt: '2026-10-02T10:00:00Z', ...extra,
});
const queue = (entries: Entry[], extra: Partial<QueueSnapshot> = {}): QueueSnapshot => ({ entries, paused: false, ready: true, ...extra });

describe('QueuePane', () => {
  let api: typeof import('../api').api;
  beforeEach(async () => {
    api = (await import('../api')).api;
    vi.clearAllMocks();
  });

  it('renders every state with its actions', async () => {
    const now = Date.parse('2026-10-02T10:10:00Z');
    render(
      <QueuePane
        queue={queue([
          entry('done1', 'done', { startedAt: '2026-10-02T10:00:00Z', endedAt: '2026-10-02T10:04:12Z' }),
          entry('rip', 'ripping', { startedAt: '2026-10-02T10:05:00Z' }),
          entry('wait', 'waiting'),
          entry('bad', 'failed', { label: 'encrypted', message: 'title 01: disc is encrypted' }),
          entry('stop', 'canceled'),
        ])}
        progress={{ rip: { id: 'rip', phase: 'muxing', fraction: 0.5, bytesDone: 3e9, bytesTotal: 6e9, phaseStartedAt: now - 60_000 } }}
        now={now}
      />,
    );
    const row = (name: string) => screen.getByText(name).closest('li') as HTMLElement;
    expect(within(row('done1.mkv')).getByText('done in 4m12s')).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText(/muxing/)).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText('50%')).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText('3.0 GB / 6.0 GB')).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText('ETA 1m00s')).toBeInTheDocument();
    expect(within(row('bad.mkv')).getByText('encrypted')).toHaveAttribute('title', 'title 01: disc is encrypted');

    await userEvent.click(within(row('rip.mkv')).getByRole('button', { name: 'Cancel' }));
    expect(api.cancel).toHaveBeenCalledWith('rip');
    await userEvent.click(within(row('bad.mkv')).getByRole('button', { name: 'Retry' }));
    expect(api.retry).toHaveBeenCalledWith('bad');
    await userEvent.click(within(row('done1.mkv')).getByRole('button', { name: 'Show' }));
    expect(api.reveal).toHaveBeenCalledWith('done1');
    await userEvent.click(within(row('wait.mkv')).getByRole('button', { name: 'Move up' }));
    expect(api.move).toHaveBeenCalledWith('wait', 1);
    await userEvent.click(screen.getByRole('button', { name: 'Pause queue' }));
    expect(api.setPaused).toHaveBeenCalledWith(true);
    await userEvent.click(screen.getByRole('button', { name: 'Clear finished' }));
    expect(api.clearFinished).toHaveBeenCalled();
  });

  it('renames a waiting entry and shows rename errors', async () => {
    render(<QueuePane queue={queue([entry('wait', 'waiting')])} progress={{}} now={0} />);
    await userEvent.click(screen.getByRole('button', { name: 'Rename' }));
    const field = screen.getByRole('textbox', { name: 'New name for wait.mkv' });
    await userEvent.clear(field);
    await userEvent.type(field, 'a/b{Enter}');
    expect(await screen.findByText("a file name can't contain a folder")).toBeInTheDocument();
    await userEvent.clear(field);
    await userEvent.type(field, 'Better{Enter}');
    expect(api.rename).toHaveBeenLastCalledWith('wait', 'Better');
    expect(screen.queryByRole('textbox', { name: 'New name for wait.mkv' })).not.toBeInTheDocument();
  });

  it('reorders by drag and drop', () => {
    render(<QueuePane queue={queue([entry('a', 'waiting'), entry('b', 'waiting')])} progress={{}} now={0} />);
    const store = new Map<string, string>();
    const dataTransfer = { setData: (k: string, v: string) => store.set(k, v), getData: (k: string) => store.get(k) ?? '', effectAllowed: '' };
    fireEvent.dragStart(screen.getByText('b.mkv').closest('li') as HTMLElement, { dataTransfer });
    fireEvent.drop(screen.getByText('a.mkv').closest('li') as HTMLElement, { dataTransfer });
    expect(api.move).toHaveBeenCalledWith('b', 0);
  });

  it('says when paused, not ready, or empty', () => {
    const { rerender } = render(<QueuePane queue={queue([], { paused: true, ready: false })} progress={{}} now={0} />);
    expect(screen.getByRole('button', { name: 'Start queue' })).toBeInTheDocument();
    expect(screen.getByText('Waiting for mkvmerge — see the message above.')).toBeInTheDocument();
    expect(screen.getByText('Nothing queued yet.')).toBeInTheDocument();
    rerender(<QueuePane queue={queue([])} progress={{}} now={0} />);
    expect(screen.queryByRole('button', { name: 'Clear finished' })).not.toBeInTheDocument();
  });
});
```

`gui/frontend/src/__tests__/Banners.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Banners } from '../components/Banners';

vi.mock('../api', () => ({ api: { recheckMkvmerge: vi.fn(() => Promise.resolve()) } }));

describe('Banners', () => {
  it('shows each banner and a Recheck button for mkvmerge', async () => {
    render(<Banners banners={[{ id: 'config', message: 'bad key' }, { id: 'mkvmerge', message: 'mkvmerge missing', action: 'recheck' }]} />);
    expect(screen.getAllByRole('alert')).toHaveLength(2);
    await userEvent.click(screen.getByRole('button', { name: 'Recheck' }));
    const { api } = await import('../api');
    expect(api.recheckMkvmerge).toHaveBeenCalled();
  });
});
```

`gui/frontend/src/__tests__/App.test.tsx`:

```tsx
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../App';
import type { DiscSummary } from '../types';

const handlers = vi.hoisted(() => new Map<string, (v: unknown) => void>());
const dropped = vi.hoisted(() => ({ cb: null as ((paths: string[]) => void) | null }));
vi.mock('../api', () => ({
  api: {
    ready: vi.fn(() => Promise.resolve()),
    version: vi.fn(() => Promise.resolve('v1.1.0')),
    addPaths: vi.fn(() => Promise.resolve()),
    reloadConfig: vi.fn(() => Promise.resolve()),
    enqueue: vi.fn(() => Promise.resolve(null)),
    pickISOs: vi.fn(() => Promise.resolve()),
    pickFolder: vi.fn(() => Promise.resolve()),
    removeDisc: vi.fn(() => Promise.resolve()),
    pickOutputDir: vi.fn(() => Promise.resolve()),
    setPaused: vi.fn(() => Promise.resolve()),
  },
  on: (event: string, cb: (v: unknown) => void) => {
    handlers.set(event, cb);
    return () => handlers.delete(event);
  },
  onFileDrop: (cb: (paths: string[]) => void) => {
    dropped.cb = cb;
  },
  offFileDrop: () => {
    dropped.cb = null;
  },
}));

const disc: DiscSummary = {
  path: '/Swiss.iso', state: 'ready', name: 'Swiss Family Robinson', format: 'DVD', ambiguous: false, outputDir: '/Movies',
  titles: [
    { id: '01', durationSeconds: 7581, chapters: 18, sizeBytes: 6.2e9, main: true, rippable: true, skippedCells: [], defaultName: 'Swiss Family Robinson.mkv' },
    { id: '02', durationSeconds: 151, chapters: 1, sizeBytes: 1e8, main: false, rippable: true, skippedCells: [], defaultName: 'Swiss Family Robinson (2).mkv' },
  ],
};

describe('App', () => {
  let api: typeof import('../api').api;
  beforeEach(async () => {
    handlers.clear();
    api = (await import('../api')).api;
    vi.clearAllMocks();
  });

  it('shows discs from events, enqueues ticked titles and clears ticks', async () => {
    render(<App />);
    act(() => handlers.get('discs:changed')?.([disc]));
    act(() => handlers.get('discs:select')?.('/Swiss.iso'));
    await userEvent.click(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(api.enqueue).toHaveBeenCalledWith('/Swiss.iso', ['01'], ['Swiss Family Robinson.mkv']);
    expect(await screen.findByRole('button', { name: 'Add 0 titles to queue' })).toBeDisabled();
  });

  it('shows per-title errors from enqueue', async () => {
    vi.mocked(api.enqueue).mockResolvedValueOnce(['another queue entry already writes this file']);
    render(<App />);
    act(() => handlers.get('discs:changed')?.([disc]));
    await userEvent.click(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(await screen.findByText('another queue entry already writes this file')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Title 01' })).toBeChecked();
  });

  it('adds dropped paths and reloads config on focus', () => {
    render(<App />);
    act(() => dropped.cb?.(['/a.iso', '/b folder']));
    expect(api.addPaths).toHaveBeenCalledWith(['/a.iso', '/b folder']);
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(api.reloadConfig).toHaveBeenCalled();
  });

  it('opens About with the version', async () => {
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    expect(await screen.findByText('Zenvik v1.1.0')).toBeInTheDocument();
  });
});
```

Run: `cd gui/frontend && npm test`
Expected: FAIL, `Failed to resolve import "../components/QueuePane"`.

- [ ] **Step 2: Implement the components and the layout**

`gui/frontend/src/components/QueuePane.tsx`:

```tsx
import { useState, type KeyboardEvent } from 'react';
import { api } from '../api';
import { basename, clamp01, eta, formatBytes, formatSpan } from '../format';
import type { LiveProgress } from '../store';
import type { Entry, QueueSnapshot } from '../types';

interface Props {
  queue: QueueSnapshot;
  progress: Record<string, LiveProgress>;
  now: number;
}

interface Renaming {
  id: string;
  value: string;
  error: string;
}

const finishedStates: Entry['state'][] = ['done', 'failed', 'canceled'];

export function QueuePane({ queue, progress, now }: Props) {
  const [renaming, setRenaming] = useState<Renaming | null>(null);
  const finished = queue.entries.some((e) => finishedStates.includes(e.state));

  const submitRename = async (r: Renaming) => {
    const msg = await api.rename(r.id, r.value);
    setRenaming(msg ? { ...r, error: msg } : null);
  };

  return (
    <section className="queue" aria-label="Queue">
      <header>
        <h2>Queue</h2>
        <button onClick={() => void api.setPaused(!queue.paused)}>{queue.paused ? 'Start queue' : 'Pause queue'}</button>
        {finished && <button onClick={() => void api.clearFinished()}>Clear finished</button>}
      </header>
      {!queue.ready && <p className="hint">Waiting for mkvmerge — see the message above.</p>}
      {queue.entries.length === 0 ? (
        <p className="muted">Nothing queued yet.</p>
      ) : (
        <ol className="entries">
          {queue.entries.map((e, i) => (
            <li
              key={e.id}
              data-state={e.state}
              draggable={e.state === 'waiting'}
              onDragStart={(ev) => {
                ev.dataTransfer.setData('text/plain', e.id);
                ev.dataTransfer.effectAllowed = 'move';
              }}
              onDragOver={(ev) => ev.preventDefault()}
              onDrop={(ev) => {
                ev.preventDefault();
                const id = ev.dataTransfer.getData('text/plain');
                if (id && id !== e.id) void api.move(id, i);
              }}
            >
              {renaming?.id === e.id ? (
                <span className="rename">
                  <input
                    type="text"
                    aria-label={`New name for ${basename(e.outputPath)}`}
                    autoFocus
                    value={renaming.value}
                    onChange={(ev) => setRenaming({ ...renaming, value: ev.target.value })}
                    onKeyDown={(ev: KeyboardEvent<HTMLInputElement>) => {
                      if (ev.key === 'Enter') void submitRename(renaming);
                      if (ev.key === 'Escape') setRenaming(null);
                    }}
                  />
                  {renaming.error && <span className="error">{renaming.error}</span>}
                </span>
              ) : (
                <span className="entry-name" title={e.outputPath}>{basename(e.outputPath)}</span>
              )}
              <EntryStatus entry={e} progress={progress[e.id]} now={now} />
              <span className="actions">
                {e.state === 'waiting' && (
                  <>
                    <button onClick={() => setRenaming({ id: e.id, value: basename(e.outputPath), error: '' })}>Rename</button>
                    <button aria-label="Move up" disabled={i === 0} onClick={() => void api.move(e.id, i - 1)}>↑</button>
                    <button aria-label="Move down" disabled={i === queue.entries.length - 1} onClick={() => void api.move(e.id, i + 1)}>↓</button>
                  </>
                )}
                {e.state === 'ripping' && <button onClick={() => void api.cancel(e.id)}>Cancel</button>}
                {e.state === 'done' && <button onClick={() => void api.reveal(e.id)}>Show</button>}
                {(e.state === 'failed' || e.state === 'canceled') && <button onClick={() => void api.retry(e.id)}>Retry</button>}
                {e.state !== 'ripping' && (
                  <button aria-label={`Remove ${basename(e.outputPath)}`} onClick={() => void api.remove(e.id)}>✕</button>
                )}
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function EntryStatus({ entry: e, progress: p, now }: { entry: Entry; progress: LiveProgress | undefined; now: number }) {
  switch (e.state) {
    case 'waiting':
      return <span className="muted">waiting</span>;
    case 'ripping': {
      if (!p) return <span className="muted">starting…</span>;
      const f = clamp01(p.fraction);
      const left = eta(f, p.phaseStartedAt, now);
      return (
        <span className="progress">
          <span>{p.phase}</span>
          <progress max={1} value={f} aria-label={`${p.phase} progress`} />
          <span>{Math.floor(f * 100)}%</span>
          {p.bytesTotal > 0 && <span className="muted">{formatBytes(p.bytesDone)} / {formatBytes(p.bytesTotal)}</span>}
          {left && <span className="muted">ETA {left}</span>}
        </span>
      );
    }
    case 'done': {
      const took = e.startedAt && e.endedAt ? (Date.parse(e.endedAt) - Date.parse(e.startedAt)) / 1000 : 0;
      return <span className="ok">done in {formatSpan(took)}</span>;
    }
    case 'failed':
      return (
        <span className="error" title={e.message}>
          {e.label ?? 'failed'}
        </span>
      );
    case 'canceled':
      return <span className="muted">canceled</span>;
  }
}
```

`gui/frontend/src/components/Banners.tsx`:

```tsx
import { api } from '../api';
import type { Banner } from '../types';

export function Banners({ banners }: { banners: Banner[] }) {
  if (banners.length === 0) return null;
  return (
    <div className="banners">
      {banners.map((b) => (
        <div key={b.id} role="alert" className="banner">
          <span>{b.message}</span>
          {b.action === 'recheck' && <button onClick={() => void api.recheckMkvmerge()}>Recheck</button>}
        </div>
      ))}
    </div>
  );
}
```

`gui/frontend/src/components/About.tsx`:

```tsx
import { useState } from 'react';
import { api } from '../api';

export function About() {
  const [version, setVersion] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const show = async () => {
    setOpen(true);
    setVersion(await api.version());
  };
  return (
    <>
      <button onClick={() => void show()}>About</button>
      {open && (
        <div role="dialog" aria-label="About Zenvik" className="dialog">
          <p><strong>Zenvik {version ?? ''}</strong></p>
          <p>Rips unencrypted Blu-ray and DVD images to MKV with mkvmerge.</p>
          <p><a href="https://github.com/chad3814/zenvik">github.com/chad3814/zenvik</a></p>
          <button onClick={() => setOpen(false)}>Close</button>
        </div>
      )}
    </>
  );
}
```

`gui/frontend/src/App.tsx`:

```tsx
import { useEffect, useState } from 'react';
import { api, offFileDrop, on, onFileDrop } from './api';
import { About } from './components/About';
import { Banners } from './components/Banners';
import { DiscList } from './components/DiscList';
import { QueuePane } from './components/QueuePane';
import { TitlesPanel } from './components/TitlesPanel';
import { clearTicks, nameFor, setPick, tickedTitles, type Picks } from './picks';
import { useAppState } from './store';
import type { DiscSummary } from './types';

export default function App() {
  const state = useAppState();
  const [selected, setSelected] = useState<string | null>(null);
  const [picks, setPicks] = useState<Picks>({});
  const [errors, setErrors] = useState<Record<string, Record<string, string>>>({});
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => on<string>('discs:select', setSelected), []);
  useEffect(() => {
    onFileDrop((paths) => void api.addPaths(paths));
    const focus = () => void api.reloadConfig();
    window.addEventListener('focus', focus);
    return () => {
      offFileDrop();
      window.removeEventListener('focus', focus);
    };
  }, []);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const disc = state.discs.find((d) => d.path === selected) ?? state.discs[0];
  const queued = state.queue.entries.filter((e) => e.state === 'waiting' || e.state === 'ripping').length;

  const enqueue = async (d: DiscSummary) => {
    const titles = tickedTitles(d, picks[d.path]);
    const msgs = await api.enqueue(d.path, titles.map((t) => t.id), titles.map((t) => nameFor(t, picks[d.path])));
    if (!msgs || msgs.every((m) => m === '')) {
      setPicks((p) => clearTicks(p, d));
      setErrors((e) => ({ ...e, [d.path]: {} }));
      return;
    }
    const byTitle: Record<string, string> = {};
    titles.forEach((t, i) => {
      if (msgs[i]) byTitle[t.id] = msgs[i];
    });
    setErrors((e) => ({ ...e, [d.path]: byTitle }));
  };

  return (
    <div className="app">
      <header className="top">
        <h1>Zenvik</h1>
        <span className="muted">
          {state.discs.length} {state.discs.length === 1 ? 'disc' : 'discs'} · {queued} queued
        </span>
        <About />
      </header>
      <Banners banners={state.banners} />
      <main className="panes">
        <div className="left">
          <DiscList
            discs={state.discs}
            selected={disc?.path ?? null}
            onSelect={setSelected}
            onRemove={(path) => void api.removeDisc(path)}
            onPickISOs={() => void api.pickISOs()}
            onPickFolder={() => void api.pickFolder()}
          />
          {disc && (
            <TitlesPanel
              disc={disc}
              picks={picks[disc.path]}
              errors={errors[disc.path] ?? {}}
              onTick={(id, ticked) => setPicks((p) => setPick(p, disc.path, id, { ticked }))}
              onName={(id, name) => setPicks((p) => setPick(p, disc.path, id, { name }))}
              onPickOutputDir={() => void api.pickOutputDir(disc.path)}
              onEnqueue={() => void enqueue(disc)}
            />
          )}
        </div>
        <QueuePane queue={state.queue} progress={state.progress} now={now} />
      </main>
    </div>
  );
}
```

`gui/frontend/src/style.css`:

```css
:root {
  color-scheme: light dark;
  --bg: #f6f7f9; --panel: #ffffff; --text: #1d2330; --muted: #6b7385; --line: #dfe3ea;
  --accent: #2f6fde; --error: #c62f3a; --ok: #2e8b57; --warn: #b7791f; --select: #e7efff;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  font-size: 14px;
}
@media (prefers-color-scheme: dark) {
  :root { --bg: #16191f; --panel: #1f232b; --text: #e6e9ef; --muted: #98a0b3; --line: #313744; --select: #23324d; }
}
html, body, #root { height: 100%; margin: 0; background: var(--bg); color: var(--text); }
button { font: inherit; padding: 3px 10px; border: 1px solid var(--line); border-radius: 6px; background: var(--panel); color: inherit; cursor: pointer; }
button:disabled { opacity: 0.45; cursor: default; }
button.primary { background: var(--accent); border-color: var(--accent); color: #fff; margin-top: 10px; }
button.icon { border: none; background: none; color: var(--muted); }
input[type="text"] { font: inherit; width: 100%; box-sizing: border-box; padding: 3px 6px; border: 1px solid var(--line); border-radius: 5px; background: var(--bg); color: inherit; }
input[aria-invalid="true"] { border-color: var(--error); }
h1 { font-size: 16px; margin: 0; }
h2 { font-size: 13px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--muted); margin: 0 0 8px; }
.app { display: flex; flex-direction: column; height: 100%; }
.top { display: flex; align-items: center; gap: 12px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.top .muted { flex: 1; }
.banners { padding: 8px 14px 0; display: grid; gap: 6px; }
.banner { display: flex; gap: 10px; align-items: center; justify-content: space-between; padding: 8px 10px; border-radius: 6px; background: color-mix(in srgb, var(--warn) 18%, var(--panel)); }
.panes { flex: 1; display: grid; grid-template-columns: 1.3fr 1fr; gap: 12px; padding: 12px 14px; min-height: 0; }
.left { display: flex; flex-direction: column; gap: 12px; min-height: 0; overflow: auto; }
section { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; padding: 10px 12px; }
.queue { overflow: auto; }
ul, ol { list-style: none; margin: 0; padding: 0; }
.discs li { display: flex; align-items: center; }
.disc { flex: 1; text-align: left; border: none; background: none; padding: 5px 6px; border-radius: 5px; }
.disc[aria-pressed="true"] { background: var(--select); }
.disc-name { font-weight: 600; margin-right: 6px; }
.add { display: flex; gap: 8px; align-items: center; margin-top: 8px; flex-wrap: wrap; }
.titles header, .queue header { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
.titles header h2 { flex: 1; }
.folder code { font-size: 12px; }
.title-rows li { padding: 4px 0; border-bottom: 1px solid var(--line); }
.title-rows li.disabled { color: var(--muted); }
.title-row { display: flex; gap: 10px; align-items: center; }
.title-row .id { font-variant-numeric: tabular-nums; min-width: 3.5em; }
.name-field { margin: 4px 0 2px 26px; display: grid; gap: 2px; }
.badge { font-size: 11px; padding: 0 6px; border-radius: 9px; background: var(--select); }
.warn { color: var(--warn); }
.entries li { display: grid; grid-template-columns: 1fr auto; gap: 2px 8px; padding: 6px 4px; border-bottom: 1px solid var(--line); }
.entries li[draggable="true"] { cursor: grab; }
.entry-name { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.actions { grid-row: 1 / span 2; grid-column: 2; display: flex; gap: 4px; align-items: center; }
.progress { display: flex; gap: 8px; align-items: center; }
.progress progress { width: 120px; }
.rename { display: grid; gap: 2px; }
.muted { color: var(--muted); }
.hint { color: var(--warn); margin: 4px 0 8px; }
.error { color: var(--error); }
.ok { color: var(--ok); }
.dialog { position: fixed; top: 20%; left: 50%; transform: translateX(-50%); background: var(--panel); border: 1px solid var(--line); border-radius: 10px; padding: 16px 20px; box-shadow: 0 8px 30px rgba(0,0,0,.25); z-index: 10; }
```

- [ ] **Step 3: Run the tests, lint and build**

Run: `cd gui/frontend && npm test && npm run lint && npm run build`
Expected:
- Every test passes: 19 from Task 7, plus 4 QueuePane, 1 Banners and 4 App tests.
- Lint is clean.
- `dist/` is built.

- [ ] **Step 4: Check the real window**

Run: `cd gui && $WAILS build -platform darwin/arm64 -clean && open build/bin/Zenvik.app`

Expected:
- The window opens with the layout from §3. If mkvmerge is installed, there are no banners.
- Close the window afterwards.

The full manual pass is Task 10.

- [ ] **Step 5: Commit**

```bash
git add gui/frontend/src
git commit -m "Add the GUI queue pane, banners, About dialog and two-pane layout"
```

---

### Task 9: CI, release workflow and docs

**Files:**
- Create: `scripts/release-gui.sh`
- Modify: `.github/workflows/ci.yml` (add the `gui` job), `.github/workflows/release.yml` (rewrite), `README.md` (GUI section), `CLAUDE.md` (pointer to `gui/`)

**Interfaces:**
- Consumes: the `gui/` layout from Task 1. Builds need Tasks 1–8 to be green, but this task's files are independent.
- Produces: `scripts/release-gui.sh vX.Y.Z os/arch`, which writes `dist/zenvik-gui_<ver>_<os>_<arch>.{zip|tar.gz}`. It also produces the `gui` CI job and the release jobs `cli`, `gui` (4-target matrix) and `publish`.

- [ ] **Step 1: Prove the script is missing (red)**

Run: `scripts/release-gui.sh v0.0.0-test darwin/arm64`
Expected: FAIL, `No such file or directory`.

- [ ] **Step 2: Write `scripts/release-gui.sh`**

```bash
#!/usr/bin/env bash
# Build one zenvik GUI release archive for this host into dist/.
#
#   scripts/release-gui.sh v1.1.0 darwin/arm64
#
# Targets: darwin/arm64 and darwin/amd64 (on macOS; clang builds both),
# windows/amd64 (on Windows, with 7-Zip), linux/amd64 (on Linux, needs
# libgtk-3-dev and libwebkit2gtk-4.1-dev). The GUI uses cgo, so each OS
# builds its own; the CLI archives come from scripts/release-build.sh.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^v[0-9] ]]; then
	echo "usage: $0 vX.Y.Z os/arch" >&2
	exit 2
fi
tag=$1
target=$2
ver=${tag#v}
goos=${target%/*}
goarch=${target#*/}

root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
mkdir -p "$dist"
name="zenvik-gui_${ver}_${goos}_${goarch}"

tags=()
[[ $goos == linux ]] && tags=(-tags webkit2_41)
(cd "$root/gui" && go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build \
	-clean -trimpath -platform "$target" -ldflags "-s -w -X main.version=$tag" ${tags[@]+"${tags[@]}"})

bin="$root/gui/build/bin"
stage="$dist/$name"
rm -rf "$stage"
mkdir -p "$stage"
cp "$root/LICENSE" "$stage/"
cp "$root/gui/README.md" "$stage/README.md"
case $goos in
darwin)
	cp -R "$bin/Zenvik.app" "$stage/"
	(cd "$dist" && rm -f "$name.zip" && ditto -c -k --keepParent "$name" "$name.zip")
	;;
windows)
	cp "$bin/zenvik-gui.exe" "$stage/"
	(cd "$dist" && rm -f "$name.zip" && 7z a -tzip -bso0 "$name.zip" "$name")
	;;
linux)
	cp "$bin/zenvik-gui" "$stage/"
	tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	;;
*)
	echo "unsupported target $target" >&2
	exit 2
	;;
esac
rm -rf "$stage"
ls -l "$dist/$name".*
```

```bash
chmod 755 scripts/release-gui.sh
```

- [ ] **Step 3: Run it for both macOS targets (green)**

```bash
scripts/release-gui.sh v0.0.0-test darwin/arm64 && scripts/release-gui.sh v0.0.0-test darwin/amd64
unzip -l dist/zenvik-gui_0.0.0-test_darwin_amd64.zip | grep -E 'Zenvik.app/Contents/MacOS/zenvik-gui|LICENSE|README.md'
mkdir -p /tmp/zg && unzip -oq dist/zenvik-gui_0.0.0-test_darwin_amd64.zip -d /tmp/zg && file /tmp/zg/zenvik-gui_0.0.0-test_darwin_amd64/Zenvik.app/Contents/MacOS/zenvik-gui
rm -rf /tmp/zg dist
```

Expected:
- Two zips are written, each listing `Zenvik.app/Contents/MacOS/zenvik-gui`, `LICENSE` and `README.md`.
- `file` reports `Mach-O 64-bit executable x86_64` for the amd64 build.

- [ ] **Step 4: Add the CI job**

Append to `.github/workflows/ci.yml` under `jobs:`:

```yaml
  gui:
    strategy:
      fail-fast: false
      matrix:
        os: [macos-latest, ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    defaults:
      run:
        shell: bash
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: gui/go.mod
          cache-dependency-path: gui/go.sum
      - uses: actions/setup-node@v5
        with:
          node-version-file: gui/frontend/.nvmrc
          cache: npm
          cache-dependency-path: gui/frontend/package-lock.json
      - if: runner.os == 'Linux'
        run: sudo apt-get update && sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
      - working-directory: gui/frontend
        run: npm ci && npm run lint && npm test && npm run build
      - working-directory: gui
        run: go vet ${{ runner.os == 'Linux' && '-tags webkit2_41' || '' }} ./... && go test ${{ runner.os == 'Linux' && '-tags webkit2_41' || '' }} ./...
      - if: runner.os == 'macOS'
        run: brew install mkvtoolnix ffmpeg
      - if: runner.os == 'macOS'
        working-directory: gui
        run: go test -tags integration ./internal/queue/
      - working-directory: gui
        run: go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build -clean ${{ runner.os == 'Linux' && '-tags webkit2_41' || '' }}
```

- [ ] **Step 5: Rewrite the release workflow**

`.github/workflows/release.yml`:

```yaml
name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  cli:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...
      - run: scripts/release-build.sh "$GITHUB_REF_NAME"
      - uses: actions/upload-artifact@v4
        with:
          name: cli
          path: |
            dist/*.tar.gz
            dist/*.zip

  gui:
    strategy:
      fail-fast: true
      matrix:
        include:
          - { os: macos-latest, target: darwin/arm64 }
          - { os: macos-latest, target: darwin/amd64 }
          - { os: windows-latest, target: windows/amd64 }
          - { os: ubuntu-latest, target: linux/amd64 }
    runs-on: ${{ matrix.os }}
    defaults:
      run:
        shell: bash
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: gui/go.mod
          cache-dependency-path: gui/go.sum
      - uses: actions/setup-node@v5
        with:
          node-version-file: gui/frontend/.nvmrc
          cache: npm
          cache-dependency-path: gui/frontend/package-lock.json
      - if: runner.os == 'Linux'
        run: sudo apt-get update && sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
      - run: scripts/release-gui.sh "$GITHUB_REF_NAME" "${{ matrix.target }}"
      - uses: actions/upload-artifact@v4
        with:
          name: gui-${{ strategy.job-index }}
          path: dist/zenvik-gui_*

  publish:
    needs: [cli, gui]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/download-artifact@v5
        with:
          path: dist
          merge-multiple: true
      - run: cd dist && sha256sum ./*.tar.gz ./*.zip | sed 's| \./| |' > SHA256SUMS && cat SHA256SUMS
      - name: Create the GitHub release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          flags=(--generate-notes --verify-tag)
          if [[ $GITHUB_REF_NAME == *-* ]]; then
            flags+=(--prerelease)
          fi
          gh release create "$GITHUB_REF_NAME" "${flags[@]}" dist/*.tar.gz dist/*.zip dist/SHA256SUMS
```

- [ ] **Step 6: Lint the workflows**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml .github/workflows/release.yml`
Expected: no output (exit 0).

- [ ] **Step 7: Update the docs**

Add to `README.md`, after the install section:

```markdown
## Desktop app

`Zenvik` is a desktop app for the same job: add disc images or folders (drop them on the window), tick the titles you want, name each file, and queue rips that run one at a time. It uses your zenvik config. Download `zenvik-gui_<version>_<os>_<arch>` from the [releases](https://github.com/chad3814/zenvik/releases) (macOS: separate Apple Silicon and Intel builds). See [gui/README.md](gui/README.md) for requirements; the macOS app isn't signed yet.
```

Add to `CLAUDE.md`, under Commands:

```markdown
- GUI (separate module in `gui/`, see `gui/CLAUDE.md`): `cd gui && go test ./...`; frontend `cd gui/frontend && npm ci && npm test`. GUI release archives: `scripts/release-gui.sh vX.Y.Z os/arch` (one target per host OS).
```

Update CLAUDE.md's release line so it mentions that the release workflow also builds the four GUI archives and writes one combined SHA256SUMS.

- [ ] **Step 8: Commit**

```bash
git add scripts/release-gui.sh .github/workflows README.md CLAUDE.md
git commit -m "Build, test and release the GUI: CI job, per-OS release matrix with a combined SHA256SUMS, docs"
```

---

### Task 10: Manual acceptance on macOS (run with the user)

**Files:** none. This is a checklist: record the results in the ledger and in the final message.

- [ ] **Step 1: Build and launch**

```bash
cd /Users/chad/Projects/zenvik/worktrees/gui-design/gui && $WAILS build -platform darwin/arm64 -clean && open build/bin/Zenvik.app
```

- [ ] **Step 2: Run the checklist**

1. **Startup:** with mkvmerge installed, there are no banners and the queue reads "Nothing queued yet."
2. **Drop a disc:** drop `/Users/chad/Projects/zenvik/Swiss Family Robinson (1960) (USA).iso` onto the window.
   - It shows `opening…`, then `DVD`.
   - Title 01 is ticked with an editable name, the skipped-cell ⚠ shows, and unsupported titles are greyed out.
3. **Add ISO… and Add folder…:** both open native dialogs. Add a second real unencrypted disc if you have one (skip if not): it lists with its format, and with the default template its extra titles' names get ` (2)`, ` (3)` suffixes.
4. **Queue and rip:** set Swiss Family's output folder to a scratch folder with **Change…**, then **Add 1 title to queue**.
   - The rip runs with phase, bar, %, bytes and ETA.
   - It ends `done in …`, and **Show** reveals it in Finder.
5. **Cancel:** queue the same title again under a new name. **Cancel** it partway: it ends `canceled`, and no `.partial` is left in the scratch folder.
6. **Quit during a rip:** start a rip and quit with ⌘Q.
   - **Keep ripping** keeps it going.
   - Quitting again and choosing **Cancel rip and quit** closes the app.
   - On relaunch, that entry is `waiting` and starts again.
7. **mkvmerge banner:** set `mkvmerge_path = "/nonexistent"` in the zenvik config and focus the window. A config-driven mkvmerge banner appears after **Recheck**. Restore the config.
8. **Cleanup:** delete the MKVs in the scratch folder and the scratch folder itself. The ISO is untouched; never copy it anywhere.

- [ ] **Step 3: Record**

Append one ledger line per checklist item (pass/fail plus what you saw). Each failure becomes a finding for the final fix pass.
