# zenvik

Go module `github.com/chad3814/zenvik`. Design: `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

## Commands

- Build: `go build ./...` (must also pass with `CGO_ENABLED=0`)
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- Unit tests: `go test -race ./...`
- Integration tests (need `mkvmerge`, `mkvextract`, `ffmpeg` and `dvdauthor`/`spumux`; macOS also exercises `hdiutil`): `go test -tags integration ./...`
- UDF repair (`zenvik repair-udf`, `internal/udfrepair`): `udf.(*FS).PaddingCRCFixes` finds directory entries whose tag CRC length leaves out padding (Linux's UDF driver rejects them; on Linux, rip names the cause when the mounted image shows no BDMV); `udfrepair` patches only those 16-byte tags, writing `<image>.udf-repair-backup` first, and `--undo` restores the original. Test images: `udfimage.Options.UnpaddedFIDCRC`. The real-kernel check is `TestKernelUDFRepair` (Linux, root, `-tags integration`).
- Regenerate the hdiutil UDF fixture (macOS only): `scripts/gen-udf-fixtures.sh`
- Release archives (what the release workflow runs): `scripts/release-build.sh vX.Y.Z` writes `dist/`. Pushing a `v*` tag runs `.github/workflows/release.yml`, which tests, builds the four CLI archives (`release-build.sh` takes an optional `os/arch …` target list; the darwin ones build on macOS), the four GUI downloads (one runner per OS, `scripts/release-gui.sh`; macOS gets a `.dmg` built by `scripts/make-dmg.sh`, tested by `scripts/make-dmg_test.sh`, with an `Applications` link; Windows and Linux get archives), signs and notarizes every darwin CLI binary, app and disk image, and publishes one GitHub release with a combined `SHA256SUMS` (tags containing `-` are pre-releases).
- GUI (separate module in `gui/`, see `gui/CLAUDE.md`): `cd gui && go test -race $(go list ./... | grep -v /node_modules/)` (skips the Go package npm's `flatted` ships under `frontend/node_modules`); frontend `cd gui/frontend && npm ci && npm test`. GUI release archives: `scripts/release-gui.sh vX.Y.Z os/arch` (one target per host OS).
- macOS signing (`scripts/macos-sign.sh`, tested by `scripts/macos-sign_test.sh`; lint scripts with `shellcheck -x scripts/*.sh`): the release workflow's macOS jobs run in the GitHub environment `release` (deployable only from `v*` tags) with secrets `MACOS_CERT_P12`, `MACOS_CERT_PASSWORD`, `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`, the repository variable `MACOS_SIGN_IDENTITY`, and `SIGN_REQUIRED=1` (missing credentials fail the release). Local signed build (no notarization): `MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-gui.sh vX.Y.Z darwin/arm64`. Never print, log or commit credential values.
- Bundled mkvmerge (desktop app, macOS and Windows): `third_party/mkvtoolnix.env` pins the MKVToolNix release and its upstream SHA-256s; `scripts/fetch-mkvmerge.sh os/arch outdir` downloads, verifies and extracts it with its license files (tests: `scripts/fetch-mkvmerge_test.sh`, add `ZENVIK_NET_TESTS=1` for real downloads); `release-gui.sh` puts it in `Zenvik.app/Contents/Helpers` (re-signed together with its Qt library — never re-sign one without the other) or beside `zenvik-gui.exe`. Repin with `scripts/bump-mkvtoolnix.sh <ver> [<macos-build>]`; its `--publish` creates the public source release `mkvtoolnix-src-<ver>`, so only run it with the user's approval.
- Homebrew tap (`chad3814/homebrew-tap`: formula `zenvik` from source with `depends_on "mkvtoolnix"`, cask `zenvik-gui` from the DMGs; its `check.sh` is the test, and Homebrew 6+ tap trust means it runs `brew trust` on both first): `scripts/homebrew-render.sh vX.Y.Z outdir` writes both from a published release (tests: `scripts/homebrew-render_test.sh`). For final tags only, the release workflow's `homebrew-render`, `homebrew-check` (runs the tap's `check.sh` on the render) and `homebrew-push` jobs update the tap, and `scripts/homebrew-should-bump.sh` (tests: `scripts/homebrew-should-bump_test.sh`) stops a push that would downgrade it; only `homebrew-push` reads the `release` environment secret `HOMEBREW_TAP_DEPLOY_KEY`, a write deploy key on the tap. Never print or commit it. Re-run a failed job to retry a bump.

## Rules

- No cgo. Public packages `bluray` and `udf` use only the standard library.
- Third-party dependencies are limited to `spf13/cobra` and `pelletier/go-toml/v2`.
- Never commit copyrighted disc data; build fixtures with `internal/testdisc`.
- Return sentinel errors wrapped with `%w`.
- Tests must not touch real user config/state: set XDG_CONFIG_HOME and XDG_STATE_HOME to temp dirs (cmd/zenvik has a TestMain for this).
