# zenvik

Go module `github.com/chad3814/zenvik`. Design: `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

## Commands

- Build: `go build ./...` (must also pass with `CGO_ENABLED=0`)
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- Unit tests: `go test -race ./...`
- Integration tests (need `mkvmerge`, `mkvextract`, `ffmpeg` and `dvdauthor`/`spumux`; macOS also exercises `hdiutil`): `go test -tags integration ./...`
- Regenerate the hdiutil UDF fixture (macOS only): `scripts/gen-udf-fixtures.sh`
- Release archives (what the release workflow runs): `scripts/release-build.sh vX.Y.Z` writes `dist/`. Pushing a `v*` tag runs `.github/workflows/release.yml`, which tests, builds the four CLI archives (darwin/arm64, darwin/amd64, linux/amd64, windows/amd64), also builds the four GUI archives (one runner per OS, `scripts/release-gui.sh`), and publishes one GitHub release with a combined `SHA256SUMS` (tags containing `-` are pre-releases).
- GUI (separate module in `gui/`, see `gui/CLAUDE.md`): `cd gui && go test -race $(go list ./... | grep -v /node_modules/)` (skips the Go package npm's `flatted` ships under `frontend/node_modules`); frontend `cd gui/frontend && npm ci && npm test`. GUI release archives: `scripts/release-gui.sh vX.Y.Z os/arch` (one target per host OS).

## Rules

- No cgo. Public packages `bluray` and `udf` use only the standard library.
- Third-party dependencies are limited to `spf13/cobra` and `pelletier/go-toml/v2`.
- Never commit copyrighted disc data; build fixtures with `internal/testdisc`.
- Return sentinel errors wrapped with `%w`.
- Tests must not touch real user config/state: set XDG_CONFIG_HOME and XDG_STATE_HOME to temp dirs (cmd/zenvik has a TestMain for this).
