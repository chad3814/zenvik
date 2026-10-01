# zenvik Milestone 1 (Parsers) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go module skeleton plus three tested packages: `bluray` (parsers for index.bdmv, MovieObject.bdmv, MPLS, CLPI, and bdmt metadata), `udf` (a read-only UDF 1.02–2.60 file system implementing `io/fs.FS`), and `internal/testdisc` (synthetic Blu-ray and UDF fixture builders).

**Architecture:** `bluray` and `udf` are standalone public parsers that depend only on the standard library. `bluray` parses byte slices through a bounds-checked cursor. `udf` resolves the volume structure (anchor → volume descriptor sequence → partition maps → optional metadata partition → file set descriptor → root) and serves files through `fs.FS`. `internal/testdisc` encodes `bluray` structs back to bytes, which gives round-trip tests. Its `udfimage` subpackage is an independent UDF writer, checked against macOS's UDF driver.

**Tech Stack:** Go 1.27, standard library only (`encoding/binary`, `encoding/xml`, `io/fs`, `testing/fstest`), golangci-lint v2, GitHub Actions, macOS `hdiutil` (integration tests and fixture generation only).

**Spec:** `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, sections 2, 3 (types consumed later), 6 (`udf`, `bluray`), 9, 10, and 11 (milestone 1).

## Global Constraints

- Module path `github.com/chad3814/zenvik`; `go 1.27` in `go.mod`.
- No cgo: `CGO_ENABLED=0 go build ./...` must succeed.
- `bluray` and `udf` import only the standard library. Their non-test code must not import `internal/...` or the root package.
- No third-party Go dependencies in this milestone. The project-wide allowance is only `spf13/cobra` and `pelletier/go-toml/v2`, and they arrive in later milestones.
- Never commit copyrighted disc data. All fixtures come from `internal/testdisc` or from `scripts/gen-udf-fixtures.sh`.
- Errors are sentinel values wrapped with `%w`, checkable with `errors.Is`.
- Code passes `gofmt`, `go vet ./...`, `golangci-lint run`, and `go test -race ./...`.
- License MIT (already committed on `main`).
- Work happens in worktree `/Users/chad/Projects/zenvik/worktrees/m1-parsers` on branch `feat/m1-parsers`, created from `docs/v1-design`. The `add-worktree` skill can't be used yet, because origin has no branches until the user approves a push.
- Never `git push`.
- Commits are normally SSH-signed through the 1Password agent. If signing fails (for example, approval times out while the user is away), commit unsigned with `git -c commit.gpgsign=false commit ...` and mention it in the task report.

## Review Focus

These are inputs the spec implies but no feature test exercises. Each one has a test pinned to the task that owns the code.

1. **Truncated ISO (e.g. an interrupted download).** Opening or reading must return an error, never zeros, wrong bytes, or a panic. Tested in Task 12 (`TestTruncatedImageNeverReturnsWrongData`).
2. **Malformed or hostile Blu-ray navigation files.** Every parser returns `ErrInvalid`, never panics. Covered by fuzz targets in Tasks 2, 3, 5 and 6 and by truncation tests in each parser task.
3. **Hostile length and count fields.** No unbounded allocations or loops: a directory size of 2^40 bytes, huge partition-map counts, and huge Blu-ray item counts. Tested in Task 11 (map table bounds), Task 12 (`TestHugeDirectoryIsCorrupt`), and the parser fuzz targets.
4. **Reference cycles.** Indirect-entry loops, allocation-extent-descriptor chains, and movie-object jump cycles must terminate with an error or a finite result. Tested in Task 3 (`TestPlaylistsJumpCycle`) and Task 12 (`TestAEDChainLoopIsCorrupt`, `TestIndirectEntryLoopIsCorrupt`), plus `FuzzOpen` in Task 12.
5. **Inconsistent playlist timing.** A play item whose OUT time is before its IN time is rejected. Marks that point at missing play items, or fall outside their item's time range, are ignored. Tested in Task 5 (`TestParsePlaylistRejectsOutBeforeIn`, `TestChaptersIgnoresBadMarks`).

---

## File Map

| File | Responsibility |
|---|---|
| `go.mod`, `doc.go` | Module definition; root package doc (facade arrives in milestone 2) |
| `.gitignore`, `.golangci.yml`, `CLAUDE.md`, `README.md` | Repo hygiene, lint config, contributor/agent instructions |
| `.github/workflows/ci.yml` | Lint, unit tests on 3 OSes, macOS integration tests |
| `bluray/reader.go` | Bounds-checked big-endian cursor shared by all parsers; `ErrInvalid` |
| `bluray/ticks.go` | 45 kHz clock type and conversion |
| `bluray/index.go` | `index.bdmv` parser |
| `bluray/movieobject.go` | `MovieObject.bdmv` parser, navigation command decoding, playlist reachability |
| `bluray/stream.go` | Coding types, stream enums, shared stream-attribute parsing |
| `bluray/playlist.go` | MPLS parser, durations, chapters |
| `bluray/clip.go` | CLPI parser |
| `bluray/meta.go` | `bdmt_*.xml` parser and file selection |
| `internal/testdisc/encode.go` | Encoders: `bluray` structs → bytes |
| `internal/testdisc/disc.go` | `Disc` builder → file map / `fstest.MapFS` / directory; nav command constructors; clean M2TS |
| `internal/testdisc/udfimage/udfimage.go` | Independent in-memory UDF 1.02/2.50 image writer |
| `udf/tag.go` | Descriptor tags, CRC, errors, little-endian helpers |
| `udf/strings.go` | OSTA CS0 d-characters and dstrings |
| `udf/timestamp.go` | ECMA-167 timestamps |
| `udf/volume.go` | `Open`: recognition sequence, anchor, descriptor sequence, partition maps, metadata partition, file set |
| `udf/entry.go` | File entries, allocation descriptors, extent reads, directory parsing |
| `udf/fs.go` | `fs.FS` implementation: `Open`, files, directory entries, file info |
| `udf/image.go` | `OpenImage` convenience for files on disk |
| `scripts/gen-udf-fixtures.sh`, `udf/testdata/hdiutil-udf102.iso` | Externally produced UDF 1.02 fixture |

---

### Task 1: Module scaffold and Blu-ray byte cursor

**Files:**
- Create: `go.mod`, `doc.go`, `.gitignore`, `.golangci.yml`, `CLAUDE.md`, `README.md`, `.github/workflows/ci.yml`
- Create: `bluray/reader.go`, `bluray/ticks.go`
- Test: `bluray/reader_test.go`, `bluray/ticks_test.go`, `bluray/helpers_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `bluray`, used by Tasks 2–7):
  - `var ErrInvalid error`
  - unexported `type reader struct`, plus `newReader(b []byte) *reader` and the methods `err() error`, `fail(format string, args ...any)`, `u8() uint8`, `u16() uint16`, `u32() uint32`, `bytes(n int) []byte`, `str(n int) string`, `skip(n int)`, `seek(off int)`, `sub(n int) *reader`, `remaining() int`, `header(magic string) string`
  - `const TicksPerSecond = 45000`, `type Ticks uint32`, `func (t Ticks) Duration() time.Duration`, unexported `ticksDuration(t uint64) time.Duration`
  - test helpers (package `bluray`, `_test.go`): `be16`, `be32`, `cat`, `zeros`

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/chad/Projects/zenvik
git worktree add -b feat/m1-parsers worktrees/m1-parsers docs/v1-design
cd worktrees/m1-parsers
git status
```

Expected: `On branch feat/m1-parsers`, clean tree.

- [ ] **Step 2: Write the module and repo files**

`go.mod`:
```
module github.com/chad3814/zenvik

go 1.27
```

`doc.go`:
```go
// Package zenvik converts unencrypted Blu-ray disc images and BDMV
// directories into MKV files.
package zenvik
```

`.gitignore`:
```
/zenvik
/dist/
*.partial
.DS_Store
```

`.golangci.yml`:
```yaml
version: "2"
linters:
  default: standard
  enable:
    - errorlint
    - misspell
    - unconvert
  exclusions:
    presets:
      - std-error-handling
formatters:
  enable:
    - gofmt
    - goimports
```

`README.md`:
```markdown
# zenvik

A friendly command-line tool and Go library for remuxing **unencrypted** Blu-ray disc images
(ISO or BDMV folders) to MKV. zenvik never decrypts anything.

Status: early development. See `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

License: MIT.
```

`CLAUDE.md`:
```markdown
# zenvik

Go module `github.com/chad3814/zenvik`. Design: `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`.

## Commands

- Build: `go build ./...` (must also pass with `CGO_ENABLED=0`)
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- Unit tests: `go test -race ./...`
- Integration tests (macOS, needs `hdiutil`): `go test -tags integration ./...`
- Regenerate the hdiutil UDF fixture (macOS only): `scripts/gen-udf-fixtures.sh`

## Rules

- No cgo. Public packages `bluray` and `udf` use only the standard library.
- Third-party dependencies are limited to `spf13/cobra` and `pelletier/go-toml/v2`.
- Never commit copyrighted disc data; build fixtures with `internal/testdisc`.
- Return sentinel errors wrapped with `%w`.
```

`.github/workflows/ci.yml`:
```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@v8
        with:
          version: latest

  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...
      - run: go build ./...
        env:
          CGO_ENABLED: "0"

  integration-macos:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go test -tags integration ./...
```

- [ ] **Step 3: Write the failing cursor and ticks tests**

`bluray/helpers_test.go`:
```go
package bluray

import "encoding/binary"

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func zeros(n int) []byte { return make([]byte, n) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
```

`bluray/reader_test.go`:
```go
package bluray

import (
	"errors"
	"testing"
)

func TestReaderReadsBigEndian(t *testing.T) {
	r := newReader(cat([]byte{0x01}, be16(0x0203), be32(0x04050607), []byte("--abc")))
	if got := r.u8(); got != 0x01 {
		t.Errorf("u8 = %#x", got)
	}
	if got := r.u16(); got != 0x0203 {
		t.Errorf("u16 = %#x", got)
	}
	if got := r.u32(); got != 0x04050607 {
		t.Errorf("u32 = %#x", got)
	}
	r.skip(2)
	if got := r.str(3); got != "abc" {
		t.Errorf("str = %q", got)
	}
	if r.remaining() != 0 || r.err() != nil {
		t.Errorf("remaining = %d, err = %v", r.remaining(), r.err())
	}
}

func TestReaderOutOfRangeIsSticky(t *testing.T) {
	r := newReader([]byte{1, 2, 3})
	if got := r.u32(); got != 0 {
		t.Errorf("u32 past end = %d, want 0", got)
	}
	if !errors.Is(r.err(), ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", r.err())
	}
	if got := r.u8(); got != 0 {
		t.Errorf("u8 after error = %d, want 0", got)
	}
}

func TestSubReaderSharesError(t *testing.T) {
	r := newReader([]byte{2, 0xAA, 0xBB, 0xCC})
	s := r.sub(int(r.u8()))
	s.u16()
	s.u8() // past the 2-byte window
	if !errors.Is(r.err(), ErrInvalid) {
		t.Fatalf("parent err = %v, want ErrInvalid", r.err())
	}
}

func TestSubReaderAdvancesParent(t *testing.T) {
	r := newReader([]byte{0xAA, 0xBB, 0xCC})
	r.sub(2)
	if got := r.u8(); got != 0xCC {
		t.Errorf("after sub, u8 = %#x, want 0xCC", got)
	}
}

func TestSeekBounds(t *testing.T) {
	r := newReader(make([]byte, 4))
	r.seek(4)
	if r.err() != nil {
		t.Fatalf("seek to end: %v", r.err())
	}
	r.seek(5)
	if !errors.Is(r.err(), ErrInvalid) {
		t.Errorf("seek past end err = %v", r.err())
	}
}

func TestHeader(t *testing.T) {
	r := newReader([]byte("MPLS0300"))
	if v := r.header("MPLS"); v != "0300" || r.err() != nil {
		t.Errorf("header = %q, %v", v, r.err())
	}
	r = newReader([]byte("HDMV0300"))
	r.header("MPLS")
	if !errors.Is(r.err(), ErrInvalid) {
		t.Errorf("wrong magic err = %v", r.err())
	}
}
```

`bluray/ticks_test.go`:
```go
package bluray

import (
	"testing"
	"time"
)

func TestTicksDuration(t *testing.T) {
	tests := []struct {
		in   Ticks
		want time.Duration
	}{
		{0, 0},
		{45000, time.Second},
		{22500, 500 * time.Millisecond},
		{45000 * 3600, time.Hour},
		{0xFFFFFFFF, 95443*time.Second + 717666666},
	}
	for _, tt := range tests {
		if got := tt.in.Duration(); got != tt.want {
			t.Errorf("Ticks(%d).Duration() = %v, want %v", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./bluray/`
Expected: FAIL to compile (`undefined: newReader`, `undefined: Ticks`).

- [ ] **Step 5: Implement the cursor and ticks**

`bluray/reader.go`:
```go
// Package bluray parses Blu-ray Disc navigation files: index.bdmv,
// MovieObject.bdmv, playlists (MPLS), clip information (CLPI), and disc
// library metadata (bdmt_*.xml). It reads byte slices and never touches
// the file system.
package bluray

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalid reports malformed or truncated Blu-ray navigation data.
var ErrInvalid = errors.New("bluray: invalid data")

// reader is a bounds-checked big-endian cursor over a byte slice. Readers
// made with sub share one error slot with their parent, so the first
// out-of-range access anywhere stops all further reads and a parser only
// needs to check err once.
type reader struct {
	b   []byte
	off int
	e   *error
}

func newReader(b []byte) *reader {
	return &reader{b: b, e: new(error)}
}

func (r *reader) err() error { return *r.e }

func (r *reader) fail(format string, args ...any) {
	if *r.e == nil {
		*r.e = fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
}

func (r *reader) need(n int) bool {
	if *r.e != nil {
		return false
	}
	if n < 0 || n > len(r.b)-r.off {
		r.fail("need %d bytes at offset %d of %d", n, r.off, len(r.b))
		return false
	}
	return true
}

func (r *reader) remaining() int { return len(r.b) - r.off }

func (r *reader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.BigEndian.Uint16(r.b[r.off:])
	r.off += 2
	return v
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.BigEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

func (r *reader) str(n int) string { return string(r.bytes(n)) }

func (r *reader) skip(n int) {
	if r.need(n) {
		r.off += n
	}
}

// seek moves to an absolute offset within the reader's window.
func (r *reader) seek(off int) {
	if *r.e != nil {
		return
	}
	if off < 0 || off > len(r.b) {
		r.fail("offset %d outside %d bytes", off, len(r.b))
		return
	}
	r.off = off
}

// sub consumes the next n bytes and returns a reader limited to them.
func (r *reader) sub(n int) *reader {
	return &reader{b: r.bytes(n), e: r.e}
}

// header reads the 4-byte type indicator and 4-byte version string that
// start every navigation file, failing if the indicator is not magic.
func (r *reader) header(magic string) string {
	m := r.str(4)
	v := r.str(4)
	if *r.e == nil && m != magic {
		r.fail("type indicator %q, want %q", m, magic)
	}
	return v
}
```

`bluray/ticks.go`:
```go
package bluray

import "time"

// TicksPerSecond is the rate of the 45 kHz clock used for play item and
// playlist mark times.
const TicksPerSecond = 45000

// Ticks is a timestamp or duration on the 45 kHz clock.
type Ticks uint32

// Duration converts t to a time.Duration.
func (t Ticks) Duration() time.Duration { return ticksDuration(uint64(t)) }

func ticksDuration(t uint64) time.Duration {
	return time.Duration(t/TicksPerSecond)*time.Second +
		time.Duration(t%TicksPerSecond)*time.Second/TicksPerSecond
}
```

- [ ] **Step 6: Run tests and checks to verify they pass**

Run: `go test -race ./... && go vet ./... && golangci-lint --version && golangci-lint run && CGO_ENABLED=0 go build ./...`
Expected: tests PASS; `golangci-lint --version` reports 2.x; no lint findings; build succeeds.

- [ ] **Step 7: Commit**

```bash
git add go.mod doc.go .gitignore .golangci.yml CLAUDE.md README.md .github bluray
git commit -m "Scaffold module and add Blu-ray byte cursor"
```

---

### Task 2: index.bdmv parser

**Files:**
- Create: `bluray/index.go`
- Test: `bluray/index_test.go`

**Interfaces:**
- Consumes: the `reader` from Task 1.
- Produces:
  - `type ObjectType uint8`, constants `ObjectHDMV ObjectType = 1`, `ObjectBDJ ObjectType = 2`
  - `type Object struct { Type ObjectType; PlaybackType uint8; MovieObjectID uint16; BDJOName string }`
  - `type IndexTitle struct { Object; AccessType uint8 }`
  - `type Index struct { Version string; FirstPlayback, TopMenu Object; Titles []IndexTitle }` (`Titles[0]` is title 1)
  - `func ParseIndex(b []byte) (*Index, error)`

- [ ] **Step 1: Write the failing test**

`bluray/index_test.go`:
```go
package bluray

import (
	"errors"
	"reflect"
	"testing"
)

// indexBytes hand-assembles an index.bdmv following the layout in
// libbluray's index_parse.c: 40-byte header, AppInfoBDMV, then Indexes.
func indexBytes() []byte {
	appInfo := cat(be32(34), zeros(34))
	objects := cat(
		[]byte{0x40, 0, 0, 0}, be16(0x0000), be16(0), zeros(4), // first playback: HDMV, movie object 0
		[]byte{0x80, 0, 0, 0}, be16(0x4000), []byte("00000"), zeros(1), // top menu: BD-J, playback type 1
		be16(2), // number of titles
		[]byte{0x40, 0, 0, 0}, be16(0), be16(1), zeros(4), // title 1: HDMV, movie object 1
		[]byte{0x90, 0, 0, 0}, be16(0xC000), []byte("00001"), zeros(1), // title 2: BD-J, access type 1, playback type 3
	)
	head := cat([]byte("INDX0200"), be32(uint32(40+len(appInfo))), be32(0), zeros(24))
	return cat(head, appInfo, be32(uint32(len(objects))), objects)
}

func TestParseIndex(t *testing.T) {
	got, err := ParseIndex(indexBytes())
	if err != nil {
		t.Fatal(err)
	}
	want := &Index{
		Version:       "0200",
		FirstPlayback: Object{Type: ObjectHDMV, MovieObjectID: 0},
		TopMenu:       Object{Type: ObjectBDJ, PlaybackType: 1, BDJOName: "00000"},
		Titles: []IndexTitle{
			{Object: Object{Type: ObjectHDMV, MovieObjectID: 1}},
			{Object: Object{Type: ObjectBDJ, PlaybackType: 3, BDJOName: "00001"}, AccessType: 1},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseIndex =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseIndexErrors(t *testing.T) {
	b := indexBytes()
	bad := append([]byte("XXXX"), b[4:]...)
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-5],
		"magic":     bad,
		"empty":     nil,
	} {
		if _, err := ParseIndex(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func FuzzParseIndex(f *testing.F) {
	f.Add(indexBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		idx, err := ParseIndex(b)
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected error type: %v", err)
		}
		if err == nil && idx == nil {
			t.Fatal("nil index without error")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bluray/ -run 'Index'`
Expected: FAIL to compile (`undefined: ParseIndex`).

- [ ] **Step 3: Implement**

`bluray/index.go`:
```go
package bluray

// ObjectType says how a title is implemented.
type ObjectType uint8

// Object types.
const (
	ObjectHDMV ObjectType = 1 // movie object navigation commands
	ObjectBDJ  ObjectType = 2 // BD-J (Java) application
)

// Object is the entry for first playback, top menu, or a title.
type Object struct {
	Type          ObjectType
	PlaybackType  uint8
	MovieObjectID uint16 // HDMV only
	BDJOName      string // BD-J only, e.g. "00000"
}

// IndexTitle is one numbered title in index.bdmv.
type IndexTitle struct {
	Object
	AccessType uint8
}

// Index is a parsed index.bdmv.
type Index struct {
	Version       string
	FirstPlayback Object
	TopMenu       Object
	Titles        []IndexTitle // Titles[0] is title 1
}

// ParseIndex parses the contents of BDMV/index.bdmv.
func ParseIndex(b []byte) (*Index, error) {
	r := newReader(b)
	idx := &Index{Version: r.header("INDX")}
	indexesStart := r.u32()
	r.seek(int(indexesStart))
	ir := r.sub(int(r.u32()))
	t, _ := objectHeader(ir)
	idx.FirstPlayback = objectBody(ir, t)
	t, _ = objectHeader(ir)
	idx.TopMenu = objectBody(ir, t)
	n := int(ir.u16())
	for i := 0; i < n && ir.err() == nil; i++ {
		t, access := objectHeader(ir)
		idx.Titles = append(idx.Titles, IndexTitle{Object: objectBody(ir, t), AccessType: access})
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return idx, nil
}

// objectHeader reads object_type (2 bits) and, for titles, access_type
// (2 bits) from a 32-bit field.
func objectHeader(r *reader) (ObjectType, uint8) {
	h := r.u32()
	return ObjectType(h >> 30), uint8(h>>28) & 0x3
}

// objectBody reads the 8-byte HDMV or BD-J object reference.
func objectBody(r *reader, t ObjectType) Object {
	o := Object{Type: t, PlaybackType: uint8(r.u16() >> 14)}
	switch t {
	case ObjectHDMV:
		o.MovieObjectID = r.u16()
		r.skip(4)
	case ObjectBDJ:
		o.BDJOName = r.str(5)
		r.skip(1)
	default:
		r.skip(6)
	}
	return o
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./bluray/ -run 'Index' && go test ./bluray/ -run '^$' -fuzz FuzzParseIndex -fuzztime 20s`
Expected: PASS, and fuzzing finds no failures.

- [ ] **Step 5: Commit**

```bash
git add bluray/index.go bluray/index_test.go
git commit -m "Add index.bdmv parser"
```

---

### Task 3: MovieObject.bdmv parser and playlist reachability

**Files:**
- Create: `bluray/movieobject.go`
- Test: `bluray/movieobject_test.go`

**Interfaces:**
- Consumes: `reader` (Task 1).
- Produces:
  - `type NavCommand struct { Opcode, Dst, Src uint32 }` with the methods `Group() uint8`, `SubGroup() uint8`, `ImmDst() bool`, `ImmSrc() bool`, `BranchOpt() uint8`, `SetOpt() uint8`
  - `type MovieObject struct { ResumeIntention, MenuCallMask, TitleSearchMask bool; Commands []NavCommand }`
  - `type MovieObjects struct { Version string; Objects []MovieObject }`
  - `func ParseMovieObjects(b []byte) (*MovieObjects, error)`
  - `func (m *MovieObjects) Playlists(objectID int) []int`
  - opcode bit layout: byte 0 = op_cnt:3 group:2 sub_group:3; byte 1 = imm_dst:1 imm_src:1 reserved:2 branch_opt:4; byte 2 = reserved:4 cmp_opt:4; byte 3 = reserved:3 set_opt:5. The groups are BRANCH=0, CMP=1, SET=2. The BRANCH sub-groups are GOTO=0, JUMP=1, PLAY=2. The JUMP options are JUMP_OBJECT=0, JUMP_TITLE=1, CALL_OBJECT=2, CALL_TITLE=3, RESUME=4. The PLAY options are PLAY_PL=0, PLAY_PL_PI=1, PLAY_PL_PM=2. In the SET group, sub-group SET=0, and set option MOVE=1. (Source: libbluray `hdmv_insn.h`.)

- [ ] **Step 1: Write the failing test**

`bluray/movieobject_test.go`:
```go
package bluray

import (
	"errors"
	"reflect"
	"testing"
)

func cmd(op, dst, src uint32) []byte { return cat(be32(op), be32(dst), be32(src)) }

func movieObjectBytes() []byte {
	objs := cat(
		zeros(4), be16(3),
		be16(0x8000), be16(3), // object 0: resume intention, 3 commands
		cmd(0x50400001, 5, 801), // MOVE r5, 801 (immediate source)
		cmd(0x22000000, 5, 0),   // PLAY_PL r5 (register operand)
		cmd(0x21800000, 1, 0),   // JUMP_OBJECT 1
		be16(0), be16(2), // object 1
		cmd(0x22800000, 800, 0), // PLAY_PL 800
		cmd(0x22800000, 801, 0), // PLAY_PL 801 (already seen from object 0)
		be16(0), be16(2), // object 2
		cmd(0x22810000, 5, 0), // PLAY_PL_PI 5
		cmd(0x21800000, 2, 0), // JUMP_OBJECT 2 (jumps to itself)
	)
	return cat([]byte("MOBJ0200"), be32(0), zeros(28), be32(uint32(len(objs))), objs)
}

func TestParseMovieObjects(t *testing.T) {
	m, err := ParseMovieObjects(movieObjectBytes())
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0200" || len(m.Objects) != 3 {
		t.Fatalf("version %q, %d objects", m.Version, len(m.Objects))
	}
	o := m.Objects[0]
	if !o.ResumeIntention || o.MenuCallMask || o.TitleSearchMask || len(o.Commands) != 3 {
		t.Fatalf("object 0 = %+v", o)
	}
	move := o.Commands[0]
	if move.Group() != 2 || move.SubGroup() != 0 || move.SetOpt() != 1 || !move.ImmSrc() || move.ImmDst() {
		t.Errorf("MOVE decoded as group %d sub %d set %d immSrc %v immDst %v",
			move.Group(), move.SubGroup(), move.SetOpt(), move.ImmSrc(), move.ImmDst())
	}
	play := o.Commands[1]
	if play.Group() != 0 || play.SubGroup() != 2 || play.BranchOpt() != 0 || play.ImmDst() {
		t.Errorf("PLAY_PL decoded as group %d sub %d opt %d immDst %v",
			play.Group(), play.SubGroup(), play.BranchOpt(), play.ImmDst())
	}
	if pi := m.Objects[2].Commands[0]; pi.BranchOpt() != 1 || !pi.ImmDst() || pi.Dst != 5 {
		t.Errorf("PLAY_PL_PI = %+v", pi)
	}
}

func TestPlaylists(t *testing.T) {
	m, err := ParseMovieObjects(movieObjectBytes())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		obj  int
		want []int
	}{
		{0, []int{801, 800}},
		{1, []int{800, 801}},
		{2, []int{5}},
		{9, nil},
		{-1, nil},
	}
	for _, tt := range tests {
		if got := m.Playlists(tt.obj); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Playlists(%d) = %v, want %v", tt.obj, got, tt.want)
		}
	}
}

func TestPlaylistsJumpCycle(t *testing.T) {
	jump := func(to uint32) NavCommand { return NavCommand{Opcode: 0x21800000, Dst: to} }
	m := &MovieObjects{Objects: []MovieObject{
		{Commands: []NavCommand{jump(1), {Opcode: 0x22800000, Dst: 10}}},
		{Commands: []NavCommand{jump(0), {Opcode: 0x22800000, Dst: 11}}},
	}}
	if got, want := m.Playlists(0), []int{11, 10}; !reflect.DeepEqual(got, want) {
		t.Errorf("Playlists(0) = %v, want %v", got, want)
	}
}

func TestPlaylistsUnknownRegisterIsSkipped(t *testing.T) {
	m := &MovieObjects{Objects: []MovieObject{{Commands: []NavCommand{
		{Opcode: 0x50400001, Dst: 3, Src: 900}, // MOVE r3, 900
		{Opcode: 0x50400003, Dst: 3, Src: 1},   // ADD r3, 1 (no longer a known constant)
		{Opcode: 0x22000000, Dst: 3},           // PLAY_PL r3
		{Opcode: 0x22000000, Dst: 4},           // PLAY_PL r4 (never set)
	}}}}
	if got := m.Playlists(0); got != nil {
		t.Errorf("Playlists(0) = %v, want nil", got)
	}
}

func TestParseMovieObjectsErrors(t *testing.T) {
	b := movieObjectBytes()
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-1],
		"magic":     append([]byte("INDX"), b[4:]...),
	} {
		if _, err := ParseMovieObjects(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func FuzzParseMovieObjects(f *testing.F) {
	f.Add(movieObjectBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := ParseMovieObjects(b)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unexpected error type: %v", err)
			}
			return
		}
		for i := range m.Objects {
			m.Playlists(i)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bluray/ -run 'MovieObject|Playlists'`
Expected: FAIL to compile (`undefined: ParseMovieObjects`).

- [ ] **Step 3: Implement**

`bluray/movieobject.go`:
```go
package bluray

// Navigation command fields (libbluray hdmv_insn.h).
const (
	groupBranch = 0
	groupSet    = 2

	branchJump = 1
	branchPlay = 2

	jumpObject = 0
	callObject = 2

	playPLPM = 2 // PLAY_PL=0, PLAY_PL_PI=1, PLAY_PL_PM=2

	setSet  = 0
	setMove = 1
)

// NavCommand is one 12-byte HDMV navigation command.
type NavCommand struct {
	Opcode uint32
	Dst    uint32
	Src    uint32
}

// Group returns the instruction group (0 branch, 1 compare, 2 set).
func (c NavCommand) Group() uint8 { return uint8(c.Opcode>>27) & 0x3 }

// SubGroup returns the instruction sub-group.
func (c NavCommand) SubGroup() uint8 { return uint8(c.Opcode>>24) & 0x7 }

// ImmDst reports whether Dst is an immediate value rather than a register.
func (c NavCommand) ImmDst() bool { return c.Opcode&(1<<23) != 0 }

// ImmSrc reports whether Src is an immediate value rather than a register.
func (c NavCommand) ImmSrc() bool { return c.Opcode&(1<<22) != 0 }

// BranchOpt returns the branch option of branch-group instructions.
func (c NavCommand) BranchOpt() uint8 { return uint8(c.Opcode>>16) & 0xF }

// SetOpt returns the set option of set-group instructions.
func (c NavCommand) SetOpt() uint8 { return uint8(c.Opcode) & 0x1F }

// MovieObject is one HDMV movie object.
type MovieObject struct {
	ResumeIntention bool
	MenuCallMask    bool
	TitleSearchMask bool
	Commands        []NavCommand
}

// MovieObjects is a parsed MovieObject.bdmv.
type MovieObjects struct {
	Version string
	Objects []MovieObject
}

// ParseMovieObjects parses the contents of BDMV/MovieObject.bdmv.
func ParseMovieObjects(b []byte) (*MovieObjects, error) {
	r := newReader(b)
	m := &MovieObjects{Version: r.header("MOBJ")}
	r.skip(4)  // extension data start address
	r.skip(28) // reserved
	mr := r.sub(int(r.u32()))
	mr.skip(4)
	n := int(mr.u16())
	for i := 0; i < n && mr.err() == nil; i++ {
		flags := mr.u16()
		obj := MovieObject{
			ResumeIntention: flags&0x8000 != 0,
			MenuCallMask:    flags&0x4000 != 0,
			TitleSearchMask: flags&0x2000 != 0,
		}
		nc := int(mr.u16())
		for j := 0; j < nc && mr.err() == nil; j++ {
			obj.Commands = append(obj.Commands, NavCommand{Opcode: mr.u32(), Dst: mr.u32(), Src: mr.u32()})
		}
		m.Objects = append(m.Objects, obj)
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return m, nil
}

// Playlists returns the playlist numbers that movie object objectID may
// play, in first-seen order without duplicates. It follows JumpObject and
// CallObject branches with immediate targets. A register operand is
// resolved only when an earlier Move in the same object loaded that
// register with a known value; anything else is skipped. Commands are
// scanned in order, ignoring conditional branches.
func (m *MovieObjects) Playlists(objectID int) []int {
	var out []int
	seen := map[int]bool{}
	visited := map[int]bool{}
	var walk func(id int)
	walk = func(id int) {
		if id < 0 || id >= len(m.Objects) || visited[id] {
			return
		}
		visited[id] = true
		regs := map[uint32]uint32{}
		for _, c := range m.Objects[id].Commands {
			switch {
			case c.Group() == groupSet && c.SubGroup() == setSet:
				v, ok := c.Src, c.ImmSrc()
				if !ok {
					v, ok = regs[c.Src]
				}
				if c.SetOpt() == setMove && ok {
					regs[c.Dst] = v
				} else {
					delete(regs, c.Dst)
				}
			case c.Group() == groupBranch && c.SubGroup() == branchPlay && c.BranchOpt() <= playPLPM:
				pl, ok := c.Dst, c.ImmDst()
				if !ok {
					pl, ok = regs[c.Dst]
				}
				if ok && !seen[int(pl)] {
					seen[int(pl)] = true
					out = append(out, int(pl))
				}
			case c.Group() == groupBranch && c.SubGroup() == branchJump && c.ImmDst() &&
				(c.BranchOpt() == jumpObject || c.BranchOpt() == callObject):
				walk(int(c.Dst))
			}
		}
	}
	walk(objectID)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./bluray/ -run 'MovieObject|Playlists' && go test ./bluray/ -run '^$' -fuzz FuzzParseMovieObjects -fuzztime 20s`
Expected: PASS; no fuzz failures.

- [ ] **Step 5: Commit**

```bash
git add bluray/movieobject.go bluray/movieobject_test.go
git commit -m "Add MovieObject.bdmv parser and playlist reachability"
```

---

### Task 4: Stream coding types and attribute parsing

**Files:**
- Create: `bluray/stream.go`
- Test: `bluray/stream_test.go`

**Interfaces:**
- Consumes: `reader` (Task 1).
- Produces:
  - `type CodingType uint8` with the constants `CodingMPEG1Video` (0x01), `CodingMPEG2Video` (0x02), `CodingMPEG1Audio` (0x03), `CodingMPEG2Audio` (0x04), `CodingAVC` (0x1B), `CodingMVC` (0x20), `CodingHEVC` (0x24), `CodingLPCM` (0x80), `CodingAC3` (0x81), `CodingDTS` (0x82), `CodingTrueHD` (0x83), `CodingEAC3` (0x84), `CodingDTSHDHR` (0x85), `CodingDTSHDMA` (0x86), `CodingPG` (0x90), `CodingIG` (0x91), `CodingTextST` (0x92), `CodingEAC3Secondary` (0xA1), `CodingDTSHDSecondary` (0xA2) and `CodingVC1` (0xEA). Methods: `Kind() StreamKind`, `String() string`.
  - `type StreamKind uint8`: `KindUnknown`, `KindVideo`, `KindAudio`, `KindPG`, `KindIG`, `KindText`
  - `type VideoFormat uint8`, `type FrameRate uint8`, `type DynamicRange uint8`, `type AudioFormat uint8`, `type SampleRate uint8`, each with `String() string`
  - `type Stream struct { PID uint16; Coding CodingType; VideoFormat VideoFormat; FrameRate FrameRate; DynamicRange DynamicRange; AudioFormat AudioFormat; SampleRate SampleRate; Language string }`
  - unexported `parseAttributes(r *reader, s *Stream, clip bool)`, which reads one length-prefixed `stream_attributes` block (MPLS STN, `clip == false`) or `StreamCodingInfo` block (CLPI, `clip == true`)

- [ ] **Step 1: Write the failing test**

`bluray/stream_test.go`:
```go
package bluray

import (
	"reflect"
	"testing"
)

func TestParseAttributes(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		clip bool
		want Stream
	}{
		{"avc", []byte{2, 0x1B, 0x61}, false,
			Stream{Coding: CodingAVC, VideoFormat: 6, FrameRate: 1}},
		{"hevc-mpls", []byte{3, 0x24, 0x81, 0x10}, false,
			Stream{Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 1}},
		{"hevc-clpi", []byte{4, 0x24, 0x81, 0x30, 0x20}, true,
			Stream{Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 2}},
		{"hevc-short", []byte{2, 0x24, 0x81}, false,
			Stream{Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1}},
		{"truehd", []byte{5, 0x83, 0x61, 'e', 'n', 'g'}, false,
			Stream{Coding: CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"}},
		{"pgs", []byte{4, 0x90, 'f', 'r', 'a'}, false,
			Stream{Coding: CodingPG, Language: "fra"}},
		{"text", []byte{5, 0x92, 0x01, 'j', 'p', 'n'}, false,
			Stream{Coding: CodingTextST, Language: "jpn"}},
		{"unknown", []byte{3, 0x77, 0xFF, 0xFF}, false,
			Stream{Coding: 0x77}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newReader(append(tt.in, 0xEE))
			var s Stream
			parseAttributes(r, &s, tt.clip)
			if r.err() != nil {
				t.Fatal(r.err())
			}
			if !reflect.DeepEqual(s, tt.want) {
				t.Errorf("got %+v, want %+v", s, tt.want)
			}
			if got := r.u8(); got != 0xEE {
				t.Errorf("reader not positioned after block: next byte %#x", got)
			}
		})
	}
}

func TestParseAttributesSkipsPadding(t *testing.T) {
	r := newReader([]byte{8, 0x81, 0x31, 'e', 'n', 'g', 0, 0, 0, 0xEE})
	var s Stream
	parseAttributes(r, &s, false)
	if s.Coding != CodingAC3 || s.Language != "eng" || r.u8() != 0xEE {
		t.Errorf("padding not skipped: %+v", s)
	}
}

func TestStreamNames(t *testing.T) {
	tests := []struct{ got, want string }{
		{CodingTrueHD.String(), "TrueHD"},
		{CodingHEVC.String(), "HEVC"},
		{CodingType(0x77).String(), "0x77"},
		{VideoFormat(8).String(), "2160p"},
		{VideoFormat(15).String(), "unknown(15)"},
		{FrameRate(1).String(), "23.976"},
		{DynamicRange(1).String(), "HDR10"},
		{AudioFormat(6).String(), "multi-channel"},
		{SampleRate(5).String(), "192 kHz"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestCodingKind(t *testing.T) {
	tests := map[CodingType]StreamKind{
		CodingAVC: KindVideo, CodingVC1: KindVideo, CodingHEVC: KindVideo,
		CodingAC3: KindAudio, CodingDTSHDMA: KindAudio, CodingLPCM: KindAudio,
		CodingPG: KindPG, CodingIG: KindIG, CodingTextST: KindText, 0x77: KindUnknown,
	}
	for c, want := range tests {
		if got := c.Kind(); got != want {
			t.Errorf("%v.Kind() = %d, want %d", c, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bluray/ -run 'Attributes|StreamNames|CodingKind'`
Expected: FAIL to compile (`undefined: Stream`).

- [ ] **Step 3: Implement**

`bluray/stream.go`:
```go
package bluray

import "fmt"

// CodingType is a stream_coding_type value.
type CodingType uint8

// Coding types used on Blu-ray discs.
const (
	CodingMPEG1Video     CodingType = 0x01
	CodingMPEG2Video     CodingType = 0x02
	CodingMPEG1Audio     CodingType = 0x03
	CodingMPEG2Audio     CodingType = 0x04
	CodingAVC            CodingType = 0x1B
	CodingMVC            CodingType = 0x20
	CodingHEVC           CodingType = 0x24
	CodingLPCM           CodingType = 0x80
	CodingAC3            CodingType = 0x81
	CodingDTS            CodingType = 0x82
	CodingTrueHD         CodingType = 0x83
	CodingEAC3           CodingType = 0x84
	CodingDTSHDHR        CodingType = 0x85
	CodingDTSHDMA        CodingType = 0x86
	CodingPG             CodingType = 0x90
	CodingIG             CodingType = 0x91
	CodingTextST         CodingType = 0x92
	CodingEAC3Secondary  CodingType = 0xA1
	CodingDTSHDSecondary CodingType = 0xA2
	CodingVC1            CodingType = 0xEA
)

// StreamKind groups coding types by elementary stream category.
type StreamKind uint8

// Stream kinds.
const (
	KindUnknown StreamKind = iota
	KindVideo
	KindAudio
	KindPG // presentation graphics (PGS subtitles)
	KindIG // interactive graphics (menus)
	KindText
)

// Kind returns the stream category of c.
func (c CodingType) Kind() StreamKind {
	switch c {
	case CodingMPEG1Video, CodingMPEG2Video, CodingAVC, CodingMVC, CodingHEVC, CodingVC1:
		return KindVideo
	case CodingMPEG1Audio, CodingMPEG2Audio, CodingLPCM, CodingAC3, CodingDTS, CodingTrueHD,
		CodingEAC3, CodingDTSHDHR, CodingDTSHDMA, CodingEAC3Secondary, CodingDTSHDSecondary:
		return KindAudio
	case CodingPG:
		return KindPG
	case CodingIG:
		return KindIG
	case CodingTextST:
		return KindText
	}
	return KindUnknown
}

var codingNames = map[CodingType]string{
	CodingMPEG1Video: "MPEG-1 Video", CodingMPEG2Video: "MPEG-2 Video",
	CodingMPEG1Audio: "MPEG-1 Audio", CodingMPEG2Audio: "MPEG-2 Audio",
	CodingAVC: "H.264/AVC", CodingMVC: "H.264/MVC", CodingHEVC: "HEVC", CodingVC1: "VC-1",
	CodingLPCM: "LPCM", CodingAC3: "AC-3", CodingDTS: "DTS", CodingTrueHD: "TrueHD",
	CodingEAC3: "E-AC-3", CodingDTSHDHR: "DTS-HD HR", CodingDTSHDMA: "DTS-HD MA",
	CodingEAC3Secondary: "E-AC-3 (secondary)", CodingDTSHDSecondary: "DTS-HD (secondary)",
	CodingPG: "PGS", CodingIG: "IGS", CodingTextST: "Text subtitle",
}

func (c CodingType) String() string {
	if n, ok := codingNames[c]; ok {
		return n
	}
	return fmt.Sprintf("0x%02X", uint8(c))
}

// VideoFormat is the video_format field (resolution and scan type).
type VideoFormat uint8

// FrameRate is the frame_rate field.
type FrameRate uint8

// DynamicRange is the dynamic_range_type field of HEVC streams.
type DynamicRange uint8

// AudioFormat is the audio_presentation_type field.
type AudioFormat uint8

// SampleRate is the sampling_frequency field.
type SampleRate uint8

func enumName[T ~uint8](names map[T]string, v T) string {
	if n, ok := names[v]; ok {
		return n
	}
	return fmt.Sprintf("unknown(%d)", uint8(v))
}

var (
	videoFormatNames = map[VideoFormat]string{
		1: "480i", 2: "576i", 3: "480p", 4: "1080i", 5: "720p", 6: "1080p", 7: "576p", 8: "2160p",
	}
	frameRateNames = map[FrameRate]string{
		1: "23.976", 2: "24", 3: "25", 4: "29.97", 6: "50", 7: "59.94",
	}
	dynamicRangeNames = map[DynamicRange]string{0: "SDR", 1: "HDR10", 2: "Dolby Vision"}
	audioFormatNames  = map[AudioFormat]string{
		1: "mono", 3: "stereo", 6: "multi-channel", 12: "stereo + multi-channel",
	}
	sampleRateNames = map[SampleRate]string{
		1: "48 kHz", 4: "96 kHz", 5: "192 kHz", 12: "48/192 kHz", 14: "48/96 kHz",
	}
)

func (v VideoFormat) String() string  { return enumName(videoFormatNames, v) }
func (v FrameRate) String() string    { return enumName(frameRateNames, v) }
func (v DynamicRange) String() string { return enumName(dynamicRangeNames, v) }
func (v AudioFormat) String() string  { return enumName(audioFormatNames, v) }
func (v SampleRate) String() string   { return enumName(sampleRateNames, v) }

// Stream describes one elementary stream from a playlist STN table or a
// clip's program info. Fields that do not apply to the stream kind are zero.
type Stream struct {
	PID          uint16
	Coding       CodingType
	VideoFormat  VideoFormat
	FrameRate    FrameRate
	DynamicRange DynamicRange
	AudioFormat  AudioFormat
	SampleRate   SampleRate
	Language     string // ISO 639-2 code for audio and subtitle streams
}

// parseAttributes reads a length-prefixed stream attribute block: MPLS
// stream_attributes when clip is false, CLPI StreamCodingInfo when clip is
// true. The layouts differ only for HEVC video, where CLPI has an extra
// byte (aspect ratio and flags) before the dynamic range (libbluray
// mpls_parse.c and clpi_parse.c).
func parseAttributes(r *reader, s *Stream, clip bool) {
	ar := r.sub(int(r.u8()))
	s.Coding = CodingType(ar.u8())
	switch s.Coding.Kind() {
	case KindVideo:
		b := ar.u8()
		s.VideoFormat, s.FrameRate = VideoFormat(b>>4), FrameRate(b&0x0F)
		if s.Coding != CodingHEVC {
			return
		}
		if clip {
			if ar.remaining() < 2 {
				return
			}
			ar.skip(1) // aspect ratio, oc_flag, cr_flag
		}
		if ar.remaining() >= 1 {
			s.DynamicRange = DynamicRange(ar.u8() >> 4)
		}
	case KindAudio:
		b := ar.u8()
		s.AudioFormat, s.SampleRate = AudioFormat(b>>4), SampleRate(b&0x0F)
		s.Language = ar.str(3)
	case KindPG, KindIG:
		s.Language = ar.str(3)
	case KindText:
		ar.skip(1) // character code
		s.Language = ar.str(3)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./bluray/ -run 'Attributes|StreamNames|CodingKind'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add bluray/stream.go bluray/stream_test.go
git commit -m "Add stream coding types and attribute parsing"
```

---

### Task 5: MPLS playlist parser

**Files:**
- Create: `bluray/playlist.go`
- Test: `bluray/playlist_test.go`

**Interfaces:**
- Consumes: `reader`, `Ticks`, `ticksDuration` (Task 1); `Stream`, `parseAttributes` (Task 4).
- Produces:
  - `type STN struct { Video, Audio, PG []Stream }`
  - `type PlayItem struct { ClipID, CodecID string; ConnectionCondition, STCID uint8; In, Out Ticks; StillMode uint8; Angles []string; STN STN }`, where `Angles` holds the clip IDs of angles 2..n and is empty for single-angle items. Method: `Duration() time.Duration`.
  - `type MarkType uint8`; `MarkEntry MarkType = 1`, `MarkLink MarkType = 2`
  - `type Mark struct { Type MarkType; PlayItem uint16; Time Ticks; PID uint16; Duration Ticks }`
  - `type Playlist struct { Version string; Items []PlayItem; Marks []Mark }`, with the methods `Duration() time.Duration` and `Chapters() []time.Duration`
  - `func ParsePlaylist(b []byte) (*Playlist, error)`, which rejects items with `Out < In`

- [ ] **Step 1: Write the failing test**

`bluray/playlist_test.go`:
```go
package bluray

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// streamEntry builds an STN entry: a type-1 stream_entry (main clip PID)
// followed by its stream_attributes.
func streamEntry(pid uint16, attrs ...byte) []byte {
	return cat([]byte{9, 1}, be16(pid), zeros(6), []byte{byte(len(attrs))}, attrs)
}

func stnBytes() []byte {
	body := cat(
		zeros(2),
		[]byte{1, 2, 1, 0, 0, 0, 0}, // video, audio, PG, IG, secondary audio, secondary video, PiP PG
		zeros(5),
		streamEntry(0x1011, 0x1B, 0x61),                // AVC 1080p 23.976
		streamEntry(0x1100, 0x83, 0x61, 'e', 'n', 'g'), // TrueHD multi-channel 48 kHz
		streamEntry(0x1101, 0x81, 0x31, 'f', 'r', 'a'), // AC-3 stereo 48 kHz
		streamEntry(0x1200, 0x90, 'e', 'n', 'g'),       // PGS
	)
	return cat(be16(uint16(len(body))), body)
}

func playItemBytes(clip string, in, out uint32, angles []string) []byte {
	flags := uint16(1) // connection condition 1
	var angleData []byte
	if len(angles) > 0 {
		flags |= 0x10
		angleData = []byte{byte(len(angles) + 1), 0}
		for _, a := range angles {
			angleData = cat(angleData, []byte(a), []byte("M2TS"), zeros(1))
		}
	}
	body := cat(
		[]byte(clip), []byte("M2TS"), be16(flags), []byte{0},
		be32(in), be32(out),
		zeros(8),    // UO mask
		[]byte{0, 0}, // random access flag, still mode
		be16(0),     // still time
		angleData, stnBytes(),
	)
	return cat(be16(uint16(len(body))), body)
}

func markBytes(typ byte, item uint16, ticks uint32) []byte {
	return cat([]byte{0, typ}, be16(item), be32(ticks), be16(0xFFFF), be32(0))
}

func buildPlaylist(items, marks [][]byte) []byte {
	pl := cat(zeros(2), be16(uint16(len(items))), be16(0), cat(items...))
	mk := cat(be16(uint16(len(marks))), cat(marks...))
	appInfo := cat(be32(14), zeros(14))
	plStart := 40 + len(appInfo)
	markStart := plStart + 4 + len(pl)
	head := cat([]byte("MPLS0300"), be32(uint32(plStart)), be32(uint32(markStart)), be32(0), zeros(20))
	return cat(head, appInfo, be32(uint32(len(pl))), pl, be32(uint32(len(mk))), mk)
}

const minute = 45000 * 60

func playlistBytes() []byte {
	return buildPlaylist(
		[][]byte{
			playItemBytes("00001", 900000, 900000+10*minute, []string{"00002"}),
			playItemBytes("00003", 0, 5*minute, nil),
		},
		[][]byte{
			markBytes(1, 0, 900000),
			markBytes(1, 0, 900000+minute),
			markBytes(2, 0, 900000+minute+minute/2), // link point, not a chapter
			markBytes(1, 1, 0),
		},
	)
}

func wantSTN() STN {
	return STN{
		Video: []Stream{{PID: 0x1011, Coding: CodingAVC, VideoFormat: 6, FrameRate: 1}},
		Audio: []Stream{
			{PID: 0x1100, Coding: CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1101, Coding: CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "fra"},
		},
		PG: []Stream{{PID: 0x1200, Coding: CodingPG, Language: "eng"}},
	}
}

func TestParsePlaylist(t *testing.T) {
	p, err := ParsePlaylist(playlistBytes())
	if err != nil {
		t.Fatal(err)
	}
	want := &Playlist{
		Version: "0300",
		Items: []PlayItem{
			{ClipID: "00001", CodecID: "M2TS", ConnectionCondition: 1, In: 900000, Out: 900000 + 10*minute,
				Angles: []string{"00002"}, STN: wantSTN()},
			{ClipID: "00003", CodecID: "M2TS", ConnectionCondition: 1, In: 0, Out: 5 * minute, STN: wantSTN()},
		},
		Marks: []Mark{
			{Type: MarkEntry, PlayItem: 0, Time: 900000, PID: 0xFFFF},
			{Type: MarkEntry, PlayItem: 0, Time: 900000 + minute, PID: 0xFFFF},
			{Type: MarkLink, PlayItem: 0, Time: 900000 + minute + minute/2, PID: 0xFFFF},
			{Type: MarkEntry, PlayItem: 1, Time: 0, PID: 0xFFFF},
		},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("ParsePlaylist =\n%+v\nwant\n%+v", p, want)
	}
	if got := p.Duration(); got != 15*time.Minute {
		t.Errorf("Duration = %v", got)
	}
	if got := p.Items[1].Duration(); got != 5*time.Minute {
		t.Errorf("item 1 Duration = %v", got)
	}
	if got, want := p.Chapters(), []time.Duration{0, time.Minute, 10 * time.Minute}; !reflect.DeepEqual(got, want) {
		t.Errorf("Chapters = %v, want %v", got, want)
	}
}

func TestParsePlaylistRejectsOutBeforeIn(t *testing.T) {
	b := buildPlaylist([][]byte{playItemBytes("00001", 100, 50, nil)}, nil)
	if _, err := ParsePlaylist(b); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestParsePlaylistErrors(t *testing.T) {
	b := playlistBytes()
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-3],
		"magic":     append([]byte("HDMV"), b[4:]...),
		"short":     b[:20],
	} {
		if _, err := ParsePlaylist(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestChaptersIgnoresBadMarks(t *testing.T) {
	p := &Playlist{
		Items: []PlayItem{
			{In: 0, Out: 100 * 45000},
			{In: 1000, Out: 1000 + 50*45000},
		},
		Marks: []Mark{
			{Type: MarkEntry, PlayItem: 0, Time: 0},
			{Type: MarkEntry, PlayItem: 0, Time: 0},             // duplicate
			{Type: MarkLink, PlayItem: 0, Time: 45000},          // not an entry mark
			{Type: MarkEntry, PlayItem: 7, Time: 0},             // no such play item
			{Type: MarkEntry, PlayItem: 1, Time: 500},           // before the item's IN time
			{Type: MarkEntry, PlayItem: 1, Time: 1000 + 51*45000}, // after the item's OUT time
			{Type: MarkEntry, PlayItem: 1, Time: 1000 + 10*45000},
		},
	}
	if got, want := p.Chapters(), []time.Duration{0, 110 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Errorf("Chapters = %v, want %v", got, want)
	}
}

func FuzzParsePlaylist(f *testing.F) {
	f.Add(playlistBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParsePlaylist(b)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unexpected error type: %v", err)
			}
			return
		}
		if p.Duration() < 0 {
			t.Fatal("negative duration")
		}
		p.Chapters()
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bluray/ -run 'Playlist|Chapters'`
Expected: FAIL to compile (`undefined: ParsePlaylist`).

- [ ] **Step 3: Implement**

`bluray/playlist.go`:
```go
package bluray

import "time"

// STN is a play item's stream number table: the streams a player may
// select, in stream-number order. Interactive graphics and secondary
// streams are not decoded.
type STN struct {
	Video []Stream
	Audio []Stream
	PG    []Stream
}

// PlayItem is one clip segment of a playlist.
type PlayItem struct {
	ClipID              string // "00001" → CLIPINF/00001.clpi, STREAM/00001.m2ts
	CodecID             string // "M2TS"
	ConnectionCondition uint8
	STCID               uint8
	In, Out             Ticks
	StillMode           uint8
	Angles              []string // clip IDs of angles 2..n; empty for single-angle items
	STN                 STN
}

// Duration returns the length of the play item.
func (p PlayItem) Duration() time.Duration { return ticksDuration(span(p)) }

func span(p PlayItem) uint64 {
	if p.Out < p.In {
		return 0
	}
	return uint64(p.Out - p.In)
}

// MarkType distinguishes chapter entry marks from link points.
type MarkType uint8

// Mark types.
const (
	MarkEntry MarkType = 1
	MarkLink  MarkType = 2
)

// Mark is a playlist mark.
type Mark struct {
	Type     MarkType
	PlayItem uint16
	Time     Ticks // on the referenced play item's clock
	PID      uint16
	Duration Ticks
}

// Playlist is a parsed MPLS file.
type Playlist struct {
	Version string
	Items   []PlayItem
	Marks   []Mark
}

// Duration returns the summed length of all play items.
func (p *Playlist) Duration() time.Duration {
	var t uint64
	for _, it := range p.Items {
		t += span(it)
	}
	return ticksDuration(t)
}

// Chapters returns the start of each entry mark relative to the start of
// the playlist, ascending and without duplicates. Marks that reference a
// missing play item or fall outside their item's IN/OUT range are ignored.
func (p *Playlist) Chapters() []time.Duration {
	starts := make([]uint64, len(p.Items))
	var t uint64
	for i, it := range p.Items {
		starts[i] = t
		t += span(it)
	}
	var out []time.Duration
	for _, m := range p.Marks {
		if m.Type != MarkEntry || int(m.PlayItem) >= len(p.Items) {
			continue
		}
		it := p.Items[m.PlayItem]
		if m.Time < it.In || m.Time > it.Out {
			continue
		}
		d := ticksDuration(starts[m.PlayItem] + uint64(m.Time-it.In))
		if len(out) > 0 && d <= out[len(out)-1] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// ParsePlaylist parses the contents of a BDMV/PLAYLIST/*.mpls file.
func ParsePlaylist(b []byte) (*Playlist, error) {
	r := newReader(b)
	p := &Playlist{Version: r.header("MPLS")}
	plStart := r.u32()
	markStart := r.u32()

	r.seek(int(plStart))
	pr := r.sub(int(r.u32()))
	pr.skip(2)
	nItems := int(pr.u16())
	pr.skip(2) // number of sub-paths
	for i := 0; i < nItems && pr.err() == nil; i++ {
		p.Items = append(p.Items, parsePlayItem(pr))
	}

	r.seek(int(markStart))
	mr := r.sub(int(r.u32()))
	nMarks := int(mr.u16())
	for i := 0; i < nMarks && mr.err() == nil; i++ {
		mr.skip(1)
		p.Marks = append(p.Marks, Mark{
			Type:     MarkType(mr.u8()),
			PlayItem: mr.u16(),
			Time:     Ticks(mr.u32()),
			PID:      mr.u16(),
			Duration: Ticks(mr.u32()),
		})
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return p, nil
}

func parsePlayItem(r *reader) PlayItem {
	pr := r.sub(int(r.u16()))
	var pi PlayItem
	pi.ClipID = pr.str(5)
	pi.CodecID = pr.str(4)
	flags := pr.u16()
	multiAngle := flags&0x10 != 0
	pi.ConnectionCondition = uint8(flags & 0x0F)
	pi.STCID = pr.u8()
	pi.In = Ticks(pr.u32())
	pi.Out = Ticks(pr.u32())
	if pr.err() == nil && pi.Out < pi.In {
		pr.fail("play item %s: OUT time %d before IN time %d", pi.ClipID, pi.Out, pi.In)
	}
	pr.skip(8) // UO mask table
	pr.skip(1) // random access flag, reserved
	pi.StillMode = pr.u8()
	pr.skip(2) // still time or reserved
	if multiAngle {
		n := int(pr.u8())
		pr.skip(1) // is_different_audios, is_seamless_angle_change
		for i := 1; i < n && pr.err() == nil; i++ {
			pi.Angles = append(pi.Angles, pr.str(5))
			pr.skip(4 + 1) // codec identifier, ref_to_STC_id
		}
	}
	pi.STN = parseSTN(pr)
	return pi
}

func parseSTN(r *reader) STN {
	sr := r.sub(int(r.u16()))
	sr.skip(2)
	nVideo, nAudio, nPG := int(sr.u8()), int(sr.u8()), int(sr.u8())
	sr.skip(4) // IG, secondary audio, secondary video, PiP PG counts
	sr.skip(5)
	read := func(n int) []Stream {
		var out []Stream
		for i := 0; i < n && sr.err() == nil; i++ {
			out = append(out, parseStreamEntry(sr))
		}
		return out
	}
	var stn STN
	stn.Video = read(nVideo)
	stn.Audio = read(nAudio)
	stn.PG = read(nPG)
	return stn
}

// parseStreamEntry reads a stream_entry (which locates the stream's PID)
// followed by its stream_attributes.
func parseStreamEntry(r *reader) Stream {
	var s Stream
	er := r.sub(int(r.u8()))
	switch er.u8() {
	case 1: // stream in the main clip
		s.PID = er.u16()
	case 2, 4: // sub-path, sub-clip entry
		er.skip(2)
		s.PID = er.u16()
	case 3: // sub-path
		er.skip(1)
		s.PID = er.u16()
	}
	parseAttributes(r, &s, false)
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./bluray/ -run 'Playlist|Chapters' && go test ./bluray/ -run '^$' -fuzz FuzzParsePlaylist -fuzztime 30s`
Expected: PASS; no fuzz failures.

- [ ] **Step 5: Commit**

```bash
git add bluray/playlist.go bluray/playlist_test.go
git commit -m "Add MPLS playlist parser with durations and chapters"
```

---

### Task 6: CLPI clip information parser

**Files:**
- Create: `bluray/clip.go`
- Test: `bluray/clip_test.go`

**Interfaces:**
- Consumes: `reader` (Task 1); `Stream`, `parseAttributes` (Task 4).
- Produces:
  - `type Program struct { SPNStart uint32; PMTPID uint16; Streams []Stream }`
  - `type Clip struct { Version string; StreamType, ApplicationType uint8; TSRecordingRate, SourcePackets uint32; Programs []Program }` with the method `Size() int64` (source packets × 192)
  - `func ParseClip(b []byte) (*Clip, error)`

- [ ] **Step 1: Write the failing test**

`bluray/clip_test.go`:
```go
package bluray

import (
	"errors"
	"reflect"
	"testing"
)

func clipBytes() []byte {
	clipInfo := cat(zeros(2), []byte{1, 1}, zeros(4), be32(48_000_000), be32(1000), zeros(128), be16(0))
	program := cat(
		zeros(1), []byte{1}, // reserved, number of programs
		be32(0), be16(0x0100), []byte{3, 0}, // SPN start, PMT PID, streams, groups
		be16(0x1011), []byte{4, 0x24, 0x81, 0x30, 0x10}, // HEVC 2160p 23.976, HDR10
		be16(0x1100), []byte{5, 0x86, 0x61, 'e', 'n', 'g'}, // DTS-HD MA multi-channel 48 kHz
		be16(0x1200), []byte{4, 0x90, 'e', 'n', 'g'}, // PGS
	)
	seq := cat(be32(2), zeros(2))
	seqStart := 40 + 4 + len(clipInfo)
	progStart := seqStart + len(seq)
	head := cat([]byte("HDMV0300"), be32(uint32(seqStart)), be32(uint32(progStart)), zeros(12), zeros(12))
	return cat(head, be32(uint32(len(clipInfo))), clipInfo, seq, be32(uint32(len(program))), program)
}

func TestParseClip(t *testing.T) {
	c, err := ParseClip(clipBytes())
	if err != nil {
		t.Fatal(err)
	}
	want := &Clip{
		Version: "0300", StreamType: 1, ApplicationType: 1,
		TSRecordingRate: 48_000_000, SourcePackets: 1000,
		Programs: []Program{{
			PMTPID: 0x0100,
			Streams: []Stream{
				{PID: 0x1011, Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 1},
				{PID: 0x1100, Coding: CodingDTSHDMA, AudioFormat: 6, SampleRate: 1, Language: "eng"},
				{PID: 0x1200, Coding: CodingPG, Language: "eng"},
			},
		}},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("ParseClip =\n%+v\nwant\n%+v", c, want)
	}
	if c.Size() != 192000 {
		t.Errorf("Size = %d", c.Size())
	}
}

func TestParseClipErrors(t *testing.T) {
	b := clipBytes()
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-2],
		"magic":     append([]byte("MPLS"), b[4:]...),
	} {
		if _, err := ParseClip(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func FuzzParseClip(f *testing.F) {
	f.Add(clipBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		if _, err := ParseClip(b); err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected error type: %v", err)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bluray/ -run 'Clip'`
Expected: FAIL to compile (`undefined: ParseClip`).

- [ ] **Step 3: Implement**

`bluray/clip.go`:
```go
package bluray

// Program is one program sequence of a clip.
type Program struct {
	SPNStart uint32
	PMTPID   uint16
	Streams  []Stream
}

// Clip is a parsed CLPI (clip information) file.
type Clip struct {
	Version         string
	StreamType      uint8
	ApplicationType uint8
	TSRecordingRate uint32
	SourcePackets   uint32
	Programs        []Program
}

// Size returns the clip's M2TS size in bytes (192-byte source packets).
func (c *Clip) Size() int64 { return int64(c.SourcePackets) * 192 }

// ParseClip parses the contents of a BDMV/CLIPINF/*.clpi file.
func ParseClip(b []byte) (*Clip, error) {
	r := newReader(b)
	c := &Clip{Version: r.header("HDMV")}
	r.skip(4) // sequence info start address
	progStart := r.u32()

	r.seek(40) // ClipInfo follows the fixed-size header
	cr := r.sub(int(r.u32()))
	cr.skip(2)
	c.StreamType = cr.u8()
	c.ApplicationType = cr.u8()
	cr.skip(4) // reserved, is_ATC_delta
	c.TSRecordingRate = cr.u32()
	c.SourcePackets = cr.u32()

	r.seek(int(progStart))
	pr := r.sub(int(r.u32()))
	pr.skip(1)
	n := int(pr.u8())
	for i := 0; i < n && pr.err() == nil; i++ {
		p := Program{SPNStart: pr.u32(), PMTPID: pr.u16()}
		ns := int(pr.u8())
		pr.skip(1) // number of groups
		for j := 0; j < ns && pr.err() == nil; j++ {
			s := Stream{PID: pr.u16()}
			parseAttributes(pr, &s, true)
			p.Streams = append(p.Streams, s)
		}
		c.Programs = append(c.Programs, p)
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return c, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./bluray/ -run 'Clip' && go test ./bluray/ -run '^$' -fuzz FuzzParseClip -fuzztime 20s`
Expected: PASS; no fuzz failures.

- [ ] **Step 5: Commit**

```bash
git add bluray/clip.go bluray/clip_test.go
git commit -m "Add CLPI clip information parser"
```

---

### Task 7: Disc library metadata (bdmt) parser

**Files:**
- Create: `bluray/meta.go`
- Test: `bluray/meta_test.go`

**Interfaces:**
- Consumes: `ErrInvalid` (Task 1).
- Produces:
  - `type DiscMeta struct { Title, Language string }`
  - `func ParseMeta(b []byte) (*DiscMeta, error)` for `BDMV/META/DL/bdmt_*.xml`
  - `func PickMetaFile(names []string) string`, which returns `"bdmt_eng.xml"` when present, else the lexically first `bdmt_*.xml`, else `""`

- [ ] **Step 1: Write the failing test**

`bluray/meta_test.go`:
```go
package bluray

import (
	"errors"
	"testing"
)

const sampleMeta = `<?xml version="1.0" encoding="utf-8"?>
<disclib xmlns="urn:BDA:bdmv;disclib" xmlns:di="urn:BDA:bdmv;discinfo">
  <di:discinfo>
    <di:date>2026-01-01</di:date>
    <di:title>
      <di:name> Big Buck Bunny </di:name>
      <di:numSets>1</di:numSets>
    </di:title>
    <di:description><di:thumbnail href="thumb.jpg"/></di:description>
    <di:language>eng</di:language>
  </di:discinfo>
</disclib>`

func TestParseMeta(t *testing.T) {
	m, err := ParseMeta([]byte(sampleMeta))
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Big Buck Bunny" || m.Language != "eng" {
		t.Errorf("ParseMeta = %+v", m)
	}
}

func TestParseMetaInvalid(t *testing.T) {
	if _, err := ParseMeta([]byte("<disclib><unclosed>")); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestPickMetaFile(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"bdmt_fra.xml", "bdmt_eng.xml"}, "bdmt_eng.xml"},
		{[]string{"thumb.jpg", "bdmt_jpn.xml", "bdmt_fra.xml"}, "bdmt_fra.xml"},
		{[]string{"thumb.jpg"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := PickMetaFile(tt.in); got != tt.want {
			t.Errorf("PickMetaFile(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./bluray/ -run 'Meta'`
Expected: FAIL to compile (`undefined: ParseMeta`).

- [ ] **Step 3: Implement**

`bluray/meta.go`:
```go
package bluray

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// DiscMeta is the disc title information from a disc library metadata
// file (BDMV/META/DL/bdmt_<lang>.xml).
type DiscMeta struct {
	Title    string
	Language string
}

// ParseMeta parses a bdmt_*.xml file.
func ParseMeta(b []byte) (*DiscMeta, error) {
	var doc struct {
		XMLName  xml.Name `xml:"disclib"`
		DiscInfo struct {
			Title struct {
				Name string `xml:"name"`
			} `xml:"title"`
			Language string `xml:"language"`
		} `xml:"discinfo"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: disc library metadata: %v", ErrInvalid, err)
	}
	return &DiscMeta{
		Title:    strings.TrimSpace(doc.DiscInfo.Title.Name),
		Language: strings.TrimSpace(doc.DiscInfo.Language),
	}, nil
}

// PickMetaFile chooses which metadata file to read from the names in
// BDMV/META/DL: English if present, otherwise the lexically first
// bdmt_*.xml, otherwise "".
func PickMetaFile(names []string) string {
	best := ""
	for _, n := range names {
		if !strings.HasPrefix(n, "bdmt_") || !strings.HasSuffix(n, ".xml") {
			continue
		}
		if n == "bdmt_eng.xml" {
			return n
		}
		if best == "" || n < best {
			best = n
		}
	}
	return best
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./bluray/ && go vet ./... && golangci-lint run`
Expected: PASS; no findings.

- [ ] **Step 5: Commit**

```bash
git add bluray/meta.go bluray/meta_test.go
git commit -m "Add disc library metadata parser"
```

---

### Task 8: Synthetic Blu-ray disc builder (`internal/testdisc`)

**Files:**
- Create: `internal/testdisc/encode.go`, `internal/testdisc/disc.go`
- Test: `internal/testdisc/testdisc_test.go`

**Interfaces:**
- Consumes: every `bluray` type and parser from Tasks 2–7.
- Produces (package `testdisc`, used by milestone 2 ranking and source tests):
  - `func Index(idx *bluray.Index) []byte`
  - `func MovieObjects(m *bluray.MovieObjects) []byte`
  - `func Playlist(p *bluray.Playlist) []byte`
  - `func Clip(c *bluray.Clip) []byte`
  - `func Meta(title, lang string) []byte`
  - `func CleanM2TS(units int) []byte`: unencrypted aligned units, 6144 bytes each
  - command constructors returning `bluray.NavCommand`: `CmdPlayPL(pl uint32)`, `CmdPlayPLReg(reg uint32)`, `CmdMove(reg, value uint32)`, `CmdJumpObject(id uint32)`, `CmdJumpTitle(title uint32)`
  - `type Disc struct { FirstPlayback, TopMenu bluray.Object; Titles []bluray.IndexTitle; MovieObjects []bluray.MovieObject; Playlists map[string]*bluray.Playlist; Clips map[string]*bluray.Clip; ClipData map[string][]byte; MetaTitle string }`
  - `func (d *Disc) Files() map[string][]byte`. The keys are slash paths: `BDMV/index.bdmv`, `BDMV/MovieObject.bdmv`, `BDMV/PLAYLIST/<id>.mpls`, `BDMV/CLIPINF/<id>.clpi`, `BDMV/STREAM/<id>.m2ts`, and `BDMV/META/DL/bdmt_eng.xml` when `MetaTitle != ""`.
  - `func (d *Disc) MapFS() fstest.MapFS`
  - `func (d *Disc) WriteDir(dir string) error`
- Encoders write version `"0200"` when `Version == ""` and codec `"M2TS"` when `CodecID == ""`. Stream fields that don't apply to a stream's kind are not encoded.

- [ ] **Step 1: Write the failing test**

`internal/testdisc/testdisc_test.go`:
```go
package testdisc

import (
	"io/fs"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/chad3814/zenvik/bluray"
)

func samplePlaylist() *bluray.Playlist {
	stn := bluray.STN{
		Video: []bluray.Stream{{PID: 0x1011, Coding: bluray.CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 1}},
		Audio: []bluray.Stream{
			{PID: 0x1100, Coding: bluray.CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1101, Coding: bluray.CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "deu"},
		},
		PG: []bluray.Stream{{PID: 0x1200, Coding: bluray.CodingPG, Language: "eng"}},
	}
	return &bluray.Playlist{
		Version: "0300",
		Items: []bluray.PlayItem{
			{ClipID: "00001", CodecID: "M2TS", ConnectionCondition: 1, In: 0, Out: 45000 * 600, STN: stn},
			{ClipID: "00002", CodecID: "M2TS", ConnectionCondition: 5, In: 0, Out: 45000 * 300,
				Angles: []string{"00003", "00004"}, STN: stn},
		},
		Marks: []bluray.Mark{
			{Type: bluray.MarkEntry, PlayItem: 0, Time: 0, PID: 0xFFFF},
			{Type: bluray.MarkEntry, PlayItem: 1, Time: 45000 * 60, PID: 0xFFFF},
		},
	}
}

func sampleClip() *bluray.Clip {
	return &bluray.Clip{
		Version: "0300", StreamType: 1, ApplicationType: 1, TSRecordingRate: 48_000_000, SourcePackets: 32,
		Programs: []bluray.Program{{PMTPID: 0x100, Streams: []bluray.Stream{
			{PID: 0x1011, Coding: bluray.CodingAVC, VideoFormat: 6, FrameRate: 1},
			{PID: 0x1012, Coding: bluray.CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 2},
			{PID: 0x1100, Coding: bluray.CodingDTSHDMA, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1200, Coding: bluray.CodingPG, Language: "eng"},
			{PID: 0x1800, Coding: bluray.CodingTextST, Language: "fra"},
		}}},
	}
}

func TestIndexRoundTrip(t *testing.T) {
	in := &bluray.Index{
		Version:       "0300",
		FirstPlayback: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0},
		TopMenu:       bluray.Object{Type: bluray.ObjectBDJ, PlaybackType: 1, BDJOName: "00000"},
		Titles: []bluray.IndexTitle{
			{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 1}},
			{Object: bluray.Object{Type: bluray.ObjectBDJ, BDJOName: "00001"}, AccessType: 2},
		},
	}
	got, err := bluray.ParseIndex(Index(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
}

func TestMovieObjectsRoundTrip(t *testing.T) {
	in := &bluray.MovieObjects{Version: "0200", Objects: []bluray.MovieObject{
		{ResumeIntention: true, Commands: []bluray.NavCommand{CmdMove(2, 800), CmdPlayPLReg(2), CmdJumpObject(1)}},
		{TitleSearchMask: true, Commands: []bluray.NavCommand{CmdPlayPL(801), CmdJumpTitle(2)}},
	}}
	got, err := bluray.ParseMovieObjects(MovieObjects(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
	if pls := got.Playlists(0); !reflect.DeepEqual(pls, []int{800, 801}) {
		t.Errorf("Playlists(0) = %v", pls)
	}
}

func TestPlaylistRoundTrip(t *testing.T) {
	in := samplePlaylist()
	got, err := bluray.ParsePlaylist(Playlist(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
}

func TestClipRoundTrip(t *testing.T) {
	in := sampleClip()
	got, err := bluray.ParseClip(Clip(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
}

func TestMetaRoundTrip(t *testing.T) {
	got, err := bluray.ParseMeta(Meta("Tom & Jerry <Deluxe>", "eng"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Tom & Jerry <Deluxe>" || got.Language != "eng" {
		t.Errorf("ParseMeta = %+v", got)
	}
}

func TestCleanM2TS(t *testing.T) {
	b := CleanM2TS(2)
	if len(b) != 2*6144 {
		t.Fatalf("len = %d", len(b))
	}
	for p := 0; p < 64; p++ {
		if b[p*192+4] != 0x47 {
			t.Fatalf("packet %d has no sync byte", p)
		}
	}
}

func sampleDisc() *Disc {
	return &Disc{
		FirstPlayback: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0},
		Titles:        []bluray.IndexTitle{{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0}}},
		MovieObjects:  []bluray.MovieObject{{Commands: []bluray.NavCommand{CmdPlayPL(800)}}},
		Playlists:     map[string]*bluray.Playlist{"00800": samplePlaylist()},
		Clips:         map[string]*bluray.Clip{"00001": sampleClip(), "00002": sampleClip()},
		ClipData:      map[string][]byte{"00002": []byte("custom")},
		MetaTitle:     "Sample Disc",
	}
}

func TestDiscFiles(t *testing.T) {
	files := sampleDisc().Files()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	want := []string{
		"BDMV/CLIPINF/00001.clpi", "BDMV/CLIPINF/00002.clpi",
		"BDMV/META/DL/bdmt_eng.xml", "BDMV/MovieObject.bdmv",
		"BDMV/PLAYLIST/00800.mpls",
		"BDMV/STREAM/00001.m2ts", "BDMV/STREAM/00002.m2ts",
		"BDMV/index.bdmv",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("files = %v\nwant %v", names, want)
	}
	if len(files["BDMV/STREAM/00001.m2ts"]) != 6144 {
		t.Errorf("default clip data length = %d", len(files["BDMV/STREAM/00001.m2ts"]))
	}
	if string(files["BDMV/STREAM/00002.m2ts"]) != "custom" {
		t.Errorf("custom clip data not used")
	}
	idx, err := bluray.ParseIndex(files["BDMV/index.bdmv"])
	if err != nil || len(idx.Titles) != 1 {
		t.Errorf("index = %+v, %v", idx, err)
	}
}

func TestDiscWriteDirMatchesMapFS(t *testing.T) {
	d := sampleDisc()
	dir := t.TempDir()
	if err := d.WriteDir(dir); err != nil {
		t.Fatal(err)
	}
	mfs := d.MapFS()
	err := fs.WalkDir(mfs, ".", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		want, _ := fs.ReadFile(mfs, p)
		got, err := fs.ReadFile(os.DirFS(dir), p)
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			t.Errorf("%s differs", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/testdisc/`
Expected: FAIL to compile (`undefined: Index`).

- [ ] **Step 3: Implement the encoders**

`internal/testdisc/encode.go`:
```go
// Package testdisc builds synthetic Blu-ray navigation files and disc
// trees for tests. Its encoders are the inverse of the bluray parsers.
package testdisc

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"

	"github.com/chad3814/zenvik/bluray"
)

type writer struct{ b []byte }

func (w *writer) u8(v uint8)   { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) zero(n int)   { w.b = append(w.b, make([]byte, n)...) }

// str writes s padded or truncated to exactly n bytes.
func (w *writer) str(s string, n int) {
	b := make([]byte, n)
	copy(b, s)
	w.b = append(w.b, b...)
}

func (w *writer) patch32(at int, v uint32) { binary.BigEndian.PutUint32(w.b[at:], v) }

// block32, block16 and block8 write a length field, then fn's output, then
// patch the length to the number of bytes fn wrote.
func (w *writer) block32(fn func()) {
	at := len(w.b)
	w.u32(0)
	fn()
	w.patch32(at, uint32(len(w.b)-at-4))
}

func (w *writer) block16(fn func()) {
	at := len(w.b)
	w.u16(0)
	fn()
	binary.BigEndian.PutUint16(w.b[at:], uint16(len(w.b)-at-2))
}

func (w *writer) block8(fn func()) {
	at := len(w.b)
	w.u8(0)
	fn()
	w.b[at] = uint8(len(w.b) - at - 1)
}

func version(v string) string {
	if v == "" {
		return "0200"
	}
	return v
}

// Index encodes an index.bdmv.
func Index(idx *bluray.Index) []byte {
	var w writer
	w.str("INDX", 4)
	w.str(version(idx.Version), 4)
	w.u32(0) // indexes start address, patched below
	w.u32(0) // extension data start address
	w.zero(24)
	w.block32(func() { w.zero(34) }) // AppInfoBDMV
	w.patch32(8, uint32(len(w.b)))
	w.block32(func() {
		object(&w, idx.FirstPlayback, 0)
		object(&w, idx.TopMenu, 0)
		w.u16(uint16(len(idx.Titles)))
		for _, t := range idx.Titles {
			object(&w, t.Object, t.AccessType)
		}
	})
	return w.b
}

func object(w *writer, o bluray.Object, access uint8) {
	w.u32(uint32(o.Type)<<30 | uint32(access&0x3)<<28)
	w.u16(uint16(o.PlaybackType) << 14)
	if o.Type == bluray.ObjectBDJ {
		w.str(o.BDJOName, 5)
		w.u8(0)
		return
	}
	w.u16(o.MovieObjectID)
	w.u32(0)
}

// MovieObjects encodes a MovieObject.bdmv.
func MovieObjects(m *bluray.MovieObjects) []byte {
	var w writer
	w.str("MOBJ", 4)
	w.str(version(m.Version), 4)
	w.u32(0) // extension data start address
	w.zero(28)
	w.block32(func() {
		w.zero(4)
		w.u16(uint16(len(m.Objects)))
		for _, o := range m.Objects {
			var flags uint16
			if o.ResumeIntention {
				flags |= 0x8000
			}
			if o.MenuCallMask {
				flags |= 0x4000
			}
			if o.TitleSearchMask {
				flags |= 0x2000
			}
			w.u16(flags)
			w.u16(uint16(len(o.Commands)))
			for _, c := range o.Commands {
				w.u32(c.Opcode)
				w.u32(c.Dst)
				w.u32(c.Src)
			}
		}
	})
	return w.b
}

// Playlist encodes an MPLS file.
func Playlist(p *bluray.Playlist) []byte {
	var w writer
	w.str("MPLS", 4)
	w.str(version(p.Version), 4)
	w.u32(0) // playlist start address, patched below
	w.u32(0) // playlist mark start address, patched below
	w.u32(0) // extension data start address
	w.zero(20)
	w.block32(func() { // AppInfoPlayList
		w.zero(1)
		w.u8(1) // playback type: sequential
		w.zero(2 + 8 + 2)
	})
	w.patch32(8, uint32(len(w.b)))
	w.block32(func() {
		w.zero(2)
		w.u16(uint16(len(p.Items)))
		w.u16(0) // sub-paths
		for _, it := range p.Items {
			playItem(&w, it)
		}
	})
	w.patch32(12, uint32(len(w.b)))
	w.block32(func() {
		w.u16(uint16(len(p.Marks)))
		for _, m := range p.Marks {
			w.zero(1)
			w.u8(uint8(m.Type))
			w.u16(m.PlayItem)
			w.u32(uint32(m.Time))
			w.u16(m.PID)
			w.u32(uint32(m.Duration))
		}
	})
	return w.b
}

func playItem(w *writer, it bluray.PlayItem) {
	codec := it.CodecID
	if codec == "" {
		codec = "M2TS"
	}
	w.block16(func() {
		w.str(it.ClipID, 5)
		w.str(codec, 4)
		flags := uint16(it.ConnectionCondition & 0x0F)
		if len(it.Angles) > 0 {
			flags |= 0x10
		}
		w.u16(flags)
		w.u8(it.STCID)
		w.u32(uint32(it.In))
		w.u32(uint32(it.Out))
		w.zero(8) // UO mask
		w.u8(0)   // random access flag
		w.u8(it.StillMode)
		w.u16(0) // still time
		if len(it.Angles) > 0 {
			w.u8(uint8(len(it.Angles) + 1))
			w.u8(0)
			for _, a := range it.Angles {
				w.str(a, 5)
				w.str("M2TS", 4)
				w.u8(0)
			}
		}
		stn(w, it.STN)
	})
}

func stn(w *writer, s bluray.STN) {
	w.block16(func() {
		w.zero(2)
		w.u8(uint8(len(s.Video)))
		w.u8(uint8(len(s.Audio)))
		w.u8(uint8(len(s.PG)))
		w.zero(4) // IG, secondary audio, secondary video, PiP PG
		w.zero(5)
		for _, group := range [][]bluray.Stream{s.Video, s.Audio, s.PG} {
			for _, st := range group {
				w.block8(func() {
					w.u8(1) // stream in main clip
					w.u16(st.PID)
					w.zero(6)
				})
				attributes(w, st, false)
			}
		}
	})
}

func attributes(w *writer, s bluray.Stream, clip bool) {
	w.block8(func() {
		w.u8(uint8(s.Coding))
		switch s.Coding.Kind() {
		case bluray.KindVideo:
			w.u8(uint8(s.VideoFormat)<<4 | uint8(s.FrameRate)&0x0F)
			if clip {
				w.u8(0x30) // aspect ratio 16:9
			}
			if s.Coding == bluray.CodingHEVC {
				w.u8(uint8(s.DynamicRange) << 4)
			}
		case bluray.KindAudio:
			w.u8(uint8(s.AudioFormat)<<4 | uint8(s.SampleRate)&0x0F)
			w.str(s.Language, 3)
		case bluray.KindPG, bluray.KindIG:
			w.str(s.Language, 3)
		case bluray.KindText:
			w.u8(1) // UTF-8
			w.str(s.Language, 3)
		}
	})
}

// Clip encodes a CLPI file.
func Clip(c *bluray.Clip) []byte {
	var w writer
	w.str("HDMV", 4)
	w.str(version(c.Version), 4)
	w.u32(0) // sequence info start address, patched below
	w.u32(0) // program info start address, patched below
	w.zero(12) // CPI, clip mark, extension data start addresses
	w.zero(12)
	w.block32(func() { // ClipInfo
		w.zero(2)
		w.u8(c.StreamType)
		w.u8(c.ApplicationType)
		w.zero(4)
		w.u32(c.TSRecordingRate)
		w.u32(c.SourcePackets)
		w.zero(128)
		w.block16(func() {}) // TS type info block
	})
	w.patch32(8, uint32(len(w.b)))
	w.block32(func() { w.zero(2) }) // SequenceInfo with no ATC sequences
	w.patch32(12, uint32(len(w.b)))
	w.block32(func() {
		w.zero(1)
		w.u8(uint8(len(c.Programs)))
		for _, p := range c.Programs {
			w.u32(p.SPNStart)
			w.u16(p.PMTPID)
			w.u8(uint8(len(p.Streams)))
			w.u8(0)
			for _, s := range p.Streams {
				w.u16(s.PID)
				attributes(&w, s, true)
			}
		}
	})
	return w.b
}

// Meta encodes a disc library metadata file.
func Meta(title, lang string) []byte {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<disclib xmlns="urn:BDA:bdmv;disclib" xmlns:di="urn:BDA:bdmv;discinfo"><di:discinfo><di:title><di:name>`)
	_ = xml.EscapeText(&buf, []byte(title))
	buf.WriteString(`</di:name></di:title><di:language>`)
	_ = xml.EscapeText(&buf, []byte(lang))
	buf.WriteString(`</di:language></di:discinfo></disclib>`)
	return buf.Bytes()
}
```

- [ ] **Step 4: Implement the disc builder**

`internal/testdisc/disc.go`:
```go
package testdisc

import (
	"os"
	"path/filepath"
	"testing/fstest"

	"github.com/chad3814/zenvik/bluray"
)

// Navigation command opcodes (see bluray.NavCommand).
const (
	opPlayPL     = 0x22800000 // PLAY_PL, immediate playlist
	opPlayPLReg  = 0x22000000 // PLAY_PL, playlist in register
	opMoveImm    = 0x50400001 // SET MOVE, immediate source
	opJumpObject = 0x21800000 // JUMP_OBJECT, immediate
	opJumpTitle  = 0x21810000 // JUMP_TITLE, immediate
)

// CmdPlayPL plays playlist pl.
func CmdPlayPL(pl uint32) bluray.NavCommand { return bluray.NavCommand{Opcode: opPlayPL, Dst: pl} }

// CmdPlayPLReg plays the playlist whose number is in general register reg.
func CmdPlayPLReg(reg uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opPlayPLReg, Dst: reg}
}

// CmdMove loads value into general register reg.
func CmdMove(reg, value uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opMoveImm, Dst: reg, Src: value}
}

// CmdJumpObject jumps to movie object id.
func CmdJumpObject(id uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opJumpObject, Dst: id}
}

// CmdJumpTitle jumps to title number title.
func CmdJumpTitle(title uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opJumpTitle, Dst: title}
}

// CleanM2TS returns units aligned units (32 source packets, 6144 bytes)
// of unencrypted M2TS: each packet has a 4-byte TP_extra_header followed
// by a TS null packet starting with the 0x47 sync byte.
func CleanM2TS(units int) []byte {
	b := make([]byte, units*6144)
	for p := 0; p < units*32; p++ {
		o := p * 192
		b[o+4] = 0x47
		b[o+5] = 0x1F // PID 0x1FFF (null packet)
		b[o+6] = 0xFF
		b[o+7] = 0x10 // payload only
	}
	return b
}

// Disc describes a synthetic BDMV tree.
type Disc struct {
	FirstPlayback bluray.Object
	TopMenu       bluray.Object
	Titles        []bluray.IndexTitle
	MovieObjects  []bluray.MovieObject
	Playlists     map[string]*bluray.Playlist // keyed by playlist ID, e.g. "00800"
	Clips         map[string]*bluray.Clip     // keyed by clip ID, e.g. "00001"
	ClipData      map[string][]byte           // M2TS contents by clip ID; default CleanM2TS(1)
	MetaTitle     string                      // writes BDMV/META/DL/bdmt_eng.xml when non-empty
}

// Files returns the disc's files keyed by slash-separated path.
func (d *Disc) Files() map[string][]byte {
	files := map[string][]byte{
		"BDMV/index.bdmv": Index(&bluray.Index{
			Version: "0200", FirstPlayback: d.FirstPlayback, TopMenu: d.TopMenu, Titles: d.Titles,
		}),
		"BDMV/MovieObject.bdmv": MovieObjects(&bluray.MovieObjects{Version: "0200", Objects: d.MovieObjects}),
	}
	for id, p := range d.Playlists {
		files["BDMV/PLAYLIST/"+id+".mpls"] = Playlist(p)
	}
	for id, c := range d.Clips {
		files["BDMV/CLIPINF/"+id+".clpi"] = Clip(c)
		data, ok := d.ClipData[id]
		if !ok {
			data = CleanM2TS(1)
		}
		files["BDMV/STREAM/"+id+".m2ts"] = data
	}
	if d.MetaTitle != "" {
		files["BDMV/META/DL/bdmt_eng.xml"] = Meta(d.MetaTitle, "eng")
	}
	return files
}

// MapFS returns the disc as an in-memory file system.
func (d *Disc) MapFS() fstest.MapFS {
	m := fstest.MapFS{}
	for name, data := range d.Files() {
		m[name] = &fstest.MapFile{Data: data, Mode: 0o444}
	}
	return m
}

// WriteDir writes the disc's files under dir.
func (d *Disc) WriteDir(dir string) error {
	for name, data := range d.Files() {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./internal/testdisc/ ./bluray/ && go vet ./... && golangci-lint run`
Expected: PASS; no findings.

- [ ] **Step 6: Commit**

```bash
git add internal/testdisc
git commit -m "Add synthetic Blu-ray disc builder for tests"
```

---

### Task 9: Independent UDF image writer (`internal/testdisc/udfimage`)

This writer is test infrastructure, but it's the only source of UDF 2.50 metadata-partition images, so its correctness matters. A darwin integration test mounts its output with the macOS UDF driver, an independent implementation, before any reader code relies on it.

**Files:**
- Create: `internal/testdisc/udfimage/udfimage.go`
- Test: `internal/testdisc/udfimage/udfimage_test.go`, `internal/testdisc/udfimage/hdiutil_darwin_test.go`

**Interfaces:**
- Consumes: nothing (standard library only; deliberately does not import `udf`).
- Produces (used by Tasks 11–12 and by milestone 2 ISO tests):
  - `type File struct { Data []byte; SparseSize int64 }`. When `SparseSize > 0`, the file is that many zero bytes, stored as unallocated extents.
  - `type Options struct { Revision uint16; Label string; Embed bool; MaxInlineADs int }`. `Revision` is `0x0102` or `0x0250`. `MaxInlineADs` is 0, or ≥ 2 to force allocation descriptors to spill into an AED.
  - `func Build(files map[string]File, opt Options) ([]byte, error)`
- Image layout (sector = logical block = 2048 bytes):
  - sectors 16–18: volume recognition sequence (`BEA01`; `NSR02` for 1.02 or `NSR03` for 2.50; `TEA01`)
  - sectors 32–37: main volume descriptor sequence (PVD, IUVD, PD, LVD, USD, TD)
  - sectors 48–53: reserve sequence
  - sector 64: LVID
  - sectors 256 and N−1: AVDP
  - physical partition from sector 257
  - UDF 1.02: the file set descriptor, file entries, directories and file data all live in the physical partition (short ADs).
  - UDF 2.50: physical blocks 0 and 1 hold the metadata file and mirror EFEs; blocks 2.. hold the metadata partition (FSD, EFEs, directories; size rounded up to 32 blocks); file data follows, referenced by long ADs to partition 0.

- [ ] **Step 1: Write the failing unit tests**

`internal/testdisc/udfimage/udfimage_test.go`:
```go
package udfimage

import (
	"encoding/binary"
	"strings"
	"testing"
)

func sectorAt(img []byte, n int) []byte { return img[n*sector : (n+1)*sector] }

func checkTag(t *testing.T, d []byte, wantID uint16, wantLoc uint32) {
	t.Helper()
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	if sum != d[4] {
		t.Errorf("tag checksum %#x, want %#x", d[4], sum)
	}
	if id := binary.LittleEndian.Uint16(d); id != wantID {
		t.Errorf("tag id %d, want %d", id, wantID)
	}
	if loc := binary.LittleEndian.Uint32(d[12:]); loc != wantLoc {
		t.Errorf("tag location %d, want %d", loc, wantLoc)
	}
	n := int(binary.LittleEndian.Uint16(d[10:]))
	if got, want := crc16(d[16:16+n]), binary.LittleEndian.Uint16(d[8:]); got != want {
		t.Errorf("tag CRC %#x, want %#x", got, want)
	}
}

func TestCRC16KnownVector(t *testing.T) {
	// CRC-16/XMODEM (poly 0x1021, init 0) of "123456789".
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("crc16 = %#x, want 0x31C3", got)
	}
}

func TestBuildStructure(t *testing.T) {
	files := map[string]File{"BDMV/index.bdmv": {Data: []byte("INDX0200")}}
	for _, tc := range []struct {
		rev uint16
		nsr string
	}{{0x0102, "NSR02"}, {0x0250, "NSR03"}} {
		img, err := Build(files, Options{Revision: tc.rev, Label: "TEST"})
		if err != nil {
			t.Fatal(err)
		}
		if len(img)%sector != 0 {
			t.Fatalf("image length %d not a multiple of %d", len(img), sector)
		}
		for i, id := range []string{"BEA01", tc.nsr, "TEA01"} {
			if got := string(sectorAt(img, 16+i)[1:6]); got != id {
				t.Errorf("rev %#x: VRS sector %d = %q, want %q", tc.rev, 16+i, got, id)
			}
		}
		last := len(img)/sector - 1
		checkTag(t, sectorAt(img, 256), 2, 256)
		checkTag(t, sectorAt(img, last), 2, uint32(last))
		checkTag(t, sectorAt(img, 32), 1, 32) // PVD
		checkTag(t, sectorAt(img, 35), 6, 35) // LVD
		checkTag(t, sectorAt(img, 37), 8, 37) // TD
		checkTag(t, sectorAt(img, 64), 9, 64) // LVID
		if got := string(sectorAt(img, 32)[25:29]); got != "TEST" {
			t.Errorf("PVD volume identifier = %q", got)
		}
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]File
		opt   Options
		want  string
	}{
		{"revision", map[string]File{"a": {}}, Options{Revision: 0x0201}, "unsupported revision"},
		{"conflict", map[string]File{"a": {}, "a/b": {}}, Options{Revision: 0x0102}, "path conflict"},
		{"invalid path", map[string]File{"/abs": {}}, Options{Revision: 0x0102}, "invalid path"},
		{"inline ADs", map[string]File{"a": {}}, Options{Revision: 0x0102, MaxInlineADs: 1}, "MaxInlineADs"},
	}
	for _, tt := range tests {
		_, err := Build(tt.files, tt.opt)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want containing %q", tt.name, err, tt.want)
		}
	}
}
```

- [ ] **Step 2: Write the failing macOS integration test**

`internal/testdisc/udfimage/hdiutil_darwin_test.go`:
```go
//go:build integration && darwin

package udfimage

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMacOSMountsImages checks the writer against the macOS UDF driver.
func TestMacOSMountsImages(t *testing.T) {
	files := map[string]File{
		"BDMV/index.bdmv":          {Data: []byte("INDX0200 test")},
		"BDMV/PLAYLIST/00800.mpls": {Data: bytes.Repeat([]byte("p"), 5000)},
		"BDMV/STREAM/00001.m2ts":   {Data: bytes.Repeat([]byte{0x47, 0x00}, 3*1024+9)},
		"BDMV/META/DL/タイトル.txt":     {Data: []byte("unicode name")},
		"CERTIFICATE/empty.bin":    {},
	}
	for _, tc := range []struct {
		name string
		opt  Options
	}{
		{"udf102", Options{Revision: 0x0102, Label: "ZENVIK_102"}},
		{"udf250", Options{Revision: 0x0250, Label: "ZENVIK_250"}},
		{"udf250-embedded", Options{Revision: 0x0250, Label: "ZENVIK_EMB", Embed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, err := Build(files, tc.opt)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			iso := filepath.Join(dir, "test.iso")
			if err := os.WriteFile(iso, img, 0o644); err != nil {
				t.Fatal(err)
			}
			mnt := filepath.Join(dir, "mnt")
			if err := os.Mkdir(mnt, 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("hdiutil", "attach", "-readonly", "-nobrowse",
				"-imagekey", "diskimage-class=CRawDiskImage", "-mountpoint", mnt, iso).CombinedOutput()
			if err != nil {
				t.Fatalf("hdiutil attach: %v\n%s", err, out)
			}
			t.Cleanup(func() { _ = exec.Command("hdiutil", "detach", "-force", mnt).Run() })
			for name, f := range files {
				got, err := os.ReadFile(filepath.Join(mnt, filepath.FromSlash(name)))
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if !bytes.Equal(got, f.Data) {
					t.Errorf("%s: got %d bytes, want %d", name, len(got), len(f.Data))
				}
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/testdisc/udfimage/`
Expected: FAIL to compile (`undefined: Build`, `undefined: crc16`).

- [ ] **Step 4: Implement the writer**

`internal/testdisc/udfimage/udfimage.go`:
```go
// Package udfimage builds small UDF disc images in memory for tests. It
// writes UDF 1.02 (one physical partition, short allocation descriptors)
// and UDF 2.50 (a physical partition plus a metadata partition, with long
// allocation descriptors for file data), the layouts the udf package
// reads. It is written independently of the reader so reader tests do not
// share the reader's assumptions.
package udfimage

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	sector     = 2048
	partStart  = 257        // first sector of the physical partition
	lvidSector = 64         // logical volume integrity descriptor
	maxExtent  = 0x3FFFF800 // largest extent length that is a whole number of sectors
	metaAlign  = 32         // metadata file allocation unit, in blocks
)

var stamp = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// File is the content of one regular file in the image.
type File struct {
	Data []byte
	// SparseSize, when non-zero, makes the file SparseSize zero bytes
	// stored as unallocated extents, so huge files cost no image space.
	SparseSize int64
}

// Options control the image layout.
type Options struct {
	Revision uint16 // 0x0102 or 0x0250
	Label    string
	// Embed stores each file small enough to fit inside its file entry.
	Embed bool
	// MaxInlineADs, when at least 2, limits a file entry to that many
	// allocation descriptors; the rest go into an allocation extent
	// descriptor.
	MaxInlineADs int
}

type node struct {
	name     string
	dir      bool
	file     File
	parent   *node
	children []*node
	uid      uint64
	fe       uint32 // file entry block (fe space)
	dirBlk   uint32 // first directory data block (fe space)
	aed      uint32 // allocation extent descriptor block (fe space)
	hasAED   bool
	embedded bool
	data     uint32 // first data block (physical partition)
}

type ext struct {
	typ    uint8 // 0 recorded, 2 not recorded and not allocated, 3 continuation
	length uint32
	loc    uint32
}

// builder lays out two block spaces. "fe space" holds the file set
// descriptor, file entries, directories and AEDs: the physical partition
// for UDF 1.02, the metadata partition for UDF 2.50. File data always
// lives in the physical partition.
type builder struct {
	opt      Options
	meta     bool
	feRef    uint16 // partition reference number of fe space
	feSpace  map[uint32][]byte
	phys     map[uint32][]byte
	feNext   uint32
	metaLen  uint32 // metadata file length in blocks (UDF 2.50)
	physNext uint32
	nextUID  uint64
	files    uint32
	dirs     uint32
}

// Build returns a UDF image containing files, keyed by slash-separated
// paths such as "BDMV/index.bdmv". Directories are created as needed.
func Build(files map[string]File, opt Options) ([]byte, error) {
	if opt.Revision != 0x0102 && opt.Revision != 0x0250 {
		return nil, fmt.Errorf("udfimage: unsupported revision %#04x", opt.Revision)
	}
	if opt.MaxInlineADs == 1 {
		return nil, fmt.Errorf("udfimage: MaxInlineADs must be 0 or at least 2")
	}
	root, err := buildTree(files)
	if err != nil {
		return nil, err
	}
	b := &builder{
		opt:     opt,
		meta:    opt.Revision >= 0x0250,
		feSpace: map[uint32][]byte{},
		phys:    map[uint32][]byte{},
		feNext:  1, // block 0 holds the file set descriptor
		nextUID: 16,
	}
	if b.meta {
		b.feRef = 1
	}
	b.layout(root)
	if b.meta {
		b.metaLen = (b.feNext + metaAlign - 1) / metaAlign * metaAlign
		b.physNext = 2 + b.metaLen
	} else {
		b.physNext = b.feNext
	}
	b.assignData(root)
	b.emit(root)
	b.feSpace[0] = b.fsd(root)
	return b.image(), nil
}

func buildTree(files map[string]File) (*node, error) {
	root := &node{dir: true}
	root.parent = root
	for p, f := range files {
		if p == "." || !fs.ValidPath(p) {
			return nil, fmt.Errorf("udfimage: invalid path %q", p)
		}
		parts := strings.Split(p, "/")
		cur := root
		for i, part := range parts {
			last := i == len(parts)-1
			var child *node
			for _, c := range cur.children {
				if c.name == part {
					child = c
					break
				}
			}
			switch {
			case child == nil:
				child = &node{name: part, dir: !last, parent: cur}
				if last {
					child.file = f
				}
				cur.children = append(cur.children, child)
			case last || !child.dir:
				return nil, fmt.Errorf("udfimage: path conflict at %q", p)
			}
			cur = child
		}
	}
	sortTree(root)
	return root, nil
}

func sortTree(n *node) {
	sort.Slice(n.children, func(i, j int) bool { return n.children[i].name < n.children[j].name })
	for _, c := range n.children {
		sortTree(c)
	}
}

func blocks(n int64) uint32 { return uint32((n + sector - 1) / sector) }

func fidLen(nameLen int) int { return (38 + nameLen + 3) &^ 3 }

func dirLen(n *node) int {
	l := fidLen(0) // parent entry
	for _, c := range n.children {
		l += fidLen(len(encodeName(c.name)))
	}
	return l
}

func (b *builder) entryHeader() int {
	if b.meta {
		return 216 // extended file entry
	}
	return 176 // file entry
}

// layout assigns fe-space blocks in depth-first order.
func (b *builder) layout(n *node) {
	n.fe = b.feNext
	b.feNext++
	if n != n.parent {
		n.uid = b.nextUID
		b.nextUID++
	}
	if n.dir {
		b.dirs++
		n.dirBlk = b.feNext
		b.feNext += blocks(int64(dirLen(n)))
		for _, c := range n.children {
			b.layout(c)
		}
		return
	}
	b.files++
	n.embedded = b.opt.Embed && n.file.SparseSize == 0 && len(n.file.Data) <= sector-b.entryHeader()
	if !n.embedded && b.opt.MaxInlineADs >= 2 && len(b.extents(n)) > b.opt.MaxInlineADs {
		n.hasAED = true
		n.aed = b.feNext
		b.feNext++
	}
}

func (b *builder) extents(n *node) []ext {
	if n.file.SparseSize > 0 {
		var out []ext
		for left := n.file.SparseSize; left > 0; left -= maxExtent {
			out = append(out, ext{typ: 2, length: uint32(min(left, maxExtent))})
		}
		return out
	}
	if len(n.file.Data) == 0 {
		return nil
	}
	return []ext{{typ: 0, length: uint32(len(n.file.Data)), loc: n.data}}
}

func (b *builder) assignData(n *node) {
	if n.dir {
		for _, c := range n.children {
			b.assignData(c)
		}
		return
	}
	if n.embedded || n.file.SparseSize > 0 || len(n.file.Data) == 0 {
		return
	}
	n.data = b.physNext
	b.physNext += blocks(int64(len(n.file.Data)))
	for off := 0; off < len(n.file.Data); off += sector {
		b.phys[n.data+uint32(off/sector)] = n.file.Data[off:min(off+sector, len(n.file.Data))]
	}
}

type entryParams struct {
	loc      uint32
	fileType uint8
	adType   uint16
	ads      []byte
	size     uint64
	recorded uint64
	uid      uint64
	links    uint16
}

func (b *builder) emit(n *node) {
	e := entryParams{loc: n.fe, uid: n.uid, links: 1, fileType: 5}
	switch {
	case n.dir:
		data := b.dirData(n)
		for off := 0; off < len(data); off += sector {
			b.feSpace[n.dirBlk+uint32(off/sector)] = data[off:min(off+sector, len(data))]
		}
		e.fileType = 4
		e.size = uint64(len(data))
		e.recorded = uint64(blocks(int64(len(data))))
		e.ads = shortAD(0, uint32(len(data)), n.dirBlk)
		for _, c := range n.children {
			if c.dir {
				e.links++
			}
			b.emit(c)
		}
	case n.embedded:
		e.size = uint64(len(n.file.Data))
		e.adType = 3
		e.ads = n.file.Data
	default:
		exts := b.extents(n)
		e.size = uint64(len(n.file.Data))
		if n.file.SparseSize > 0 {
			e.size = uint64(n.file.SparseSize)
		}
		for _, x := range exts {
			if x.typ == 0 {
				e.recorded += uint64(blocks(int64(x.length)))
			}
		}
		e.adType, e.ads = b.fileADs(n, exts)
	}
	b.feSpace[n.fe] = b.entry(e)
}

// fileADs encodes a file's data extents: short ADs in the same partition
// for UDF 1.02, long ADs pointing at partition 0 for UDF 2.50.
func (b *builder) fileADs(n *node, exts []ext) (uint16, []byte) {
	enc := func(x ext) []byte {
		if b.meta {
			return longAD(x.typ, x.length, x.loc, 0)
		}
		return shortAD(x.typ, x.length, x.loc)
	}
	adType := uint16(0)
	if b.meta {
		adType = 1
	}
	inline, rest := exts, []ext(nil)
	if n.hasAED {
		k := b.opt.MaxInlineADs - 1 // the last inline slot is the continuation
		inline, rest = exts[:k], exts[k:]
	}
	var out []byte
	for _, x := range inline {
		out = append(out, enc(x)...)
	}
	if n.hasAED {
		var more []byte
		for _, x := range rest {
			more = append(more, enc(x)...)
		}
		d := make([]byte, 24+len(more))
		le32(d[20:], uint32(len(more)))
		copy(d[24:], more)
		b.feSpace[n.aed] = b.tag(258, n.aed, d)
		if b.meta {
			out = append(out, longAD(3, sector, n.aed, b.feRef)...)
		} else {
			out = append(out, shortAD(3, sector, n.aed)...)
		}
	}
	return adType, out
}

func (b *builder) dirData(n *node) []byte {
	var out []byte
	add := func(chars uint8, name []byte, target *node) {
		blk := n.dirBlk + uint32(len(out)/sector)
		out = append(out, b.fid(blk, chars, name, target.fe)...)
	}
	add(0x0A, nil, n.parent) // directory | parent
	for _, c := range n.children {
		var chars uint8
		if c.dir {
			chars = 0x02
		}
		add(chars, encodeName(c.name), c)
	}
	return out
}

func (b *builder) fid(blk uint32, chars uint8, name []byte, icb uint32) []byte {
	d := make([]byte, fidLen(len(name)))
	le16(d[16:], 1) // file version number
	d[18] = chars
	d[19] = uint8(len(name))
	copy(d[20:], longAD(0, sector, icb, b.feRef))
	copy(d[38:], name)
	return b.tag(257, blk, d)
}

func (b *builder) entry(e entryParams) []byte {
	hdr := b.entryHeader()
	d := make([]byte, hdr+len(e.ads))
	le16(d[16+4:], 4) // ICB strategy type 4
	le16(d[16+8:], 1) // maximum number of entries
	d[16+11] = e.fileType
	le16(d[16+18:], e.adType)
	le32(d[36:], 0xFFFFFFFF) // uid
	le32(d[40:], 0xFFFFFFFF) // gid
	le32(d[44:], 0x14A5)     // read and execute for owner, group, other
	le16(d[48:], e.links)
	le64(d[56:], e.size)
	if b.meta {
		le64(d[64:], e.size) // object size
		le64(d[72:], e.recorded)
		for _, off := range []int{80, 92, 104, 116} {
			timestamp(d[off:])
		}
		le32(d[128:], 1) // checkpoint
		regid(d[168:], "*zenvik", nil)
		le64(d[200:], e.uid)
		le32(d[212:], uint32(len(e.ads)))
	} else {
		le64(d[64:], e.recorded)
		for _, off := range []int{72, 84, 96} {
			timestamp(d[off:])
		}
		le32(d[108:], 1)
		regid(d[128:], "*zenvik", nil)
		le64(d[160:], e.uid)
		le32(d[172:], uint32(len(e.ads)))
	}
	copy(d[hdr:], e.ads)
	id := uint16(261)
	if b.meta {
		id = 266
	}
	return b.tag(id, e.loc, d)
}

func (b *builder) fsd(root *node) []byte {
	d := make([]byte, 512)
	timestamp(d[16:])
	le16(d[28:], 3) // interchange level
	le16(d[30:], 3)
	le32(d[32:], 1) // character set list
	le32(d[36:], 1)
	charspec(d[48:])
	dstring(d[112:240], b.opt.Label)
	charspec(d[240:])
	dstring(d[304:336], b.opt.Label)
	copy(d[400:], longAD(0, sector, root.fe, b.feRef))
	b.domain(d[416:])
	return b.tag(256, 0, d)
}

func (b *builder) vds(start, partLen uint32) [][]byte {
	pvd := make([]byte, 512)
	le32(pvd[16:], 1)
	dstring(pvd[24:56], b.opt.Label)
	le16(pvd[56:], 1)
	le16(pvd[58:], 1)
	le16(pvd[60:], 2)
	le16(pvd[62:], 2)
	le32(pvd[64:], 1)
	le32(pvd[68:], 1)
	dstring(pvd[72:200], b.opt.Label)
	charspec(pvd[200:])
	charspec(pvd[264:])
	timestamp(pvd[376:])
	regid(pvd[388:], "*zenvik", nil)

	iuvd := make([]byte, 512)
	le32(iuvd[16:], 2)
	b.udfRegid(iuvd[20:], "*UDF LV Info")
	charspec(iuvd[52:])
	dstring(iuvd[116:244], b.opt.Label)
	regid(iuvd[352:], "*zenvik", nil)

	pd := make([]byte, 512)
	le32(pd[16:], 3)
	le16(pd[20:], 1) // allocated
	nsr := "+NSR02"
	if b.meta {
		nsr = "+NSR03"
	}
	regid(pd[24:], nsr, nil)
	le32(pd[184:], 1) // read-only access
	le32(pd[188:], partStart)
	le32(pd[192:], partLen)
	regid(pd[196:], "*zenvik", nil)

	maps := []byte{1, 6, 1, 0, 0, 0} // type 1: volume sequence 1, partition 0
	nmaps := uint32(1)
	if b.meta {
		m := make([]byte, 64)
		m[0], m[1] = 2, 64
		b.udfRegid(m[4:], "*UDF Metadata Partition")
		le16(m[36:], 1)          // volume sequence number
		le16(m[38:], 0)          // partition number
		le32(m[40:], 0)          // metadata file location
		le32(m[44:], 1)          // metadata mirror file location
		le32(m[48:], 0xFFFFFFFF) // no metadata bitmap file
		le32(m[52:], metaAlign)  // allocation unit size
		le16(m[56:], 1)          // alignment unit size
		maps = append(maps, m...)
		nmaps = 2
	}
	lvd := make([]byte, 440+len(maps))
	le32(lvd[16:], 4)
	charspec(lvd[20:])
	dstring(lvd[84:212], b.opt.Label)
	le32(lvd[212:], sector)
	b.domain(lvd[216:])
	copy(lvd[248:], longAD(0, sector, 0, b.feRef)) // file set descriptor
	le32(lvd[264:], uint32(len(maps)))
	le32(lvd[268:], nmaps)
	regid(lvd[272:], "*zenvik", nil)
	le32(lvd[432:], sector) // integrity sequence extent
	le32(lvd[436:], lvidSector)
	copy(lvd[440:], maps)

	usd := make([]byte, 24)
	le32(usd[16:], 5)
	td := make([]byte, 512)

	out := [][]byte{pvd, iuvd, pd, lvd, usd, td}
	ids := []uint16{1, 4, 5, 6, 7, 8}
	for i := range out {
		out[i] = b.tag(ids[i], start+uint32(i), out[i])
	}
	return out
}

func (b *builder) lvid(partLen uint32) []byte {
	n := 1
	if b.meta {
		n = 2
	}
	d := make([]byte, 80+8*n+46)
	timestamp(d[16:])
	le32(d[28:], 1) // closed
	le64(d[40:], b.nextUID)
	le32(d[72:], uint32(n))
	le32(d[76:], 46)
	le32(d[80+4*n:], partLen) // size table follows the (zero) free space table
	if b.meta {
		le32(d[80+4*n+4:], b.metaLen)
	}
	iu := d[80+8*n:]
	regid(iu, "*zenvik", nil)
	le32(iu[32:], b.files)
	le32(iu[36:], b.dirs)
	le16(iu[40:], b.opt.Revision) // minimum read revision
	le16(iu[42:], b.opt.Revision) // minimum write revision
	le16(iu[44:], b.opt.Revision) // maximum write revision
	return b.tag(9, lvidSector, d)
}

func (b *builder) avdp(loc uint32) []byte {
	d := make([]byte, 512)
	le32(d[16:], 16*sector) // main volume descriptor sequence
	le32(d[20:], 32)
	le32(d[24:], 16*sector) // reserve volume descriptor sequence
	le32(d[28:], 48)
	return b.tag(2, loc, d)
}

func (b *builder) image() []byte {
	partLen := b.physNext
	total := partStart + partLen + 1
	img := make([]byte, int(total)*sector)
	put := func(s uint32, d []byte) { copy(img[int(s)*sector:], d) }
	nsr := "NSR02"
	if b.meta {
		nsr = "NSR03"
	}
	put(16, vsd("BEA01"))
	put(17, vsd(nsr))
	put(18, vsd("TEA01"))
	for i, d := range b.vds(32, partLen) {
		put(32+uint32(i), d)
	}
	for i, d := range b.vds(48, partLen) {
		put(48+uint32(i), d)
	}
	put(lvidSector, b.lvid(partLen))
	put(256, b.avdp(256))
	put(total-1, b.avdp(total-1))
	feBase := uint32(partStart)
	if b.meta {
		feBase += 2
	}
	for blk, d := range b.feSpace {
		put(feBase+blk, d)
	}
	for blk, d := range b.phys {
		put(partStart+blk, d)
	}
	if b.meta {
		for i, ft := range []uint8{250, 251} { // metadata file, metadata mirror file
			put(partStart+uint32(i), b.entry(entryParams{
				loc: uint32(i), fileType: ft, links: 1,
				ads:      shortAD(0, b.metaLen*sector, 2),
				size:     uint64(b.metaLen) * sector,
				recorded: uint64(b.metaLen),
			}))
		}
	}
	return img
}

func vsd(id string) []byte {
	d := make([]byte, sector)
	copy(d[1:], id)
	d[6] = 1
	return d
}

func (b *builder) tag(id uint16, loc uint32, d []byte) []byte {
	ver := uint16(2)
	if b.meta {
		ver = 3
	}
	le16(d[0:], id)
	le16(d[2:], ver)
	le16(d[6:], 1) // serial number
	le16(d[8:], crc16(d[16:]))
	le16(d[10:], uint16(len(d)-16))
	le32(d[12:], loc)
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	d[4] = sum
	return d
}

// crc16 is the ECMA-167 descriptor CRC: polynomial 0x1021, initial 0.
func crc16(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c ^= uint16(x) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

func le16(d []byte, v uint16) { binary.LittleEndian.PutUint16(d, v) }
func le32(d []byte, v uint32) { binary.LittleEndian.PutUint32(d, v) }
func le64(d []byte, v uint64) { binary.LittleEndian.PutUint64(d, v) }

func shortAD(typ uint8, length, loc uint32) []byte {
	d := make([]byte, 8)
	le32(d, uint32(typ)<<30|length)
	le32(d[4:], loc)
	return d
}

func longAD(typ uint8, length, loc uint32, ref uint16) []byte {
	d := make([]byte, 16)
	le32(d, uint32(typ)<<30|length)
	le32(d[4:], loc)
	le16(d[8:], ref)
	return d
}

func regid(dst []byte, id string, suffix []byte) {
	copy(dst[1:24], id)
	copy(dst[24:32], suffix)
}

func (b *builder) udfRegid(dst []byte, id string) {
	regid(dst, id, binary.LittleEndian.AppendUint16(nil, b.opt.Revision))
}

func (b *builder) domain(dst []byte) { b.udfRegid(dst, "*OSTA UDF Compliant") }

func charspec(d []byte) {
	d[0] = 0
	copy(d[1:], "OSTA Compressed Unicode")
}

func timestamp(d []byte) {
	le16(d, 1<<12) // local time with a UTC offset of 0
	le16(d[2:], uint16(stamp.Year()))
	d[4] = byte(stamp.Month())
	d[5] = byte(stamp.Day())
	d[6] = byte(stamp.Hour())
	d[7] = byte(stamp.Minute())
	d[8] = byte(stamp.Second())
}

// encodeName returns OSTA CS0 d-characters: compression ID 8 (one byte
// per character) when every rune fits in Latin-1, otherwise 16 (UTF-16BE).
func encodeName(s string) []byte {
	latin1 := true
	for _, r := range s {
		if r > 0xFF {
			latin1 = false
			break
		}
	}
	if latin1 {
		out := []byte{8}
		for _, r := range s {
			out = append(out, byte(r))
		}
		return out
	}
	out := []byte{16}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

// dstring fills a fixed-size dstring field; the last byte holds the
// number of bytes used.
func dstring(dst []byte, s string) {
	if s == "" {
		return
	}
	e := encodeName(s)
	if len(e) > len(dst)-1 {
		e = e[:len(dst)-1]
	}
	copy(dst, e)
	dst[len(dst)-1] = byte(len(e))
}
```

- [ ] **Step 5: Run unit tests to verify they pass**

Run: `go test -race ./internal/testdisc/udfimage/`
Expected: PASS.

- [ ] **Step 6: Run the macOS integration test**

Run: `go test -tags integration -run TestMacOSMountsImages -v ./internal/testdisc/udfimage/`
Expected: PASS for `udf102`, `udf250` and `udf250-embedded`.

If `hdiutil attach` refuses an image, rerun the attach by hand with `-verbose` and run `log show --last 2m --predicate 'process == "kernel"' | grep -i udf` to see the driver's complaint. Then fix the writer to match ECMA-167 / OSTA UDF 2.50; don't weaken the test. Fields worth checking first are the descriptor version (2 for NSR02, 3 for NSR03), the domain regid suffix revision, LVID sizes, and the metadata partition map.

- [ ] **Step 7: Commit**

```bash
git add internal/testdisc/udfimage
git commit -m "Add independent UDF image writer validated against macOS"
```

---

### Task 10: UDF primitives: tags, strings, timestamps

**Files:**
- Create: `udf/tag.go`, `udf/strings.go`, `udf/timestamp.go`
- Test: `udf/primitives_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `udf`):
  - `var ErrNotUDF, ErrCorrupt, ErrUnsupported error`
  - unexported: `sectorSize = 2048`, the tag ID constants (`tagPVD`=1, `tagAVDP`=2, `tagVDP`=3, `tagPD`=5, `tagLVD`=6, `tagTD`=8, `tagFSD`=256, `tagFID`=257, `tagAED`=258, `tagIE`=259, `tagFE`=261, `tagEFE`=266), `anyLocation`, `le16`, `le32`, `le64`, `crc16(b []byte) uint16`, `parseTag(d []byte, loc uint32) (uint16, error)`, `expectTag(d []byte, loc uint32, want ...uint16) (uint16, error)`, `decodeDchars(b []byte) (string, error)`, `decodeDstring(field []byte) (string, error)`, `decodeTimestamp(b []byte) time.Time`

- [ ] **Step 1: Write the failing tests**

`udf/primitives_test.go`:
```go
package udf

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// makeTag fills in the 16-byte descriptor tag at the start of d.
func makeTag(id uint16, loc uint32, d []byte) []byte {
	binary.LittleEndian.PutUint16(d[0:], id)
	binary.LittleEndian.PutUint16(d[2:], 3)
	binary.LittleEndian.PutUint16(d[8:], crc16(d[16:]))
	binary.LittleEndian.PutUint16(d[10:], uint16(len(d)-16))
	binary.LittleEndian.PutUint32(d[12:], loc)
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	d[4] = sum
	return d
}

func TestCRC16(t *testing.T) {
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("crc16 = %#x, want 0x31C3", got)
	}
}

func TestParseTag(t *testing.T) {
	d := make([]byte, 64)
	copy(d[16:], "payload")
	makeTag(tagFSD, 7, d)
	if id, err := parseTag(d, 7); err != nil || id != tagFSD {
		t.Fatalf("parseTag = %d, %v", id, err)
	}
	if _, err := parseTag(d, anyLocation); err != nil {
		t.Errorf("anyLocation: %v", err)
	}
	if _, err := parseTag(d, 8); !errors.Is(err, ErrCorrupt) {
		t.Errorf("wrong location err = %v", err)
	}
	if _, err := expectTag(d, 7, tagFE, tagEFE); !errors.Is(err, ErrCorrupt) {
		t.Errorf("unexpected id err = %v", err)
	}

	bad := append([]byte(nil), d...)
	bad[20] ^= 0xFF // payload byte: CRC mismatch
	if _, err := parseTag(bad, 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("CRC err = %v", err)
	}
	bad = append([]byte(nil), d...)
	bad[4] ^= 0xFF // checksum byte
	if _, err := parseTag(bad, 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("checksum err = %v", err)
	}
	if _, err := parseTag(d[:40], 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("CRC length past end err = %v", err)
	}
	if _, err := parseTag(d[:10], 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("short err = %v", err)
	}
}

func TestDecodeDchars(t *testing.T) {
	tests := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte{8, 'B', 'D', 'M', 'V'}, "BDMV"},
		{[]byte{254, 'a'}, "a"},
		{[]byte{8, 0xE9}, "é"},
		{[]byte{16, 0x30, 0xBF, 0x30, 0xA4}, "タイ"},
		{[]byte{255, 0x00, 'x'}, "x"},
	}
	for _, tt := range tests {
		got, err := decodeDchars(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("decodeDchars(%v) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range [][]byte{{7, 'a'}, {16, 0x30}} {
		if _, err := decodeDchars(bad); !errors.Is(err, ErrCorrupt) {
			t.Errorf("decodeDchars(%v) err = %v, want ErrCorrupt", bad, err)
		}
	}
}

func TestDecodeDstring(t *testing.T) {
	field := make([]byte, 32)
	copy(field, []byte{8, 'M', 'O', 'V', 'I', 'E'})
	field[31] = 6
	if got, err := decodeDstring(field); err != nil || got != "MOVIE" {
		t.Errorf("decodeDstring = %q, %v", got, err)
	}
	if got, err := decodeDstring(make([]byte, 32)); err != nil || got != "" {
		t.Errorf("empty dstring = %q, %v", got, err)
	}
	field[31] = 40
	if _, err := decodeDstring(field); !errors.Is(err, ErrCorrupt) {
		t.Errorf("overlong dstring err = %v", err)
	}
}

func TestDecodeTimestamp(t *testing.T) {
	b := make([]byte, 12)
	off := -300 // minutes: UTC-5
	binary.LittleEndian.PutUint16(b, 1<<12|uint16(off)&0x0FFF)
	binary.LittleEndian.PutUint16(b[2:], 2026)
	copy(b[4:], []byte{10, 1, 12, 30, 45, 50, 3, 7}) // 50 cs, 3 hundred-µs, 7 µs
	got := decodeTimestamp(b)
	want := time.Date(2026, 10, 1, 12, 30, 45, 500_307_000, time.FixedZone("", -5*3600))
	if !got.Equal(want) {
		t.Errorf("decodeTimestamp = %v, want %v", got, want)
	}
	if !decodeTimestamp(make([]byte, 12)).IsZero() {
		t.Error("all-zero timestamp should decode to the zero time")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./udf/`
Expected: FAIL to compile (`undefined: crc16`).

- [ ] **Step 3: Implement**

`udf/tag.go`:
```go
// Package udf reads UDF (ECMA-167 / OSTA UDF 1.02–2.60) file systems such
// as Blu-ray disc images, exposing them as a read-only io/fs.FS. It
// supports physical and metadata partitions, short, long and embedded
// allocation descriptors, and allocation extent chains. Sparable and
// virtual partitions are not supported.
package udf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const sectorSize = 2048

// Errors returned by Open and file operations; wrapped errors add detail.
var (
	ErrNotUDF      = errors.New("udf: no UDF volume found")
	ErrCorrupt     = errors.New("udf: corrupt structure")
	ErrUnsupported = errors.New("udf: unsupported feature")
)

// Descriptor tag identifiers (ECMA-167 3/7.2.1 and 4/7.2.1).
const (
	tagPVD  = 1
	tagAVDP = 2
	tagVDP  = 3
	tagPD   = 5
	tagLVD  = 6
	tagTD   = 8
	tagFSD  = 256
	tagFID  = 257
	tagAED  = 258
	tagIE   = 259
	tagFE   = 261
	tagEFE  = 266
)

// anyLocation disables parseTag's tag location check.
const anyLocation = math.MaxUint32

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// crc16 is the ECMA-167 descriptor CRC: polynomial 0x1021, initial 0.
func crc16(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c ^= uint16(x) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

// parseTag validates the descriptor tag at the start of d (checksum,
// location and CRC) and returns the tag identifier. loc is the logical
// block d was read from, or anyLocation.
func parseTag(d []byte, loc uint32) (uint16, error) {
	if len(d) < 16 {
		return 0, fmt.Errorf("%w: descriptor shorter than its tag", ErrCorrupt)
	}
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	if sum != d[4] {
		return 0, fmt.Errorf("%w: tag checksum mismatch (block %d)", ErrCorrupt, loc)
	}
	if got := le32(d[12:]); loc != anyLocation && got != loc {
		return 0, fmt.Errorf("%w: tag location %d read from block %d", ErrCorrupt, got, loc)
	}
	end := 16 + int(le16(d[10:]))
	if end > len(d) {
		return 0, fmt.Errorf("%w: tag CRC length %d exceeds descriptor", ErrCorrupt, end-16)
	}
	if crc16(d[16:end]) != le16(d[8:]) {
		return 0, fmt.Errorf("%w: tag CRC mismatch (block %d)", ErrCorrupt, loc)
	}
	return le16(d), nil
}

// expectTag is parseTag plus a check that the identifier is one of want.
func expectTag(d []byte, loc uint32, want ...uint16) (uint16, error) {
	id, err := parseTag(d, loc)
	if err != nil {
		return 0, err
	}
	for _, w := range want {
		if id == w {
			return id, nil
		}
	}
	return 0, fmt.Errorf("%w: descriptor tag %d at block %d, want %v", ErrCorrupt, id, loc, want)
}
```

`udf/strings.go`:
```go
package udf

import (
	"fmt"
	"unicode/utf16"
)

// decodeDchars decodes OSTA CS0 d-characters: a compression ID (8 or 254
// for one byte per character, 16 or 255 for UTF-16BE) followed by data.
func decodeDchars(b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	data := b[1:]
	switch b[0] {
	case 8, 254:
		rs := make([]rune, len(data))
		for i, c := range data {
			rs[i] = rune(c)
		}
		return string(rs), nil
	case 16, 255:
		if len(data)%2 != 0 {
			return "", fmt.Errorf("%w: odd-length UTF-16 name", ErrCorrupt)
		}
		u := make([]uint16, len(data)/2)
		for i := range u {
			u[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
		}
		return string(utf16.Decode(u)), nil
	default:
		return "", fmt.Errorf("%w: character compression ID %d", ErrCorrupt, b[0])
	}
}

// decodeDstring decodes a fixed-size dstring field whose last byte holds
// the number of bytes used.
func decodeDstring(field []byte) (string, error) {
	if len(field) == 0 {
		return "", nil
	}
	n := int(field[len(field)-1])
	if n == 0 {
		return "", nil
	}
	if n > len(field)-1 {
		return "", fmt.Errorf("%w: dstring length %d exceeds field", ErrCorrupt, n)
	}
	return decodeDchars(field[:n])
}
```

`udf/timestamp.go`:
```go
package udf

import "time"

// decodeTimestamp decodes a 12-byte ECMA-167 timestamp. Local-time stamps
// carry a UTC offset in minutes (-2047 means unspecified, read as UTC).
// An all-zero timestamp decodes to the zero time.
func decodeTimestamp(b []byte) time.Time {
	month := b[4]
	if month == 0 {
		return time.Time{}
	}
	loc := time.UTC
	tz := le16(b)
	if tz>>12 == 1 {
		off := int(tz & 0x0FFF)
		if off&0x0800 != 0 {
			off -= 0x1000
		}
		if off != -2047 {
			loc = time.FixedZone("", off*60)
		}
	}
	ns := int(b[9])*10_000_000 + int(b[10])*100_000 + int(b[11])*1000
	return time.Date(int(int16(le16(b[2:]))), time.Month(month), int(b[5]),
		int(b[6]), int(b[7]), int(b[8]), ns, loc)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./udf/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add udf
git commit -m "Add UDF descriptor tag, string and timestamp decoding"
```

---

### Task 11: UDF volume structure and file entries (`udf.Open`)

**Files:**
- Create: `udf/volume.go`, `udf/entry.go`
- Test: `udf/volume_test.go`

**Interfaces:**
- Consumes: Task 10 primitives; `udfimage.Build` (Task 9) in tests.
- Produces:
  - `type FS struct` (unexported fields) with `func Open(r io.ReaderAt, size int64) (*FS, error)` and `func (f *FS) Label() string`
  - unexported, used by Task 12:
    - `type partition struct { number uint16; start, length uint32; isMeta bool; metaLoc, mirrorLoc uint32; meta *entry }`
    - `type longAD struct { length uint32; typ uint8; block uint32; ref uint16 }`, `parseLongAD(b []byte) longAD`
    - `(f *FS) readAt(ref uint16, block uint32, off int64, p []byte) error`
    - `type extent struct { length uint32; typ uint8; ref uint16; block uint32 }`
    - `type entry struct { fs *FS; fileType uint8; size int64; modTime time.Time; inline bool; embedded []byte; extents []extent }` with `(e *entry) ReadAt(p []byte, off int64) (int, error)` and `(e *entry) addExtents(ads []byte, long bool, ref uint16, depth int) error`
    - `(f *FS) readEntry(ref uint16, block uint32) (*entry, error)`
    - constants `fileTypeDirectory = 4`, `fileTypeMetadata = 250`, `fileTypeMetadataMirror = 251`
    - `f.root *entry`

- [ ] **Step 1: Write the failing tests**

`udf/volume_test.go`:
```go
package udf_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/2048)
	}
	return b
}

var sample = map[string]udfimage.File{
	"BDMV/index.bdmv":          {Data: []byte("INDX0200 index")},
	"BDMV/MovieObject.bdmv":    {Data: []byte("MOBJ0200")},
	"BDMV/PLAYLIST/00800.mpls": {Data: bytes.Repeat([]byte("p"), 5000)},
	"BDMV/STREAM/00001.m2ts":   {Data: pattern(3*2048 + 17)},
	"BDMV/META/DL/タイトル.txt":     {Data: []byte("unicode name")},
	"BDMV/BACKUP/empty.bin":    {},
}

var layouts = []struct {
	name string
	opt  udfimage.Options
}{
	{"udf102", udfimage.Options{Revision: 0x0102, Label: "ZENVIK_102"}},
	{"udf102-embedded", udfimage.Options{Revision: 0x0102, Label: "ZENVIK_102", Embed: true}},
	{"udf250", udfimage.Options{Revision: 0x0250, Label: "ZENVIK_250"}},
	{"udf250-embedded", udfimage.Options{Revision: 0x0250, Label: "ZENVIK_250", Embed: true}},
}

func buildImage(t testing.TB, files map[string]udfimage.File, opt udfimage.Options) []byte {
	t.Helper()
	img, err := udfimage.Build(files, opt)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func openImage(t testing.TB, img []byte) *udf.FS {
	t.Helper()
	fsys, err := udf.Open(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	return fsys
}

func TestOpenLabel(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, sample, l.opt))
			if fsys.Label() != l.opt.Label {
				t.Errorf("Label = %q, want %q", fsys.Label(), l.opt.Label)
			}
		})
	}
}

func TestOpenNotUDF(t *testing.T) {
	img := make([]byte, 1<<20)
	if _, err := udf.Open(bytes.NewReader(img), int64(len(img))); !errors.Is(err, udf.ErrNotUDF) {
		t.Errorf("err = %v, want ErrNotUDF", err)
	}
	if _, err := udf.Open(bytes.NewReader(nil), 0); !errors.Is(err, udf.ErrNotUDF) {
		t.Errorf("empty: err = %v, want ErrNotUDF", err)
	}
}

func TestOpenFallsBackToLastAnchor(t *testing.T) {
	img := buildImage(t, sample, layouts[2].opt)
	clear(img[256*2048 : 257*2048])
	fsys := openImage(t, img)
	if fsys.Label() != "ZENVIK_250" {
		t.Errorf("Label = %q", fsys.Label())
	}
}

func TestOpenFallsBackToReserveSequence(t *testing.T) {
	img := buildImage(t, sample, layouts[2].opt)
	img[35*2048+100] ^= 0xFF // corrupt the main LVD
	fsys := openImage(t, img)
	if fsys.Label() != "ZENVIK_250" {
		t.Errorf("Label = %q", fsys.Label())
	}
}

func TestOpenRejectsBadPartitionMapTable(t *testing.T) {
	img := buildImage(t, sample, layouts[0].opt)
	for _, lvd := range []int{35, 48 + 3} { // main and reserve LVD
		d := img[lvd*2048 : (lvd+1)*2048]
		d[264], d[265], d[266], d[267] = 0xFF, 0xFF, 0, 0 // map table length 65535
		fixTag(d)
	}
	if _, err := udf.Open(bytes.NewReader(img), int64(len(img))); !errors.Is(err, udf.ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}
```

Append a test-only tag fixer, `udf/fixtag_test.go`:
```go
package udf_test

import "encoding/binary"

// fixTag recomputes the CRC and checksum of the descriptor tag at the
// start of d after a test edits the descriptor body.
func fixTag(d []byte) {
	n := int(binary.LittleEndian.Uint16(d[10:]))
	var c uint16
	for _, x := range d[16 : 16+n] {
		c ^= uint16(x) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	binary.LittleEndian.PutUint16(d[8:], c)
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	d[4] = sum
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./udf/`
Expected: FAIL to compile (`undefined: udf.Open`).

- [ ] **Step 3: Implement the volume structure**

`udf/volume.go`:
```go
package udf

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

type partition struct {
	number    uint16 // partition number (type 1 maps; target partition for metadata maps)
	start     uint32 // first sector (type 1)
	length    uint32 // length in blocks (type 1)
	isMeta    bool
	metaLoc   uint32 // metadata file ICB block in the physical partition
	mirrorLoc uint32 // metadata mirror file ICB block
	meta      *entry // loaded metadata file
}

// FS is a read-only UDF file system. It implements io/fs.FS; files also
// implement io.ReaderAt and io.Seeker. An FS is safe for concurrent use if
// the underlying io.ReaderAt is.
type FS struct {
	r     io.ReaderAt
	size  int64
	label string
	parts []partition // indexed by partition reference number
	root  *entry
}

type extentAD struct{ length, loc uint32 }

type longAD struct {
	length uint32
	typ    uint8
	block  uint32
	ref    uint16
}

func parseLongAD(b []byte) longAD {
	raw := le32(b)
	return longAD{length: raw & 0x3FFFFFFF, typ: uint8(raw >> 30), block: le32(b[4:]), ref: le16(b[8:])}
}

// Open reads the UDF volume in r, an image of size bytes.
func Open(r io.ReaderAt, size int64) (*FS, error) {
	f := &FS{r: r, size: size}
	if err := f.checkVRS(); err != nil {
		return nil, err
	}
	main, reserve, err := f.findAnchor()
	if err != nil {
		return nil, err
	}
	if err := f.readVDS(main); err != nil {
		if err2 := f.readVDS(reserve); err2 != nil {
			return nil, err
		}
	}
	return f, nil
}

// Label returns the volume identifier.
func (f *FS) Label() string { return f.label }

func readFull(r io.ReaderAt, p []byte, off int64) error {
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("udf: read %d bytes at offset %d: %w", len(p), off, err)
}

func (f *FS) sector(n uint32) ([]byte, error) {
	d := make([]byte, sectorSize)
	return d, readFull(f.r, d, int64(n)*sectorSize)
}

// checkVRS looks for an NSR descriptor in the volume recognition sequence.
func (f *FS) checkVRS() error {
scan:
	for s := uint32(16); s < 16+64; s++ {
		d, err := f.sector(s)
		if err != nil {
			break
		}
		switch string(d[1:6]) {
		case "NSR02", "NSR03":
			return nil
		case "BEA01", "TEA01", "CD001", "CDW02", "BOOT2":
		default:
			break scan
		}
	}
	return ErrNotUDF
}

func (f *FS) findAnchor() (main, reserve extentAD, err error) {
	candidates := []uint32{256}
	if n := f.size / sectorSize; n > 257 {
		candidates = append(candidates, uint32(n-1), uint32(n-257))
	}
	for _, loc := range candidates {
		d, err := f.sector(loc)
		if err != nil {
			continue
		}
		if _, err := expectTag(d, loc, tagAVDP); err != nil {
			continue
		}
		return extentAD{le32(d[16:]), le32(d[20:])}, extentAD{le32(d[24:]), le32(d[28:])}, nil
	}
	return extentAD{}, extentAD{}, fmt.Errorf("%w: no anchor volume descriptor pointer", ErrNotUDF)
}

// readVDS reads one volume descriptor sequence and, if it is complete,
// mounts the logical volume it describes.
func (f *FS) readVDS(e extentAD) error {
	var pvd, lvd []byte
	pds := map[uint16][]byte{}
	loc, end := e.loc, uint64(e.loc)+uint64(e.length/sectorSize)
	for hops := 0; uint64(loc) < end; loc++ {
		d, err := f.sector(loc)
		if err != nil {
			return err
		}
		id, err := parseTag(d, loc)
		if err != nil || id == tagTD {
			break // a terminating descriptor or an unrecorded sector ends the sequence
		}
		switch id {
		case tagPVD:
			if pvd == nil || le32(d[16:]) >= le32(pvd[16:]) {
				pvd = d
			}
		case tagLVD:
			if lvd == nil || le32(d[16:]) >= le32(lvd[16:]) {
				lvd = d
			}
		case tagPD:
			num := le16(d[22:])
			if old, ok := pds[num]; !ok || le32(d[16:]) >= le32(old[16:]) {
				pds[num] = d
			}
		case tagVDP:
			if hops++; hops > 16 {
				return fmt.Errorf("%w: volume descriptor pointer loop", ErrCorrupt)
			}
			next := extentAD{length: le32(d[20:]), loc: le32(d[24:])}
			loc, end = next.loc-1, uint64(next.loc)+uint64(next.length/sectorSize)
		}
	}
	if lvd == nil || len(pds) == 0 {
		return fmt.Errorf("%w: incomplete volume descriptor sequence", ErrCorrupt)
	}
	return f.mount(pvd, lvd, pds)
}

func (f *FS) mount(pvd, lvd []byte, pds map[uint16][]byte) error {
	if bs := le32(lvd[212:]); bs != sectorSize {
		return fmt.Errorf("%w: logical block size %d", ErrUnsupported, bs)
	}
	field := lvd[84:212]
	if pvd != nil {
		field = pvd[24:56]
	}
	label, err := decodeDstring(field)
	if err != nil {
		return err
	}
	parts, err := parsePartitionMaps(lvd, pds)
	if err != nil {
		return err
	}
	f.parts = parts
	for i := range f.parts {
		if err := f.loadMetadata(&f.parts[i]); err != nil {
			return err
		}
	}

	fsdAD := parseLongAD(lvd[248:264])
	d := make([]byte, sectorSize)
	if err := f.readAt(fsdAD.ref, fsdAD.block, 0, d); err != nil {
		return err
	}
	if _, err := expectTag(d, fsdAD.block, tagFSD); err != nil {
		return err
	}
	rootAD := parseLongAD(d[400:416])
	root, err := f.readEntry(rootAD.ref, rootAD.block)
	if err != nil {
		return err
	}
	if root.fileType != fileTypeDirectory {
		return fmt.Errorf("%w: root is not a directory", ErrCorrupt)
	}
	f.root, f.label = root, label
	return nil
}

func parsePartitionMaps(lvd []byte, pds map[uint16][]byte) ([]partition, error) {
	tableLen := int64(le32(lvd[264:]))
	if 440+tableLen > int64(len(lvd)) {
		return nil, fmt.Errorf("%w: partition map table length %d", ErrCorrupt, tableLen)
	}
	table := lvd[440 : 440+tableLen]
	var parts []partition
	for i := uint32(0); i < le32(lvd[268:]); i++ {
		if len(table) < 2 || table[1] < 2 || int(table[1]) > len(table) {
			return nil, fmt.Errorf("%w: partition map %d", ErrCorrupt, i)
		}
		m := table[:table[1]]
		table = table[table[1]:]
		switch m[0] {
		case 1:
			if len(m) < 6 {
				return nil, fmt.Errorf("%w: short type 1 partition map", ErrCorrupt)
			}
			num := le16(m[4:])
			pd, ok := pds[num]
			if !ok {
				return nil, fmt.Errorf("%w: no partition descriptor for partition %d", ErrCorrupt, num)
			}
			parts = append(parts, partition{number: num, start: le32(pd[188:]), length: le32(pd[192:])})
		case 2:
			if len(m) < 64 {
				return nil, fmt.Errorf("%w: short type 2 partition map", ErrCorrupt)
			}
			if id := strings.TrimRight(string(m[5:28]), "\x00"); id != "*UDF Metadata Partition" {
				return nil, fmt.Errorf("%w: partition map %q", ErrUnsupported, id)
			}
			parts = append(parts, partition{
				number: le16(m[38:]), isMeta: true, metaLoc: le32(m[40:]), mirrorLoc: le32(m[44:]),
			})
		default:
			return nil, fmt.Errorf("%w: partition map type %d", ErrCorrupt, m[0])
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: no partition maps", ErrCorrupt)
	}
	return parts, nil
}

// loadMetadata reads the metadata file (or its mirror) backing a metadata
// partition. The file's extents must lie in the physical partition, which
// also rules out a metadata partition that refers to itself.
func (f *FS) loadMetadata(p *partition) error {
	if !p.isMeta {
		return nil
	}
	phys := -1
	for j, q := range f.parts {
		if !q.isMeta && q.number == p.number {
			phys = j
			break
		}
	}
	if phys < 0 {
		return fmt.Errorf("%w: metadata partition %d has no physical partition", ErrCorrupt, p.number)
	}
	var firstErr error
	for _, loc := range []uint32{p.metaLoc, p.mirrorLoc} {
		e, err := f.readEntry(uint16(phys), loc)
		if err == nil && e.fileType != fileTypeMetadata && e.fileType != fileTypeMetadataMirror {
			err = fmt.Errorf("%w: metadata file has file type %d", ErrCorrupt, e.fileType)
		}
		if err == nil {
			for _, x := range e.extents {
				if x.ref != uint16(phys) {
					err = fmt.Errorf("%w: metadata file extent outside physical partition", ErrCorrupt)
					break
				}
			}
		}
		if err == nil {
			p.meta = e
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// readAt fills p from partition ref, starting off bytes after the start
// of logical block block.
func (f *FS) readAt(ref uint16, block uint32, off int64, p []byte) error {
	if int(ref) >= len(f.parts) {
		return fmt.Errorf("%w: partition reference %d", ErrCorrupt, ref)
	}
	part := &f.parts[ref]
	start := int64(block)*sectorSize + off
	if part.isMeta {
		if part.meta == nil {
			return fmt.Errorf("%w: metadata partition used before it is loaded", ErrCorrupt)
		}
		n, err := part.meta.ReadAt(p, start)
		if n == len(p) {
			return nil
		}
		if err == nil || errors.Is(err, io.EOF) {
			err = fmt.Errorf("%w: read past end of metadata partition", ErrCorrupt)
		}
		return err
	}
	if start < 0 || start+int64(len(p)) > int64(part.length)*sectorSize {
		return fmt.Errorf("%w: read past end of partition %d", ErrCorrupt, ref)
	}
	return readFull(f.r, p, int64(part.start)*sectorSize+start)
}
```

- [ ] **Step 4: Implement file entries**

`udf/entry.go`:
```go
package udf

import (
	"fmt"
	"io"
	"math"
	"time"
)

const (
	fileTypeDirectory      = 4
	fileTypeMetadata       = 250
	fileTypeMetadataMirror = 251
)

type extent struct {
	length uint32
	typ    uint8 // 0 recorded, 1 allocated but not recorded, 2 not allocated
	ref    uint16
	block  uint32
}

// entry is a decoded file entry or extended file entry.
type entry struct {
	fs       *FS
	fileType uint8
	size     int64
	modTime  time.Time
	inline   bool   // data is embedded in the entry
	embedded []byte // embedded data when inline
	extents  []extent
}

type entryLayout struct{ header, size, modTime, lenEA, lenAD int }

var (
	feLayout  = entryLayout{header: 176, size: 56, modTime: 84, lenEA: 168, lenAD: 172}
	efeLayout = entryLayout{header: 216, size: 56, modTime: 92, lenEA: 208, lenAD: 212}
)

// readEntry reads the file entry at a logical block, following indirect
// entries.
func (f *FS) readEntry(ref uint16, block uint32) (*entry, error) {
	d := make([]byte, sectorSize)
	for range 8 {
		if err := f.readAt(ref, block, 0, d); err != nil {
			return nil, err
		}
		id, err := expectTag(d, block, tagFE, tagEFE, tagIE)
		if err != nil {
			return nil, err
		}
		switch id {
		case tagIE:
			ad := parseLongAD(d[36:52])
			ref, block = ad.ref, ad.block
		case tagFE:
			return f.decodeEntry(d, ref, feLayout)
		default:
			return f.decodeEntry(d, ref, efeLayout)
		}
	}
	return nil, fmt.Errorf("%w: too many indirect entries", ErrCorrupt)
}

func (f *FS) decodeEntry(d []byte, ref uint16, l entryLayout) (*entry, error) {
	size := le64(d[l.size:])
	if size > math.MaxInt64 {
		return nil, fmt.Errorf("%w: file size %d", ErrCorrupt, size)
	}
	e := &entry{fs: f, fileType: d[16+11], size: int64(size), modTime: decodeTimestamp(d[l.modTime:])}
	start := int64(l.header) + int64(le32(d[l.lenEA:]))
	end := start + int64(le32(d[l.lenAD:]))
	if end > int64(len(d)) {
		return nil, fmt.Errorf("%w: allocation descriptors overrun file entry", ErrCorrupt)
	}
	ads := d[start:end]
	switch adType := le16(d[16+18:]) & 7; adType {
	case 0, 1:
		if err := e.addExtents(ads, adType == 1, ref, 0); err != nil {
			return nil, err
		}
	case 3:
		if e.size > int64(len(ads)) {
			return nil, fmt.Errorf("%w: embedded data shorter than file size", ErrCorrupt)
		}
		e.inline = true
		e.embedded = append([]byte(nil), ads...)
	default:
		return nil, fmt.Errorf("%w: allocation descriptor type %d", ErrUnsupported, adType)
	}
	return e, nil
}

// addExtents appends the extents described by short (8-byte) or long
// (16-byte) allocation descriptors, following continuation extents into
// allocation extent descriptors.
func (e *entry) addExtents(ads []byte, long bool, ref uint16, depth int) error {
	size := 8
	if long {
		size = 16
	}
	for len(ads) >= size {
		raw := le32(ads)
		x := extent{length: raw & 0x3FFFFFFF, typ: uint8(raw >> 30), ref: ref, block: le32(ads[4:])}
		if long {
			x.ref = le16(ads[8:])
		}
		ads = ads[size:]
		if x.length == 0 {
			return nil
		}
		if x.typ != 3 {
			e.extents = append(e.extents, x)
			continue
		}
		if depth >= 16 {
			return fmt.Errorf("%w: allocation extent chain too long", ErrCorrupt)
		}
		d := make([]byte, sectorSize)
		if err := e.fs.readAt(x.ref, x.block, 0, d); err != nil {
			return err
		}
		if _, err := expectTag(d, x.block, tagAED); err != nil {
			return err
		}
		n := int64(le32(d[20:]))
		if 24+n > sectorSize {
			return fmt.Errorf("%w: allocation extent descriptor length %d", ErrCorrupt, n)
		}
		return e.addExtents(d[24:24+n], long, x.ref, depth+1)
	}
	return nil
}

// ReadAt reads file data. Unrecorded extents read as zeros.
func (e *entry) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("udf: negative offset %d", off)
	}
	if off >= e.size {
		return 0, io.EOF
	}
	want := p
	if rem := e.size - off; int64(len(want)) > rem {
		want = want[:rem]
	}
	if e.inline {
		n := copy(want, e.embedded[off:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
	done := 0
	var base int64
	for _, x := range e.extents {
		if done == len(want) {
			break
		}
		l := int64(x.length)
		pos := off + int64(done)
		if pos >= base+l {
			base += l
			continue
		}
		chunk := want[done:]
		if rem := base + l - pos; int64(len(chunk)) > rem {
			chunk = chunk[:rem]
		}
		if x.typ == 0 {
			if err := e.fs.readAt(x.ref, x.block, pos-base, chunk); err != nil {
				return done, err
			}
		} else {
			clear(chunk)
		}
		done += len(chunk)
		base += l
	}
	if done < len(want) {
		return done, fmt.Errorf("%w: file extents shorter than file size", ErrCorrupt)
	}
	if len(want) < len(p) {
		return done, io.EOF
	}
	return done, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./udf/ && go vet ./udf/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add udf
git commit -m "Add UDF volume structure, metadata partition and file entry reading"
```

---

### Task 12: UDF directories and the `fs.FS` implementation

**Files:**
- Create: `udf/dir.go`, `udf/fs.go`, `udf/image.go`
- Test: `udf/fs_test.go`, `udf/hostile_test.go`

**Interfaces:**
- Consumes: everything from Task 11.
- Produces (public API used by milestone 2's `internal/source`):
  - `func (f *FS) Open(name string) (fs.File, error)`. The returned files implement `fs.File`, `io.ReaderAt` and `io.Seeker`, and directories implement `fs.ReadDirFile`.
  - `type Image struct { *FS }` (it also holds the `*os.File`), with `func OpenImage(name string) (*Image, error)` and `func (i *Image) Close() error`
  - unexported: `type dirent struct { name string; dir bool; icb longAD }`, `(e *entry) readDir() ([]dirent, error)`, `maxDirSize = 64 << 20`

- [ ] **Step 1: Write the failing tests**

`udf/fs_test.go`:
```go
package udf_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func sampleNames() []string {
	var names []string
	for n := range sample {
		names = append(names, n)
	}
	return names
}

func TestFSConformance(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, sample, l.opt))
			if err := fstest.TestFS(fsys, sampleNames()...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFSContents(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, sample, l.opt))
			for name, f := range sample {
				got, err := fs.ReadFile(fsys, name)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if !bytes.Equal(got, f.Data) {
					t.Errorf("%s: contents differ (%d bytes, want %d)", name, len(got), len(f.Data))
				}
			}
			ents, err := fs.ReadDir(fsys, "BDMV")
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, e := range ents {
				names = append(names, e.Name())
			}
			want := []string{"BACKUP", "META", "MovieObject.bdmv", "PLAYLIST", "STREAM", "index.bdmv"}
			if !reflect.DeepEqual(names, want) {
				t.Errorf("ReadDir(BDMV) = %v, want %v", names, want)
			}
			if _, err := fsys.Open("BDMV/missing.bin"); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing file err = %v", err)
			}
			if _, err := fsys.Open("BDMV/index.bdmv/child"); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("path through file err = %v", err)
			}
			if _, err := fsys.Open("/BDMV"); !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("invalid path err = %v", err)
			}
		})
	}
}

func TestReadAtAndSeek(t *testing.T) {
	fsys := openImage(t, buildImage(t, sample, layouts[2].opt))
	f, err := fsys.Open("BDMV/STREAM/00001.m2ts")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := sample["BDMV/STREAM/00001.m2ts"].Data
	ra := f.(io.ReaderAt)
	buf := make([]byte, 100)
	if n, err := ra.ReadAt(buf, 2000); n != 100 || err != nil || !bytes.Equal(buf, want[2000:2100]) {
		t.Errorf("ReadAt across block boundary = %d, %v", n, err)
	}
	if n, err := ra.ReadAt(buf, int64(len(want))-10); n != 10 || !errors.Is(err, io.EOF) {
		t.Errorf("ReadAt at end = %d, %v; want 10, EOF", n, err)
	}
	sk := f.(io.Seeker)
	if pos, err := sk.Seek(-17, io.SeekEnd); err != nil || pos != int64(len(want))-17 {
		t.Fatalf("Seek = %d, %v", pos, err)
	}
	rest, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(rest, want[len(want)-17:]) {
		t.Errorf("read after seek = %d bytes, %v", len(rest), err)
	}
}

func TestSparseHugeFileWithAllocationExtent(t *testing.T) {
	const size = 5 << 30 // 5 GiB: more than 4 GiB and five extents
	files := map[string]udfimage.File{
		"BDMV/STREAM/00005.m2ts": {SparseSize: size},
		"BDMV/index.bdmv":        {Data: []byte("INDX0200")},
	}
	for _, rev := range []uint16{0x0102, 0x0250} {
		img := buildImage(t, files, udfimage.Options{Revision: rev, MaxInlineADs: 2})
		fsys := openImage(t, img)
		info, err := fs.Stat(fsys, "BDMV/STREAM/00005.m2ts")
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != size {
			t.Errorf("rev %#x: size = %d, want %d", rev, info.Size(), size)
		}
		f, err := fsys.Open("BDMV/STREAM/00005.m2ts")
		if err != nil {
			t.Fatal(err)
		}
		buf := bytes.Repeat([]byte{0xAA}, 64)
		if n, err := f.(io.ReaderAt).ReadAt(buf, size/2+size/4); n != 64 || err != nil || !bytes.Equal(buf, make([]byte, 64)) {
			t.Errorf("rev %#x: sparse ReadAt = %d, %v, %x", rev, n, err, buf[:8])
		}
		f.Close()
		if got, err := fs.ReadFile(fsys, "BDMV/index.bdmv"); err != nil || string(got) != "INDX0200" {
			t.Errorf("rev %#x: index.bdmv = %q, %v", rev, got, err)
		}
	}
}

func TestTruncatedImageNeverReturnsWrongData(t *testing.T) {
	for _, l := range layouts {
		img := buildImage(t, sample, l.opt)
		for _, keep := range []int{len(img) - 2*2048, len(img) - 4*2048, len(img) / 2, 300 * 2048} {
			if keep <= 0 || keep >= len(img) {
				continue
			}
			cut := img[:keep]
			fsys, err := udf.Open(bytes.NewReader(cut), int64(len(cut)))
			if err != nil {
				continue // refusing to open a damaged image is acceptable
			}
			for name, f := range sample {
				got, err := fs.ReadFile(fsys, name)
				if err == nil && !bytes.Equal(got, f.Data) {
					t.Errorf("%s keep=%d: %s returned wrong data without an error", l.name, keep, name)
				}
			}
		}
	}
	// In the non-embedded 2.50 layout, index.bdmv's data block is the last
	// block before the trailing anchor, so cutting two sectors removes it.
	img := buildImage(t, sample, layouts[2].opt)
	cut := img[:len(img)-2*2048]
	fsys := openImage(t, cut)
	if _, err := fs.ReadFile(fsys, "BDMV/index.bdmv"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated data read err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestOpenImage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "disc.iso")
	if err := os.WriteFile(p, buildImage(t, sample, layouts[2].opt), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := udf.OpenImage(p)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if got, err := fs.ReadFile(img, "BDMV/index.bdmv"); err != nil || string(got) != "INDX0200 index" {
		t.Errorf("ReadFile = %q, %v", got, err)
	}
	if _, err := udf.OpenImage(filepath.Join(t.TempDir(), "missing.iso")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing image err = %v", err)
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(buildImage(f, map[string]udfimage.File{"A/b.txt": {Data: []byte("hi")}}, udfimage.Options{Revision: 0x0102}))
	f.Add(buildImage(f, map[string]udfimage.File{"A/b.txt": {Data: []byte("hi")}}, udfimage.Options{Revision: 0x0250, Embed: true}))
	f.Fuzz(func(t *testing.T, img []byte) {
		fsys, err := udf.Open(bytes.NewReader(img), int64(len(img)))
		if err != nil {
			return
		}
		walk(fsys, ".", 0)
	})
}

// walk visits a bounded part of the tree, reading up to 1 MiB per file.
// The depth limit keeps hostile directory cycles from recursing forever.
func walk(fsys fs.FS, dir string, depth int) {
	if depth > 6 {
		return
	}
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return
	}
	for i, e := range ents {
		if i > 64 {
			return
		}
		p := e.Name()
		if dir != "." {
			p = dir + "/" + p
		}
		if !fs.ValidPath(p) {
			continue
		}
		if e.IsDir() {
			walk(fsys, p, depth+1)
			continue
		}
		if fl, err := fsys.Open(p); err == nil {
			_, _ = io.CopyN(io.Discard, fl, 1<<20)
			fl.Close()
		}
	}
}
```

`udf/hostile_test.go`:
```go
package udf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// hostileFS returns an FS whose single physical partition starts at
// sector 0 of an image made of the given sectors.
func hostileFS(sectors ...[]byte) *FS {
	img := make([]byte, len(sectors)*sectorSize)
	for i, s := range sectors {
		copy(img[i*sectorSize:], s)
	}
	return &FS{
		r:     bytes.NewReader(img),
		size:  int64(len(img)),
		parts: []partition{{start: 0, length: uint32(len(sectors))}},
	}
}

func TestAEDChainLoopIsCorrupt(t *testing.T) {
	ad := make([]byte, 8)
	binary.LittleEndian.PutUint32(ad, 3<<30|sectorSize) // continuation at block 0
	aed := make([]byte, 24+len(ad))
	binary.LittleEndian.PutUint32(aed[20:], uint32(len(ad)))
	copy(aed[24:], ad)
	f := hostileFS(makeTag(tagAED, 0, aed))
	e := &entry{fs: f}
	if err := e.addExtents(ad, false, 0, 0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestIndirectEntryLoopIsCorrupt(t *testing.T) {
	ie := make([]byte, 52)
	binary.LittleEndian.PutUint32(ie[36:], sectorSize) // indirect ICB → block 0, partition 0
	f := hostileFS(makeTag(tagIE, 0, ie))
	if _, err := f.readEntry(0, 0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestHugeDirectoryIsCorrupt(t *testing.T) {
	e := &entry{fs: hostileFS(make([]byte, sectorSize)), fileType: fileTypeDirectory, size: 1 << 40}
	if _, err := e.readDir(); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestReadOutsidePartitionIsCorrupt(t *testing.T) {
	f := hostileFS(make([]byte, sectorSize))
	if err := f.readAt(0, 1, 0, make([]byte, 16)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
	if err := f.readAt(3, 0, 0, make([]byte, 16)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("bad reference err = %v, want ErrCorrupt", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./udf/`
Expected: FAIL to compile (`fsys.Open undefined`, `undefined: udf.OpenImage`, `e.readDir undefined`).

- [ ] **Step 3: Implement directory parsing**

`udf/dir.go`:
```go
package udf

import (
	"errors"
	"fmt"
	"io"
)

// maxDirSize bounds how much directory data is read into memory.
const maxDirSize = 64 << 20

// File characteristics in a file identifier descriptor.
const (
	fidDirectory = 0x02
	fidDeleted   = 0x04
	fidParent    = 0x08
)

var errNotDir = errors.New("not a directory")

type dirent struct {
	name string
	dir  bool
	icb  longAD
}

// readDir parses the file identifier descriptors of a directory, skipping
// the parent entry and deleted entries.
func (e *entry) readDir() ([]dirent, error) {
	if e.fileType != fileTypeDirectory {
		return nil, errNotDir
	}
	if e.size > maxDirSize {
		return nil, fmt.Errorf("%w: directory of %d bytes", ErrCorrupt, e.size)
	}
	data := make([]byte, e.size)
	if _, err := e.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var out []dirent
	for len(data) > 0 && !allZero(data) {
		if len(data) < 38 {
			return nil, fmt.Errorf("%w: truncated file identifier", ErrCorrupt)
		}
		lfi, liu := int(data[19]), int(le16(data[36:]))
		used := 38 + liu + lfi
		if used > len(data) {
			return nil, fmt.Errorf("%w: file identifier overruns directory", ErrCorrupt)
		}
		total := min((used+3)&^3, len(data))
		if _, err := expectTag(data[:total], anyLocation, tagFID); err != nil {
			return nil, err
		}
		chars := data[18]
		nameBytes := data[38+liu : used]
		icb := parseLongAD(data[20:36])
		data = data[total:]
		if chars&(fidParent|fidDeleted) != 0 {
			continue
		}
		name, err := decodeDchars(nameBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, dirent{name: name, dir: chars&fidDirectory != 0, icb: icb})
	}
	return out, nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Implement the file system**

`udf/fs.go`:
```go
package udf

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

var errIsDir = errors.New("is a directory")

// Open implements fs.FS.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	e, err := f.lookup(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &file{fs: f, e: e, name: path.Base(name)}, nil
}

func (f *FS) lookup(name string) (*entry, error) {
	e := f.root
	if name == "." {
		return e, nil
	}
	for _, part := range strings.Split(name, "/") {
		if e.fileType != fileTypeDirectory {
			return nil, fs.ErrNotExist
		}
		ents, err := e.readDir()
		if err != nil {
			return nil, err
		}
		var next *entry
		for _, de := range ents {
			if de.name == part {
				if next, err = f.readEntry(de.icb.ref, de.icb.block); err != nil {
					return nil, err
				}
				break
			}
		}
		if next == nil {
			return nil, fs.ErrNotExist
		}
		e = next
	}
	return e, nil
}

type file struct {
	fs     *FS
	e      *entry
	name   string
	off    int64
	ents   []fs.DirEntry
	loaded bool
	dirPos int
}

func (fl *file) Stat() (fs.FileInfo, error) { return &fileInfo{name: fl.name, e: fl.e}, nil }

func (fl *file) Close() error { return nil }

func (fl *file) Read(p []byte) (int, error) {
	if fl.e.fileType == fileTypeDirectory {
		return 0, &fs.PathError{Op: "read", Path: fl.name, Err: errIsDir}
	}
	n, err := fl.e.ReadAt(p, fl.off)
	fl.off += int64(n)
	return n, err
}

func (fl *file) ReadAt(p []byte, off int64) (int, error) {
	if fl.e.fileType == fileTypeDirectory {
		return 0, &fs.PathError{Op: "read", Path: fl.name, Err: errIsDir}
	}
	return fl.e.ReadAt(p, off)
}

func (fl *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += fl.off
	case io.SeekEnd:
		offset += fl.e.size
	default:
		return 0, &fs.PathError{Op: "seek", Path: fl.name, Err: fs.ErrInvalid}
	}
	if offset < 0 {
		return 0, &fs.PathError{Op: "seek", Path: fl.name, Err: fs.ErrInvalid}
	}
	fl.off = offset
	return offset, nil
}

// ReadDir implements fs.ReadDirFile; entries are sorted by name.
func (fl *file) ReadDir(n int) ([]fs.DirEntry, error) {
	if !fl.loaded {
		ents, err := fl.e.readDir()
		if err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: fl.name, Err: err}
		}
		for _, de := range ents {
			fl.ents = append(fl.ents, &dirEntry{fs: fl.fs, d: de})
		}
		sort.Slice(fl.ents, func(i, j int) bool { return fl.ents[i].Name() < fl.ents[j].Name() })
		fl.loaded = true
	}
	rest := fl.ents[fl.dirPos:]
	if n <= 0 {
		fl.dirPos = len(fl.ents)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	fl.dirPos += len(rest)
	return rest, nil
}

type dirEntry struct {
	fs *FS
	d  dirent
}

func (de *dirEntry) Name() string { return de.d.name }
func (de *dirEntry) IsDir() bool  { return de.d.dir }

func (de *dirEntry) Type() fs.FileMode {
	if de.d.dir {
		return fs.ModeDir
	}
	return 0
}

func (de *dirEntry) Info() (fs.FileInfo, error) {
	e, err := de.fs.readEntry(de.d.icb.ref, de.d.icb.block)
	if err != nil {
		return nil, err
	}
	return &fileInfo{name: de.d.name, e: e}, nil
}

func (de *dirEntry) String() string { return fs.FormatDirEntry(de) }

type fileInfo struct {
	name string
	e    *entry
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.e.size }
func (fi *fileInfo) ModTime() time.Time { return fi.e.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.e.fileType == fileTypeDirectory }
func (fi *fileInfo) Sys() any           { return nil }
func (fi *fileInfo) String() string     { return fs.FormatFileInfo(fi) }

func (fi *fileInfo) Mode() fs.FileMode {
	if fi.IsDir() {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
```

`udf/image.go`:
```go
package udf

import "os"

// Image is a UDF file system read from a disc image file.
type Image struct {
	*FS
	f *os.File
}

// OpenImage opens the disc image at name.
func OpenImage(name string) (*Image, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	fsys, err := Open(f, st.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Image{FS: fsys, f: f}, nil
}

// Close closes the image file.
func (i *Image) Close() error { return i.f.Close() }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./udf/ && go test ./udf/ -run '^$' -fuzz FuzzOpen -fuzztime 60s && go vet ./... && golangci-lint run`
Expected: PASS; no fuzz failures; no lint findings. If fuzzing finds a crash, fix the root cause in `udf`; the failing input is saved under `udf/testdata/fuzz/FuzzOpen/`, so commit it as a regression seed.

- [ ] **Step 6: Commit**

```bash
git add udf
git commit -m "Add UDF directory reading and io/fs implementation"
```

---

### Task 13: Externally produced UDF 1.02 fixture

**Files:**
- Create: `scripts/gen-udf-fixtures.sh`, `udf/testdata/hdiutil-udf102.iso` (generated)
- Test: `udf/hdiutil_fixture_test.go`

**Interfaces:**
- Consumes: `udf.OpenImage` (Task 12).
- Produces: a committed fixture made by a UDF writer other than ours.

- [ ] **Step 1: Write the generator script**

`scripts/gen-udf-fixtures.sh`:
```sh
#!/bin/sh
# Regenerates udf/testdata/hdiutil-udf102.iso with macOS hdiutil (UDF 1.02).
# The file contents must match udf/hdiutil_fixture_test.go.
set -eu
cd "$(dirname "$0")/.."
src=$(mktemp -d)
trap 'rm -rf "$src"' EXIT
mkdir -p "$src/BDMV/PLAYLIST" "$src/BDMV/STREAM"
printf 'MPLS0200' > "$src/BDMV/PLAYLIST/00800.mpls"
printf 'hello from hdiutil\n' > "$src/BDMV/hello.txt"
head -c 300000 /dev/zero | LC_ALL=C tr '\0' 'z' > "$src/BDMV/STREAM/00001.m2ts"
mkdir -p udf/testdata
rm -f udf/testdata/hdiutil-udf102.iso
hdiutil makehybrid -quiet -udf -udf-version 1.02 -udf-volume-name ZENVIK_HDIUTIL \
  -o udf/testdata/hdiutil-udf102.iso "$src"
ls -l udf/testdata/hdiutil-udf102.iso
```

- [ ] **Step 2: Write the failing test**

`udf/hdiutil_fixture_test.go`:
```go
package udf_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/chad3814/zenvik/udf"
)

// TestHdiutilFixture reads an image written by macOS hdiutil, an
// independent UDF implementation (see scripts/gen-udf-fixtures.sh).
func TestHdiutilFixture(t *testing.T) {
	img, err := udf.OpenImage("testdata/hdiutil-udf102.iso")
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.Label() != "ZENVIK_HDIUTIL" {
		t.Errorf("Label = %q", img.Label())
	}
	want := map[string][]byte{
		"BDMV/PLAYLIST/00800.mpls": []byte("MPLS0200"),
		"BDMV/hello.txt":           []byte("hello from hdiutil\n"),
		"BDMV/STREAM/00001.m2ts":   bytes.Repeat([]byte("z"), 300000),
	}
	for name, data := range want {
		got, err := fs.ReadFile(img, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, data) {
			t.Errorf("%s: %d bytes, want %d", name, len(got), len(data))
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./udf/ -run TestHdiutilFixture`
Expected: FAIL (`open testdata/hdiutil-udf102.iso: no such file or directory`).

- [ ] **Step 4: Generate the fixture**

Run: `chmod +x scripts/gen-udf-fixtures.sh && scripts/gen-udf-fixtures.sh`
Expected: prints the size of `udf/testdata/hdiutil-udf102.iso` (a few hundred KB).

If `hdiutil` can't set the label with `-udf-volume-name`, check `hdiutil makehybrid -help` for the current flag, update both the script and the expected label, and re-run.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -race ./udf/ -run TestHdiutilFixture -v`
Expected: PASS. If it fails, the reader disagrees with a real-world writer. Debug the reader (for example, the anchor location, the descriptor sequence order, or whether this writer uses a VDP or embedded directory data) and don't change the fixture.

- [ ] **Step 6: Commit**

```bash
git add scripts/gen-udf-fixtures.sh udf/testdata/hdiutil-udf102.iso udf/hdiutil_fixture_test.go
git commit -m "Add hdiutil-generated UDF 1.02 fixture"
```

---

### Task 14: Milestone verification

**Files:**
- Modify: none, unless a check fails.

**Interfaces:**
- Consumes: everything.
- Produces: a verified branch ready for review.

- [ ] **Step 1: Run the full verification suite**

```bash
test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
go vet ./...
golangci-lint run
go test -race ./...
go test -tags integration ./...
CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

Expected: `gofmt -l` prints nothing; every other command exits 0. The integration run includes `TestMacOSMountsImages`.

- [ ] **Step 2: Check the dependency and import rules**

```bash
go list -deps ./bluray ./udf | grep -v -e '^[a-z]*$' -e '^[a-z]*/' -e '^github.com/chad3814/zenvik/bluray$' -e '^github.com/chad3814/zenvik/udf$' || true
grep -c . go.sum 2>/dev/null || echo "no go.sum (no third-party deps)"
```

Expected: the first command prints nothing (the public packages depend only on the standard library); the second prints `no go.sum`.

- [ ] **Step 3: Run each fuzz target briefly**

```bash
for f in FuzzParseIndex FuzzParseMovieObjects FuzzParsePlaylist FuzzParseClip; do
  go test ./bluray/ -run '^$' -fuzz "^$f\$" -fuzztime 30s || exit 1
done
go test ./udf/ -run '^$' -fuzz '^FuzzOpen$' -fuzztime 60s
```

Expected: no failures. Commit any new regression seeds along with their fixes.

- [ ] **Step 4: Review the branch**

Run: `git log --oneline docs/v1-design..HEAD && git status`
Expected: one commit per task (13 commits) and a clean tree. Do not push.
