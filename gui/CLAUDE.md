# zenvik GUI

Go module `github.com/chad3814/zenvik/gui` (Wails v2 + React/TypeScript). Design: `../docs/superpowers/specs/2026-10-02-zenvik-gui-design.md`.

## Commands (from `gui/`)

- Wails CLI (never install globally): `WAILS="go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0"`
- Dev window with hot reload: `$WAILS dev`
- Build: `$WAILS build` (Linux: add `-tags webkit2_41`; needs `libgtk-3-dev libwebkit2gtk-4.1-dev`)
- Regenerate frontend bindings after changing bound methods: `$WAILS generate module`
- Go vet and tests: `go vet $(go list ./... | grep -v /node_modules/)` and `go test $(go list ./... | grep -v /node_modules/)` (plain `./...` walks into `frontend/node_modules`, where npm packages can ship Go code; needs `frontend/dist` to contain at least `gitkeep`); integration: `go test -tags integration ./internal/queue/` (needs mkvmerge and ffmpeg)
- Frontend (from `gui/frontend`): `npm ci`, `npm test`, `npm run lint`, `npm run build`

## Rules

- This module may use cgo and Wails; the root module may not, and must never import `gui/`.
- Bound App methods take and return only primitives, strings slices and `error`; data reaches the frontend as event snapshots (`discs:changed`, `discs:select`, `queue:changed`, `queue:progress`, `banners:changed`).
- `internal/discs`, `internal/queue` and `internal/errs` never import Wails.
- TypeScript: strict, never `any`; don't edit `frontend/wailsjs/` by hand.
- Tests must not touch real config/state (TestMain sets XDG dirs); disc fixtures come from the root module's `internal/testdisc`.
- Update check: `startUpdateCheck` (launch banner `update`, action `download`) and the bound `CheckForUpdate` go through `Deps.CheckUpdate`/`Deps.ForceUpdate`; tests inject fakes and set `version` to a release tag, since `dev` never checks.
