# zenvik

Go module `github.com/chad3814/zenvik`. Design: `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

## Commands

- Build: `go build ./...` (must also pass with `CGO_ENABLED=0`)
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- Unit tests: `go test -race ./...`
- Integration tests (need `mkvmerge` and `ffmpeg`; macOS also exercises `hdiutil`): `go test -tags integration ./...`
- Regenerate the hdiutil UDF fixture (macOS only): `scripts/gen-udf-fixtures.sh`

## Rules

- No cgo. Public packages `bluray` and `udf` use only the standard library.
- Third-party dependencies are limited to `spf13/cobra` and `pelletier/go-toml/v2`.
- Never commit copyrighted disc data; build fixtures with `internal/testdisc`.
- Return sentinel errors wrapped with `%w`.
