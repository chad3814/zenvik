# zenvik repair-udf Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Linux, `zenvik rip` explains, before mounting, ISO images whose directory entries Linux's UDF driver rejects. A new `zenvik repair-udf` fixes those entries in place, with an exact backup and `--undo`.

**Architecture:**
- **Detection:** the read-only `udf` package gains `(*FS).PaddingCRCFixes`, which computes the 16-byte tag patches and never writes.
- **Writing:** a new `internal/udfrepair` package applies those patches, with a backup, a pre-write check, a post-write re-check and undo.
- **rip:** `Disc.mountRoot` calls `PaddingCRCFixes` on Linux before mounting, and reports real errors after mounting.
- **CLI:** the `repair-udf` command loops over images, calling `udfrepair` for each.

**Tech Stack:** Go 1.27, standard library only (`udf` must stay stdlib-only), cobra for the CLI.

**Spec:** `docs/superpowers/specs/2026-10-04-zenvik-repair-udf-design.md`

## Global Constraints

- No cgo. `udf` imports only the standard library, and stays read-only: it never opens a file for writing.
- Third-party dependencies stay `spf13/cobra` and `pelletier/go-toml/v2`.
- Return sentinel errors wrapped with `%w`.
- Never commit disc data; build every test image with `internal/testdisc` / `udfimage`.
- CLI tests must not touch real config or state. `cmd/zenvik` has a TestMain that sets `XDG_CONFIG_HOME` and `XDG_STATE_HOME`.
- A patch changes only the 16-byte FID tag:
  - descriptor CRC length = padded length − 16;
  - CRC over that length;
  - tag checksum.
- An entry is patched only when its CRC length equals exactly `unpadded length − 16` and the unpadded length differs from the padded one.
- Backup file: `<image>.udf-repair-backup`, JSON `{"version":1,"image_size":N,"patches":[{"off":N,"old":"<hex>","new":"<hex>"}]}`.
- Linux rip error, the sentinel `ErrNeedsUDFRepair`:
  - text: `zenvik: Linux can't read this image's directories: their entries' CRC lengths leave out padding, which Linux's UDF driver rejects as corrupt`;
  - wrapped with `: run `zenvik repair-udf <quoted path>` to fix them in place (it keeps a backup), or rip it on macOS`.
- CLI: `zenvik repair-udf [--dry-run | --undo] <image>...`.
  - Exit codes: 0 when all images succeed, 1 when any fails, 2 for usage errors.
  - Each image gets one line, `<image>: <outcome>`, where the outcome is one of:
    - `repaired N entries (backup: <backup path>)`
    - `nothing to repair`
    - `would repair N entries in: <dirs>` (the root shown as `(root)`)
    - `undone`
    - `failed: <error>`

## Review Focus

1. **Paths with spaces, parentheses or apostrophes in the suggested command.** Real images live at paths like `Atomic Blonde (2017)/…`, so the rip error's `zenvik repair-udf …` must be pasteable into a shell as is. Task 3 tests a path containing a space, parentheses and an apostrophe.
2. **Offsets past 4 GiB.** Real images are about 55 GiB, so `imageOffset` arithmetic must be int64 throughout, never `uint32 × 2048`. Task 2 has an internal test with block numbers whose byte offset exceeds 2³².
3. **The backup can't be written** (a read-only directory, a full disk). The image must stay untouched. Task 4 tests a read-only directory holding a writable image.
4. **Undo on an image that's already original,** or was changed by hand. Undo must refuse without writing. Task 4 tests undo after a manual revert.
6. **A UDF 2.50 image with a separate metadata mirror.** The repair patches only the main metadata file's directories; Linux reads the mirror only when the main copy is unreadable. The builder's mirror shares the main copy's blocks, so no test covers a separate mirror. The reviewer should confirm this limit is acceptable and stated (`PaddingCRCFixes`'s doc says "reachable from the root").
5. **A FID tag that crosses an extent boundary.** The builder can't produce one, so no test exercises the split into two patches. The coalescing code must be read with this in mind. The per-byte mapping in Task 2 is written so the split falls out naturally, and the final reviewer should check it by reading.

Two consequences of this:
- The builder can't embed directories, so `ErrEmbeddedDir` has no test either (Task 2 says so).
- The spec's "simulated through a test hook" check for an image changed mid-repair is covered in Task 4 through the `beforeWrite` hook.

---

### Task 1: Test images with unpadded FID CRC lengths

**Files:**
- Modify: `internal/testdisc/udfimage/udfimage.go` (the `Options` struct; `fid` and `tag`)
- Test: `internal/testdisc/udfimage/udfimage_test.go` (create it if absent, as `package udfimage`)

**Interfaces:**
- Produces:
  - `udfimage.Options.UnpaddedFIDCRC bool`. When true, every *named* FID's tag has CRC length `38+len(name)-16`, the CRC over that many bytes, and a matching checksum. The parent FID stays padded.
  - Internal helper `tagChecksum(d []byte) byte`.

- [ ] **Step 1: Write the failing test**

Append to `internal/testdisc/udfimage/udfimage_test.go`. If the file doesn't exist, create it with `package udfimage` and imports `encoding/binary` and `testing`.

```go
// fidTags finds every file identifier descriptor in img without knowing
// the layout: directory data starts on a sector boundary, so it scans each
// sector that starts with tag 257 and walks the FIDs packed in it. For each
// FID it returns its name length, used length, padded length and stored
// CRC length.
func fidTags(img []byte) [][4]int {
	var out [][4]int
	for s := 0; s+sector <= len(img); s += sector {
		d := img[s : s+sector]
		if binary.LittleEndian.Uint16(d) != 257 {
			continue
		}
		for len(d) >= 38 && binary.LittleEndian.Uint16(d) == 257 {
			lfi := int(d[19])
			used := 38 + lfi + int(binary.LittleEndian.Uint16(d[36:]))
			padded := (used + 3) &^ 3
			out = append(out, [4]int{lfi, used, padded, int(binary.LittleEndian.Uint16(d[10:]))})
			if padded > len(d) {
				break
			}
			d = d[padded:]
		}
	}
	return out
}

func TestUnpaddedFIDCRC(t *testing.T) {
	files := map[string]File{"BDMV/index.bdmv": {Data: []byte("INDX0200")}, "CERTIFICATE/id.bdmv": {Data: []byte("x")}}
	for _, rev := range []uint16{0x0102, 0x0250} {
		for _, unpadded := range []bool{false, true} {
			img, err := Build(files, Options{Revision: rev, UnpaddedFIDCRC: unpadded})
			if err != nil {
				t.Fatal(err)
			}
			fids := fidTags(img)
			// root: parent, BDMV, CERTIFICATE; BDMV: parent, index.bdmv; CERTIFICATE: parent, id.bdmv
			if len(fids) != 7 {
				t.Fatalf("rev %#x unpadded=%v: found %d FIDs, want 7", rev, unpadded, len(fids))
			}
			for _, f := range fids {
				lfi, used, padded, crcLen := f[0], f[1], f[2], f[3]
				want := padded - 16
				if unpadded && lfi > 0 {
					want = used - 16
				}
				if crcLen != want {
					t.Errorf("rev %#x unpadded=%v: FID (name length %d) CRC length %d, want %d", rev, unpadded, lfi, crcLen, want)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/testdisc/udfimage/ -run TestUnpaddedFIDCRC`
Expected: a compile failure, `unknown field UnpaddedFIDCRC in struct literal`.

- [ ] **Step 3: Implement**

In `Options`, after `MaxInlineADs`:

```go
	// UnpaddedFIDCRC writes each named file identifier descriptor's tag
	// CRC length (and CRC) over the descriptor without its padding, the
	// way some authoring tools do. Linux's UDF driver rejects such
	// directories; the parent entry keeps the padded length, as those
	// tools write it.
	UnpaddedFIDCRC bool
```

Replace `fid` and the checksum loop in `tag`:

```go
func (b *builder) fid(blk uint32, chars uint8, name []byte, icb uint32) []byte {
	d := make([]byte, fidLen(len(name)))
	le16(d[16:], 1) // file version number
	d[18] = chars
	d[19] = uint8(len(name))
	copy(d[20:], longAD(0, sector, icb, b.feRef))
	copy(d[38:], name)
	d = b.tag(257, blk, d)
	if used := 38 + len(name); b.opt.UnpaddedFIDCRC && len(name) > 0 && used < len(d) {
		le16(d[10:], uint16(used-16))
		le16(d[8:], crc16(d[16:used]))
		d[4] = tagChecksum(d)
	}
	return d
}
```

```go
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
	d[4] = tagChecksum(d)
	return d
}

// tagChecksum is the descriptor tag checksum: the sum of the tag's bytes
// other than the checksum itself.
func tagChecksum(d []byte) byte {
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	return sum
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/testdisc/... ./udf/...`
Expected: PASS. Existing images are byte-identical when the option is off.

- [ ] **Step 5: Commit**

```bash
git add internal/testdisc/udfimage/
git commit -m "udfimage: optionally write named FIDs' tag CRC length without padding, like some authoring tools"
```

---

### Task 2: `udf.PaddingCRCFixes`

**Files:**
- Create: `udf/patch.go`
- Test: `udf/patch_test.go` (`package udf_test`), `udf/patch_internal_test.go` (`package udf`)

**Interfaces:**
- Consumes: `udfimage.Options.UnpaddedFIDCRC` (Task 1).
- Produces:
  - `type Patch struct { Off int64; Old, New []byte; Dir string }`
  - `var ErrEmbeddedDir error`
  - `func (f *FS) PaddingCRCFixes() ([]Patch, error)`
  - Internal: `func (f *FS) imageOffset(ref uint16, block uint32, off int64) (int64, error)` and `func (f *FS) entryImageOffset(e *entry, off int64) (int64, error)`.

- [ ] **Step 1: Write the failing tests**

`udf/patch_test.go`:

```go
package udf_test

import (
	"bytes"
	"io"
	"io/fs"
	"slices"
	"testing"

	"github.com/chad3814/zenvik/udf"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

// patchFiles: every directory name and file name below has a FID whose
// unpadded length isn't a multiple of 4, except "X" (38 + 2 = 40).
var patchFiles = map[string]udfimage.File{
	"BDMV/index.bdmv":          {Data: []byte("INDX0200")},
	"BDMV/PLAYLIST/00001.mpls": {Data: []byte("MPLS0200")},
	"CERTIFICATE/id.bdmv":      {Data: []byte("id")},
	"X":                        {Data: []byte("x")},
}

func TestPaddingCRCFixes(t *testing.T) {
	for _, rev := range []uint16{0x0102, 0x0250} {
		clean := buildImage(t, patchFiles, udfimage.Options{Revision: rev})
		if fixes, err := openImage(t, clean).PaddingCRCFixes(); err != nil || len(fixes) != 0 {
			t.Fatalf("rev %#x clean: %d fixes, err %v; want none", rev, len(fixes), err)
		}

		bad := buildImage(t, patchFiles, udfimage.Options{Revision: rev, UnpaddedFIDCRC: true})
		orig := slices.Clone(bad)
		fixes, err := openImage(t, bad).PaddingCRCFixes()
		if err != nil {
			t.Fatalf("rev %#x: %v", rev, err)
		}
		// root: BDMV, CERTIFICATE, X(no fix); BDMV: PLAYLIST, index.bdmv; PLAYLIST: 00001.mpls; CERTIFICATE: id.bdmv
		var dirs []string
		for _, p := range fixes {
			dirs = append(dirs, p.Dir)
			if len(p.Old) != 16 || len(p.New) != 16 {
				t.Errorf("rev %#x: patch at %d is %d/%d bytes, want 16 (no tag crosses an extent here)", rev, p.Off, len(p.Old), len(p.New))
			}
			if !bytes.Equal(bad[p.Off:p.Off+int64(len(p.Old))], p.Old) {
				t.Errorf("rev %#x: patch Old at %d doesn't match the image", rev, p.Off)
			}
		}
		slices.Sort(dirs)
		if want := []string{".", ".", "BDMV", "BDMV", "BDMV/PLAYLIST", "CERTIFICATE"}; !slices.Equal(dirs, want) {
			t.Errorf("rev %#x: patch dirs %v, want %v", rev, dirs, want)
		}

		for _, p := range fixes {
			copy(bad[p.Off:], p.New)
		}
		f := openImage(t, bad)
		if again, err := f.PaddingCRCFixes(); err != nil || len(again) != 0 {
			t.Errorf("rev %#x after patching: %d fixes, err %v; want none", rev, len(again), err)
		}
		for name, want := range patchFiles {
			got, err := fs.ReadFile(f, name)
			if err != nil || !bytes.Equal(got, want.Data) {
				t.Errorf("rev %#x after patching: %s = %q, %v", rev, name, got, err)
			}
		}
		patched := make([]bool, len(bad))
		for _, p := range fixes {
			for i := range p.New {
				patched[p.Off+int64(i)] = true
			}
		}
		for i := range bad {
			if !patched[i] && bad[i] != orig[i] {
				t.Fatalf("rev %#x: byte %d changed outside the patches", rev, i)
			}
		}
	}
}

func TestFlawedImageStillReads(t *testing.T) {
	bad := buildImage(t, patchFiles, udfimage.Options{Revision: 0x0250, UnpaddedFIDCRC: true})
	f := openImage(t, bad)
	got, err := fs.ReadFile(f, "BDMV/PLAYLIST/00001.mpls")
	if err != nil || string(got) != "MPLS0200" {
		t.Fatalf("read flawed image: %q, %v", got, err)
	}
	var _ io.ReaderAt = bytes.NewReader(nil) // keep io imported for helpers' sake
}
```

If the existing `buildImage`/`openImage` helpers in `udf/volume_test.go` take different arguments than `(t, files, opt)` and `(t, img)`, use their real signatures. As of this plan they are `buildImage(t testing.TB, files map[string]udfimage.File, opt udfimage.Options) []byte` and `openImage(t testing.TB, img []byte) *udf.FS`. Drop the `io` line if `io` isn't otherwise needed; it's only there to satisfy the import list.

`udf/patch_internal_test.go`:

```go
package udf

import "testing"

func TestImageOffsetPast4GiB(t *testing.T) {
	f := &FS{size: 1 << 40, parts: []partition{{number: 0, start: 3_000_000, length: 10_000_000}}}
	got, err := f.imageOffset(0, 2_500_000, 5)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(5_500_000)*sectorSize + 5; got != want {
		t.Fatalf("imageOffset = %d, want %d", got, want)
	}
	if _, err := f.imageOffset(0, 10_000_000, 0); err == nil {
		t.Fatal("offset past the partition: want an error")
	}
	if _, err := f.imageOffset(1, 0, 0); err == nil {
		t.Fatal("unknown partition reference: want an error")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./udf/ -run 'PaddingCRCFixes|FlawedImage|ImageOffset'`
Expected: compile errors, `PaddingCRCFixes undefined` and `f.imageOffset undefined`.

- [ ] **Step 3: Implement `udf/patch.go`**

```go
package udf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// A Patch replaces len(Old) bytes at byte offset Off of the image with New.
type Patch struct {
	Off      int64
	Old, New []byte
	Dir      string // the directory holding the entry, slash-separated ("." for the root)
}

// ErrEmbeddedDir: a directory stored inside its file entry can't be patched
// without rewriting that entry, which PaddingCRCFixes doesn't do.
var ErrEmbeddedDir = errors.New("udf: directory is embedded in its file entry")

// PaddingCRCFixes returns one Patch for each file identifier descriptor whose
// descriptor CRC length leaves out its padding (ECMA-167 4/14.4: the padding
// is part of the descriptor). Linux's UDF driver rejects such directories.
// Each patch rewrites only the 16-byte tag: the CRC length set to padded
// length − 16, the CRC recomputed over that length, and the tag checksum
// recomputed. A tag that crosses an extent boundary yields one patch per
// contiguous run of image bytes. It reads every directory reachable from
// the root, including deleted entries' tags, and never writes. A nil slice
// means nothing needs fixing.
func (f *FS) PaddingCRCFixes() ([]Patch, error) {
	type dir struct {
		e    *entry
		path string
	}
	var out []Patch
	seen := map[entryAddr]bool{f.root.addr: true}
	queue := []dir{{f.root, "."}}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if d.e.size > maxDirSize {
			return nil, fmt.Errorf("%w: directory %s of %d bytes", ErrCorrupt, d.path, d.e.size)
		}
		data := make([]byte, d.e.size)
		if _, err := d.e.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for off := 0; off < len(data) && !allZero(data[off:]); {
			rest := data[off:]
			if len(rest) < 38 {
				return nil, fmt.Errorf("%w: truncated file identifier in %s", ErrCorrupt, d.path)
			}
			lfi, liu := int(rest[19]), int(le16(rest[36:]))
			used := 38 + liu + lfi
			if used > len(rest) {
				return nil, fmt.Errorf("%w: file identifier overruns directory %s", ErrCorrupt, d.path)
			}
			padded := (used + 3) &^ 3
			total := min(padded, len(rest))
			fid := rest[:total]
			if _, err := expectTag(fid, anyLocation, tagFID); err != nil {
				return nil, fmt.Errorf("directory %s: %w", d.path, err)
			}
			if total == padded && used != padded && int(le16(fid[10:])) == used-16 {
				if d.e.inline {
					return nil, fmt.Errorf("%w: %s", ErrEmbeddedDir, d.path)
				}
				ps, err := f.tagPatches(d.e, int64(off), fid, d.path)
				if err != nil {
					return nil, err
				}
				out = append(out, ps...)
			}
			chars := fid[18]
			if chars&fidDirectory != 0 && chars&(fidParent|fidDeleted) == 0 {
				name, err := decodeDchars(fid[38+liu : used])
				if err != nil {
					return nil, fmt.Errorf("directory %s: %w", d.path, err)
				}
				icb := parseLongAD(fid[20:36])
				child, err := f.readEntry(icb.ref, icb.block)
				if err != nil {
					return nil, err
				}
				if child.fileType == fileTypeDirectory && !seen[child.addr] {
					seen[child.addr] = true
					p := name
					if d.path != "." {
						p = d.path + "/" + name
					}
					queue = append(queue, dir{child, p})
				}
			}
			off += total
		}
	}
	return out, nil
}

// tagPatches returns the patches that correct the tag of fid, which starts
// dirOff bytes into directory e's data. fid is the whole padded descriptor.
func (f *FS) tagPatches(e *entry, dirOff int64, fid []byte, path string) ([]Patch, error) {
	fixed := make([]byte, 16)
	copy(fixed, fid[:16])
	binary.LittleEndian.PutUint16(fixed[10:], uint16(len(fid)-16))
	binary.LittleEndian.PutUint16(fixed[8:], crc16(fid[16:]))
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += fixed[i]
		}
	}
	fixed[4] = sum

	var out []Patch
	for i := 0; i < 16; i++ {
		img, err := f.entryImageOffset(e, dirOff+int64(i))
		if err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].Off+int64(len(out[n-1].Old)) == img {
			out[n-1].Old = append(out[n-1].Old, fid[i])
			out[n-1].New = append(out[n-1].New, fixed[i])
			continue
		}
		out = append(out, Patch{Off: img, Old: []byte{fid[i]}, New: []byte{fixed[i]}, Dir: path})
	}
	return out, nil
}

// entryImageOffset maps byte off of entry e's data to a byte offset in the
// image.
func (f *FS) entryImageOffset(e *entry, off int64) (int64, error) {
	var base int64
	for _, x := range e.extents {
		l := int64(x.length)
		if off < base+l {
			if x.typ != 0 {
				return 0, fmt.Errorf("%w: directory data in an unrecorded extent", ErrCorrupt)
			}
			return f.imageOffset(x.ref, x.block, off-base)
		}
		base += l
	}
	return 0, fmt.Errorf("%w: offset %d past the entry's extents", ErrCorrupt, off)
}

// imageOffset maps byte off of logical block block in partition reference
// ref to a byte offset in the image, through the metadata file for a
// metadata partition. All arithmetic is int64: images exceed 4 GiB.
func (f *FS) imageOffset(ref uint16, block uint32, off int64) (int64, error) {
	if int(ref) >= len(f.parts) {
		return 0, fmt.Errorf("%w: partition reference %d", ErrCorrupt, ref)
	}
	part := &f.parts[ref]
	pos := int64(block)*sectorSize + off
	if part.isMeta {
		if part.meta == nil {
			return 0, fmt.Errorf("%w: metadata partition used before it is loaded", ErrCorrupt)
		}
		return f.entryImageOffset(part.meta, pos)
	}
	if pos < 0 || pos >= int64(part.length)*sectorSize {
		return 0, fmt.Errorf("%w: offset past the end of partition %d", ErrCorrupt, ref)
	}
	return int64(part.start)*sectorSize + pos, nil
}
```

`ErrEmbeddedDir` has no test, because `udfimage` never embeds directories (its `Embed` applies to files only). Record that in the ledger.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./udf/... ./internal/testdisc/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add udf/patch.go udf/patch_test.go udf/patch_internal_test.go
git commit -m "udf: PaddingCRCFixes finds directory entries whose tag CRC length leaves out padding, and the tag patches that fix them"
```

---

### Task 3: Linux check in rip, and real post-mount errors

**Files:**
- Modify: `internal/source/source.go` (add `Image *udf.Image` and set it for ISO sources)
- Modify: `errors.go` (add `ErrNeedsUDFRepair`)
- Modify: `rip.go` (`mountAttach`/`mountGOOS` variables, the Linux check, `discMarkerErr`, `shellArg`)
- Modify: `rip_flat_internal_test.go:71` (`hasDiscMarker` → `discMarkerErr`)
- Test: `rip_udf_internal_test.go` (`package zenvik`)

**Interfaces:**
- Consumes: `(*udf.FS).PaddingCRCFixes` (Task 2) and `udfimage.Options.UnpaddedFIDCRC` (Task 1).
- Produces:
  - `zenvik.ErrNeedsUDFRepair`
  - `source.Source.Image *udf.Image`
  - package variables in `zenvik`: `mountAttach = mount.Attach` and `mountGOOS = runtime.GOOS`
  - `func discMarkerErr(root string, f Format) error`
  - `func shellArg(s string) string`

- [ ] **Step 1: Write the failing test**

`rip_udf_internal_test.go`:

```go
package zenvik

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func writeISO(t *testing.T, dir string, opt udfimage.Options) string {
	t.Helper()
	img, err := testdisc.SampleMovie().ISO(opt)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "disc.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeMount(t *testing.T, goos string) *int {
	t.Helper()
	oldA, oldG := mountAttach, mountGOOS
	t.Cleanup(func() { mountAttach, mountGOOS = oldA, oldG })
	calls := 0
	mountAttach = func(context.Context, string) (*mount.Mount, error) {
		calls++
		return nil, errors.New("fake mount: not attached")
	}
	mountGOOS = goos
	return &calls
}

func TestLinuxRefusesUnpaddedFIDCRCBeforeMounting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Atomic Blonde (2017) - Director's Cut")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := writeISO(t, dir, udfimage.Options{Revision: 0x0250, Label: "X", UnpaddedFIDCRC: true})
	ctx := context.Background()
	d, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	calls := fakeMount(t, "linux")
	_, _, err = d.mountRoot(ctx, func(Phase, float64) {})
	if !errors.Is(err, ErrNeedsUDFRepair) {
		t.Fatalf("linux: err = %v, want ErrNeedsUDFRepair", err)
	}
	if *calls != 0 {
		t.Fatalf("linux: mount attempted %d times, want 0", *calls)
	}
	if want := "zenvik repair-udf " + shellArg(p); !strings.Contains(err.Error(), want) {
		t.Errorf("linux: error %q doesn't contain %q", err, want)
	}

	calls = fakeMount(t, "darwin")
	_, _, err = d.mountRoot(ctx, func(Phase, float64) {})
	if errors.Is(err, ErrNeedsUDFRepair) || *calls != 1 {
		t.Fatalf("darwin: err = %v, mounts = %d; want a mount attempt", err, *calls)
	}
}

func TestLinuxMountsCleanImages(t *testing.T) {
	p := writeISO(t, t.TempDir(), udfimage.Options{Revision: 0x0102, Label: "X"})
	ctx := context.Background()
	d, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	calls := fakeMount(t, "linux")
	if _, _, err := d.mountRoot(ctx, func(Phase, float64) {}); errors.Is(err, ErrNeedsUDFRepair) || *calls != 1 {
		t.Fatalf("clean image on linux: err = %v, mounts = %d; want a mount attempt", err, *calls)
	}
}

func TestShellArg(t *testing.T) {
	for in, want := range map[string]string{
		"plain.iso":                 "plain.iso",
		"Atomic Blonde (2017)/x.iso": `'Atomic Blonde (2017)/x.iso'`,
		"Director's Cut.iso":        `'Director'\''s Cut.iso'`,
	} {
		if got := shellArg(in); got != want {
			t.Errorf("shellArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDiscMarkerErr(t *testing.T) {
	root := t.TempDir()
	if err := discMarkerErr(root, Bluray); err == nil || !strings.Contains(err.Error(), "found no BDMV/index.bdmv") {
		t.Errorf("missing marker: err = %v, want found no BDMV/index.bdmv", err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission errors need a non-root Unix user")
	}
	bdmv := filepath.Join(root, "BDMV")
	if err := os.Mkdir(bdmv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bdmv, "index.bdmv"), []byte("INDX0200"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := discMarkerErr(root, Bluray); err != nil {
		t.Errorf("present marker: err = %v, want nil", err)
	}
	if err := os.Chmod(bdmv, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(bdmv, 0o755) })
	err := discMarkerErr(root, Bluray)
	if err == nil || !strings.Contains(err.Error(), "can't read BDMV/index.bdmv") || !errors.Is(err, os.ErrPermission) {
		t.Errorf("unreadable marker: err = %v, want can't read … permission denied", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test . -run 'LinuxRefuses|LinuxMounts|ShellArg|DiscMarkerErr'`
Expected: compile errors, undefined `mountAttach`, `mountGOOS`, `ErrNeedsUDFRepair`, `shellArg` and `discMarkerErr`.

- [ ] **Step 3: Implement**

`internal/source/source.go`: add the field after `FS`:

```go
	Image   *udf.Image        // the opened UDF image (ISO only)
```

In both ISO returns (around lines 159 and 166), add `Image: img,` to the struct literal.

`errors.go`: add to the `var` block:

```go
	// ErrNeedsUDFRepair: on Linux, the image's directory entries have tag CRC
	// lengths that leave out padding, which Linux's UDF driver rejects;
	// `zenvik repair-udf` fixes them.
	ErrNeedsUDFRepair = errors.New("zenvik: Linux can't read this image's directories: their entries' CRC lengths leave out padding, which Linux's UDF driver rejects as corrupt")
```

`rip.go`:
- add the package variables near the top;
- change `mountRoot` as shown;
- replace `hasDiscMarker` with `discMarkerErr`;
- add `shellArg`.

Add `runtime`, `io/fs` and `strings` to the imports if they're missing.

```go
// mountAttach and mountGOOS are variables so tests can fake mounting and
// the host OS.
var (
	mountAttach = mount.Attach
	mountGOOS   = runtime.GOOS
)
```

In `mountRoot`, after `report(PhaseMounting, 0)` and before the attach:

```go
	if mountGOOS == "linux" && d.src.Image != nil {
		// Advisory: if the check itself fails, the mount below reports
		// any real problem.
		if fixes, err := d.src.Image.PaddingCRCFixes(); err == nil && len(fixes) > 0 {
			return "", nil, fmt.Errorf("%w: run `zenvik repair-udf %s` to fix them in place (it keeps a backup), or rip it on macOS", ErrNeedsUDFRepair, shellArg(d.src.Path))
		}
	}
	m, err := mountAttach(ctx, d.src.Path)
```

Replace the marker check:

```go
	if err := discMarkerErr(m.Dir, d.Format); err != nil {
		err = fmt.Errorf("zenvik: mounted %s at %s but %w", d.src.Path, m.Dir, err)
		return "", nil, errors.Join(err, release())
	}
```

Replace `hasDiscMarker`:

```go
// discMarkerErr reports whether root holds format f's marker file: nil if
// it does, "found no …" if it's missing, and "can't read …: <err>" for any
// other error (a permission or I/O error, or a kernel's corruption error).
// DVD names are matched case-insensitively.
func discMarkerErr(root string, f Format) error {
	if f == DVD {
		if _, err := os.ReadDir(root); err != nil {
			return fmt.Errorf("can't read %s: %w", discMarker(f), err)
		}
		fsys := os.DirFS(root)
		dir := source.FindName(fsys, ".", "VIDEO_TS", true)
		if dir == "" || source.FindName(fsys, dir, "VIDEO_TS.IFO", false) == "" {
			return fmt.Errorf("found no %s", discMarker(f))
		}
		return nil
	}
	st, err := os.Stat(filepath.Join(root, "BDMV", "index.bdmv"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("found no %s", discMarker(f))
	case err != nil:
		return fmt.Errorf("can't read %s: %w", discMarker(f), err)
	case !st.Mode().IsRegular():
		return fmt.Errorf("found no %s", discMarker(f))
	}
	return nil
}

// shellArg quotes s for a POSIX shell when it holds anything but letters,
// digits and ./_-+:@%,= so a suggested command can be pasted as is.
func shellArg(s string) string {
	safe := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("./_-+:@%,=", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

Check that `discMarker(Bluray)` returns `"BDMV/index.bdmv"` (it's used in the existing message). If it returns another form, adjust the test's expected strings to match it.

`rip_flat_internal_test.go:71`: change `if !hasDiscMarker(root, Bluray) {` to `if err := discMarkerErr(root, Bluray); err != nil {`, and report `err` in that `t.Fatal`/`t.Error` message.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./... && go vet ./... && CGO_ENABLED=0 go build ./...`
Expected: PASS. On macOS, `TestDiscMarkerErr` runs the permission case, because the test user isn't root.

- [ ] **Step 5: Commit**

```bash
git add internal/source/source.go errors.go rip.go rip_flat_internal_test.go rip_udf_internal_test.go
git commit -m "rip: on Linux, refuse before mounting an image whose directories Linux rejects, naming zenvik repair-udf; report real errors from the post-mount check"
```

---

### Task 4: `internal/udfrepair`

**Files:**
- Create: `internal/udfrepair/udfrepair.go`
- Test: `internal/udfrepair/udfrepair_test.go` (`package udfrepair`)

**Interfaces:**
- Consumes: `udf.OpenImage`, `(*udf.FS).PaddingCRCFixes` and `udf.Patch` (Task 2).
- Produces:
  - `const BackupSuffix = ".udf-repair-backup"`
  - `var ErrBackupExists, ErrChanged, ErrNoBackup error`
  - `type Plan struct { Patches []udf.Patch; Dirs []string }`
  - `func Check(image string) (Plan, error)`
  - `func Repair(image string) (int, error)`: the number of entries repaired, 0 meaning nothing to repair. The count is distinct FIDs, not patches.
  - `func Undo(image string) error`
  - test hooks: `var beforeWrite = func() {}` and `var recheck = recheckImage`

- [ ] **Step 1: Write the failing tests**

`internal/udfrepair/udfrepair_test.go`:

```go
package udfrepair

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func flawed(t *testing.T, dir string, unpadded bool) string {
	t.Helper()
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "X", UnpaddedFIDCRC: unpadded})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "disc.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCheck(t *testing.T) {
	clean, err := Check(flawed(t, t.TempDir(), false))
	if err != nil || len(clean.Patches) != 0 {
		t.Fatalf("clean: %d patches, %v", len(clean.Patches), err)
	}
	plan, err := Check(flawed(t, t.TempDir(), true))
	if err != nil || len(plan.Patches) == 0 {
		t.Fatalf("flawed: %d patches, %v", len(plan.Patches), err)
	}
	if len(plan.Dirs) == 0 || plan.Dirs[0] != "." {
		t.Errorf("Dirs = %v, want the root first", plan.Dirs)
	}
}

func TestRepairAndUndo(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	plan, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Repair(p)
	if err != nil || n == 0 {
		t.Fatalf("Repair = %d, %v", n, err)
	}
	var b backup
	if err := json.Unmarshal(read(t, p+BackupSuffix), &b); err != nil || b.Version != 1 || b.ImageSize != int64(len(orig)) || len(b.Patches) != len(plan.Patches) {
		t.Fatalf("backup = %+v, %v", b, err)
	}
	fixed := read(t, p)
	inPatch := map[int64]bool{}
	for _, pt := range plan.Patches {
		for i := range pt.New {
			inPatch[pt.Off+int64(i)] = true
		}
	}
	for i := range fixed {
		if fixed[i] != orig[i] && !inPatch[int64(i)] {
			t.Fatalf("byte %d changed outside the patches", i)
		}
	}
	if again, err := Check(p); err != nil || len(again.Patches) != 0 {
		t.Fatalf("after repair: %d patches, %v", len(again.Patches), err)
	}
	if err := os.Remove(p + BackupSuffix); err != nil { // so the next Repair isn't refused for the backup
		t.Fatal(err)
	}
	if n, err := Repair(p); err != nil || n != 0 {
		t.Fatalf("second Repair = %d, %v; want nothing to repair", n, err)
	}
	if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nothing-to-repair wrote a backup: %v", err)
	}
}

func TestUndoRestoresTheOriginal(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	want := sha256.Sum256(read(t, p))
	if _, err := Repair(p); err != nil {
		t.Fatal(err)
	}
	if err := Undo(p); err != nil {
		t.Fatal(err)
	}
	if got := sha256.Sum256(read(t, p)); got != want {
		t.Fatal("Undo didn't restore the original bytes")
	}
	if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Undo left the backup: %v", err)
	}
	if err := Undo(p); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Undo without a backup: %v, want ErrNoBackup", err)
	}
}

func TestUndoRefusesAnImageThatIsNoLongerRepaired(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	if _, err := Repair(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, orig, 0o644); err != nil { // reverted by hand
		t.Fatal(err)
	}
	if err := Undo(p); !errors.Is(err, ErrChanged) {
		t.Fatalf("Undo of a hand-reverted image: %v, want ErrChanged", err)
	}
	if !bytes.Equal(read(t, p), orig) {
		t.Fatal("Undo wrote to the image")
	}
}

func TestRepairRefusesAnExistingBackup(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	if err := os.WriteFile(p+BackupSuffix, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Repair(p); !errors.Is(err, ErrBackupExists) {
		t.Fatalf("err = %v, want ErrBackupExists", err)
	}
	if !bytes.Equal(read(t, p), orig) || string(read(t, p+BackupSuffix)) != "{}" {
		t.Fatal("refused repair changed the image or the backup")
	}
}

func TestRepairStopsIfTheImageChanged(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	plan, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	old := beforeWrite
	t.Cleanup(func() { beforeWrite = old })
	var changed []byte
	beforeWrite = func() {
		b := read(t, p)
		b[plan.Patches[0].Off] ^= 0xFF
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		changed = b
	}
	if _, err := Repair(p); !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), "safe to delete") {
		t.Fatalf("err = %v, want ErrChanged mentioning the backup is safe to delete", err)
	}
	if !bytes.Equal(read(t, p), changed) {
		t.Fatal("repair wrote to an image that changed")
	}
}

func TestRepairRestoresWhenTheRecheckFails(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	old := recheck
	t.Cleanup(func() { recheck = old })
	recheck = func(string) error { return errors.New("injected re-check failure") }
	if _, err := Repair(p); err == nil || !strings.Contains(err.Error(), "injected re-check failure") {
		t.Fatalf("err = %v, want the re-check failure", err)
	}
	if !bytes.Equal(read(t, p), orig) {
		t.Fatal("failed re-check didn't restore the original bytes")
	}
	if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed re-check left the backup: %v", err)
	}
}

func TestRepairReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	t.Run("image", func(t *testing.T) {
		p := flawed(t, t.TempDir(), true)
		if err := os.Chmod(p, 0o444); err != nil {
			t.Fatal(err)
		}
		if _, err := Repair(p); err == nil || !strings.Contains(err.Error(), "can't write") {
			t.Fatalf("err = %v, want can't write", err)
		}
		if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only image left a backup: %v", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		p := flawed(t, dir, true)
		orig := read(t, p)
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		if _, err := Repair(p); err == nil {
			t.Fatal("repair with an unwritable backup directory: want an error")
		}
		if !bytes.Equal(read(t, p), orig) {
			t.Fatal("repair changed the image although the backup couldn't be written")
		}
	})
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/udfrepair/`
Expected: compile errors, undefined `Check`, `Repair`, `Undo`, `backup`, `BackupSuffix` and so on.

- [ ] **Step 3: Implement `internal/udfrepair/udfrepair.go`**

```go
// Package udfrepair fixes, in place, UDF disc images whose directory
// entries' tag CRC lengths leave out padding (see udf.PaddingCRCFixes),
// keeping a backup of every byte it changes so the repair can be undone.
package udfrepair

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/chad3814/zenvik/udf"
)

// BackupSuffix is appended to an image's path to name its backup.
const BackupSuffix = ".udf-repair-backup"

var (
	// ErrBackupExists: a backup from an earlier repair is in the way.
	ErrBackupExists = errors.New("udfrepair: a backup from an earlier repair exists")
	// ErrChanged: the image's bytes aren't what the repair or undo expects.
	ErrChanged = errors.New("udfrepair: the image changed since it was checked")
	// ErrNoBackup: there is no backup to undo.
	ErrNoBackup = errors.New("udfrepair: no backup to undo")
)

// Plan is what a repair would change.
type Plan struct {
	Patches []udf.Patch
	Dirs    []string // distinct directories with patches, in walk order
}

// Entries is the number of directory entries the plan fixes (a tag that
// crosses an extent boundary is one entry but two patches).
func (p Plan) Entries() int {
	n := 0
	for i, pt := range p.Patches {
		if i == 0 || p.Patches[i-1].Off+int64(len(p.Patches[i-1].New)) != pt.Off {
			n++
		}
	}
	return n
}

// Check opens image read-only and returns what a repair would change.
func Check(image string) (Plan, error) {
	img, err := udf.OpenImage(image)
	if err != nil {
		return Plan{}, err
	}
	defer img.Close()
	ps, err := img.PaddingCRCFixes()
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Patches: ps}
	for _, p := range ps {
		if n := len(plan.Dirs); n == 0 || plan.Dirs[n-1] != p.Dir {
			plan.Dirs = append(plan.Dirs, p.Dir)
		}
	}
	return plan, nil
}

type backup struct {
	Version   int           `json:"version"`
	ImageSize int64         `json:"image_size"`
	Patches   []backupPatch `json:"patches"`
}

type backupPatch struct {
	Off int64  `json:"off"`
	Old string `json:"old"`
	New string `json:"new"`
}

// Test hooks.
var (
	beforeWrite = func() {}
	recheck     = recheckImage
)

// Repair fixes image in place and returns the number of entries fixed (0
// when it needs nothing). It writes image+BackupSuffix first.
func Repair(image string) (int, error) {
	plan, err := Check(image)
	if err != nil {
		return 0, err
	}
	if len(plan.Patches) == 0 {
		return 0, nil
	}
	bpath := image + BackupSuffix
	if _, err := os.Lstat(bpath); err == nil {
		return 0, fmt.Errorf("%w: %s", ErrBackupExists, bpath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	f, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("can't write %s: %w", image, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if err := writeBackup(bpath, st.Size(), plan.Patches); err != nil {
		return 0, err
	}
	beforeWrite()
	if err := expect(f, plan.Patches, false); err != nil {
		return 0, fmt.Errorf("%w; nothing was written, and the backup %s is safe to delete", err, bpath)
	}
	if err := write(f, plan.Patches, false); err != nil {
		_ = write(f, plan.Patches, true)
		_ = f.Sync()
		return 0, fmt.Errorf("writing %s: %w (the original bytes were written back; %s holds them too)", image, err, bpath)
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	if err := recheck(image); err != nil {
		werr := write(f, plan.Patches, true)
		if werr == nil {
			werr = f.Sync()
		}
		if werr != nil {
			return 0, fmt.Errorf("the repaired image didn't check out (%w), and restoring it failed (%v); %s holds the original bytes", err, werr, bpath)
		}
		_ = os.Remove(bpath)
		return 0, fmt.Errorf("the repaired image didn't check out, so it was restored: %w", err)
	}
	return plan.Entries(), nil
}

// Undo writes back the bytes a Repair replaced and removes the backup.
func Undo(image string) error {
	bpath := image + BackupSuffix
	raw, err := os.ReadFile(bpath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNoBackup, bpath)
	}
	if err != nil {
		return err
	}
	var b backup
	if err := json.Unmarshal(raw, &b); err != nil || b.Version != 1 {
		return fmt.Errorf("udfrepair: %s isn't a version 1 backup", bpath)
	}
	ps := make([]udf.Patch, len(b.Patches))
	for i, bp := range b.Patches {
		o, err1 := hex.DecodeString(bp.Old)
		n, err2 := hex.DecodeString(bp.New)
		if err1 != nil || err2 != nil || len(o) != len(n) {
			return fmt.Errorf("udfrepair: %s has a malformed patch at offset %d", bpath, bp.Off)
		}
		ps[i] = udf.Patch{Off: bp.Off, Old: o, New: n}
	}
	f, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("can't write %s: %w", image, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() != b.ImageSize {
		return fmt.Errorf("%w: it is %d bytes, the backup is for %d", ErrChanged, st.Size(), b.ImageSize)
	}
	if err := expect(f, ps, true); err != nil {
		return fmt.Errorf("%w; nothing was written", err)
	}
	if err := write(f, ps, true); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return os.Remove(bpath)
}

func writeBackup(path string, size int64, ps []udf.Patch) error {
	b := backup{Version: 1, ImageSize: size}
	for _, p := range ps {
		b.Patches = append(b.Patches, backupPatch{Off: p.Off, Old: hex.EncodeToString(p.Old), New: hex.EncodeToString(p.New)})
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("writing the backup: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	return nil
}

// expect checks every patch's range holds Old (or New, when repaired).
func expect(f *os.File, ps []udf.Patch, repaired bool) error {
	for _, p := range ps {
		want := p.Old
		if repaired {
			want = p.New
		}
		got := make([]byte, len(want))
		if _, err := f.ReadAt(got, p.Off); err != nil {
			return fmt.Errorf("%w: reading offset %d: %v", ErrChanged, p.Off, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%w: offset %d", ErrChanged, p.Off)
		}
	}
	return nil
}

// write writes every patch's New (or Old, when undoing).
func write(f *os.File, ps []udf.Patch, undo bool) error {
	for _, p := range ps {
		b := p.New
		if undo {
			b = p.Old
		}
		if _, err := f.WriteAt(b, p.Off); err != nil {
			return err
		}
	}
	return nil
}

func recheckImage(image string) error {
	plan, err := Check(image)
	if err != nil {
		return err
	}
	if len(plan.Patches) > 0 {
		return fmt.Errorf("it still needs %d fixes", len(plan.Patches))
	}
	return nil
}
```

- [ ] **Step 4: Run the tests and lint**

Run: `go test -race ./internal/udfrepair/ && go vet ./internal/udfrepair/ && golangci-lint run ./internal/udfrepair/`
Expected: PASS and no lint findings. If errcheck flags the cleanup `os.Remove`/`f.Close` calls, prefix them with `_ =`.

- [ ] **Step 5: Commit**

```bash
git add internal/udfrepair/
git commit -m "udfrepair: repair padding-less FID tags in place with a backup, a pre-write check and a re-check, and undo them"
```

---

### Task 5: `zenvik repair-udf` command and docs

**Files:**
- Create: `cmd/zenvik/repair_udf.go`
- Modify: `cmd/zenvik/main.go` (`root.AddCommand(newRepairUDFCmd())`)
- Test: `cmd/zenvik/repair_udf_test.go`
- Modify: `README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: `udfrepair.Check`, `Repair`, `Undo`, `Plan.Entries` and `BackupSuffix` (Task 4).
- Produces: the `repair-udf` subcommand.

- [ ] **Step 1: Write the failing test**

`cmd/zenvik/repair_udf_test.go`:

```go
package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/internal/udfrepair"
)

func writeImage(t *testing.T, dir, name string, unpadded bool) string {
	t.Helper()
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0102, Label: "X", UnpaddedFIDCRC: unpadded})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func sum(t *testing.T, p string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

func TestRepairUDFDryRun(t *testing.T) {
	dir := t.TempDir()
	p := writeImage(t, dir, "bad.iso", true)
	before := sum(t, p)
	code, out, errOut := runCLI("repair-udf", "--dry-run", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "would repair") || !strings.Contains(out, "(root)") {
		t.Fatalf("code %d, out %q, err %q", code, out, errOut)
	}
	if sum(t, p) != before {
		t.Fatal("--dry-run changed the image")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("--dry-run created files: %v", ents)
	}
}

func TestRepairUDFRepairThenUndo(t *testing.T) {
	p := writeImage(t, t.TempDir(), "bad.iso", true)
	before := sum(t, p)
	code, out, errOut := runCLI("repair-udf", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "repaired") || !strings.Contains(out, udfrepair.BackupSuffix) {
		t.Fatalf("repair: code %d, out %q, err %q", code, out, errOut)
	}
	code, out, _ = runCLI("repair-udf", "--dry-run", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "nothing to repair") {
		t.Fatalf("dry run after repair: code %d, out %q", code, out)
	}
	code, out, errOut = runCLI("repair-udf", "--undo", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "undone") {
		t.Fatalf("undo: code %d, out %q, err %q", code, out, errOut)
	}
	if sum(t, p) != before {
		t.Fatal("undo didn't restore the original")
	}
}

func TestRepairUDFBatch(t *testing.T) {
	dir := t.TempDir()
	clean := writeImage(t, dir, "clean.iso", false)
	bad := writeImage(t, dir, "bad.iso", true)
	junk := filepath.Join(dir, "junk.iso")
	if err := os.WriteFile(junk, make([]byte, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI("repair-udf", clean, bad, junk)
	if code != 1 {
		t.Fatalf("code %d, want 1; out %q", code, out)
	}
	if !strings.Contains(lineWith(out, clean), "nothing to repair") ||
		!strings.Contains(lineWith(out, bad), "repaired") ||
		!strings.Contains(lineWith(out, junk), "failed:") {
		t.Fatalf("out %q", out)
	}
}

func TestRepairUDFUsage(t *testing.T) {
	if code, _, _ := runCLI("repair-udf"); code != 2 {
		t.Errorf("no images: code %d, want 2", code)
	}
	if code, _, _ := runCLI("repair-udf", "--dry-run", "--undo", "x.iso"); code != 2 {
		t.Errorf("--dry-run --undo: code %d, want 2", code)
	}
}
```

`lineWith` returns the first line containing its argument. `clean.iso`'s path is not a prefix of `bad.iso`'s, so each lookup finds the right line.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/zenvik/ -run RepairUDF`
Expected: the tests FAIL with exit code 2 and `unknown command "repair-udf"` (the root's `Args` rejects it).

- [ ] **Step 3: Implement `cmd/zenvik/repair_udf.go` and register it**

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik/internal/udfrepair"
)

func newRepairUDFCmd() *cobra.Command {
	var dryRun, undo bool
	cmd := &cobra.Command{
		Use:   "repair-udf [--dry-run | --undo] <image>...",
		Short: "Fix ISO images whose directories Linux's UDF driver rejects",
		Long: `Some authoring tools write each directory entry's checksum length without the
entry's padding. macOS and zenvik read such images, but Linux's UDF driver rejects
their directories as corrupt, so Linux can't mount them for ripping. repair-udf
corrects those 16-byte entry headers in place; the video data isn't touched.

Before changing an image it writes <image>.udf-repair-backup with every byte it
replaces, and --undo restores the original exactly. Don't run it on an image that
is mounted or in use. --dry-run only reports what it would fix.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("repair-udf needs at least one image")}
			}
			if dryRun && undo {
				return usageError{errors.New("--dry-run and --undo can't be combined")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepairUDF(cmd.OutOrStdout(), args, dryRun, undo)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be fixed; change nothing")
	cmd.Flags().BoolVar(&undo, "undo", false, "restore the original bytes from the backup")
	return cmd
}

func runRepairUDF(out io.Writer, images []string, dryRun, undo bool) error {
	failed := 0
	for _, image := range images {
		outcome, err := repairOne(image, dryRun, undo)
		if err != nil {
			failed++
			outcome = "failed: " + strings.TrimPrefix(err.Error(), "zenvik: ")
		}
		fmt.Fprintf(out, "%s: %s\n", image, outcome)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d images failed", failed, len(images))
	}
	return nil
}

func repairOne(image string, dryRun, undo bool) (string, error) {
	switch {
	case undo:
		if err := udfrepair.Undo(image); err != nil {
			return "", err
		}
		return "undone", nil
	case dryRun:
		plan, err := udfrepair.Check(image)
		if err != nil {
			return "", err
		}
		if len(plan.Patches) == 0 {
			return "nothing to repair", nil
		}
		dirs := make([]string, len(plan.Dirs))
		for i, d := range plan.Dirs {
			if d == "." {
				d = "(root)"
			}
			dirs[i] = d
		}
		return fmt.Sprintf("would repair %d entries in: %s", plan.Entries(), strings.Join(dirs, ", ")), nil
	}
	n, err := udfrepair.Repair(image)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "nothing to repair", nil
	}
	return fmt.Sprintf("repaired %d entries (backup: %s)", n, image+udfrepair.BackupSuffix), nil
}
```

In `newRootCmd`, after `root.AddCommand(newDoctorCmd())`, add `root.AddCommand(newRepairUDFCmd())`.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./... && go vet ./... && golangci-lint run`
Expected: PASS, with no lint findings.

- [ ] **Step 5: Docs**

**README.md:** find the rip section, the heading or paragraph that introduces `zenvik rip`, and add this paragraph at its end:

```markdown
**Linux can't mount some images.** Some authoring tools write ISO images whose directory entries Linux's UDF driver rejects as corrupt (the kernel log shows `udf_verify_fi: … CRC length … does not match entry length`). zenvik reads them fine, and on Linux `zenvik rip` stops before mounting such an image and says so. `zenvik repair-udf <image>` fixes the entries in place: it changes a few 16-byte headers, never the video, and writes `<image>.udf-repair-backup` first; `zenvik repair-udf --undo <image>` restores the original exactly. Use `--dry-run` to check images without changing them.
```

**CLAUDE.md:** add a bullet after the "Integration tests" bullet:

```markdown
- UDF repair (`zenvik repair-udf`, `internal/udfrepair`): `udf.(*FS).PaddingCRCFixes` finds directory entries whose tag CRC length leaves out padding (Linux's UDF driver rejects them; rip refuses them on Linux before mounting); `udfrepair` patches only those 16-byte tags, writing `<image>.udf-repair-backup` first, and `--undo` restores the original. Test images: `udfimage.Options.UnpaddedFIDCRC`. The real-kernel check is `TestKernelUDFRepair` (Linux, root, `-tags integration`).
```

- [ ] **Step 6: Commit**

```bash
git add cmd/zenvik/repair_udf.go cmd/zenvik/repair_udf_test.go cmd/zenvik/main.go README.md CLAUDE.md
git commit -m "zenvik repair-udf: fix, check or undo images whose directories Linux rejects"
```

---

### Task 6: Proof against a real Linux kernel

**Files:**
- Create: `internal/udfrepair/kernel_linux_integration_test.go`
- Modify: `.github/workflows/ci.yml` (the `integration-linux` job)

**Interfaces:**
- Consumes: `Repair` (Task 4) and `udfimage.Options.UnpaddedFIDCRC` (Task 1).

- [ ] **Step 1: Write the test**

```go
//go:build integration && linux

package udfrepair

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestKernelUDFRepair proves the repair against Linux's real UDF driver. It
// needs root (for mount); CI runs it with sudo.
func TestKernelUDFRepair(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to mount")
	}
	dir := t.TempDir()
	p := flawed(t, dir, true)
	mnt := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	mount := func() error {
		out, err := exec.Command("mount", "-t", "udf", "-o", "loop,ro", p, mnt).CombinedOutput()
		if err != nil && strings.Contains(string(out), "unknown filesystem type") {
			t.Skipf("this kernel has no UDF driver: %s", out)
		}
		if err != nil {
			t.Fatalf("mount: %v: %s", err, out)
		}
		return nil
	}
	umount := func() { _ = exec.Command("umount", mnt).Run() }
	t.Cleanup(func() { _ = exec.Command("umount", "-l", mnt).Run() })

	_ = mount()
	_, before := os.Stat(filepath.Join(mnt, "BDMV", "index.bdmv"))
	t.Logf("before repair, BDMV/index.bdmv: %v (an error is expected on kernels with udf_verify_fi)", before)
	umount()

	if n, err := Repair(p); err != nil || n == 0 {
		t.Fatalf("Repair = %d, %v", n, err)
	}

	_ = mount()
	defer umount()
	st, err := os.Stat(filepath.Join(mnt, "BDMV", "index.bdmv"))
	if err != nil || !st.Mode().IsRegular() {
		t.Fatalf("after repair, BDMV/index.bdmv: %v", err)
	}
}
```

`flawed` is the helper in `udfrepair_test.go`, built from `SampleMovie` at UDF 2.50. Linux mounts 2.50 too.

- [ ] **Step 2: Run it locally to check that it builds**

Run: `GOOS=linux go vet -tags integration ./internal/udfrepair/`
Expected: no output. It can't run on macOS.

- [ ] **Step 3: Add the CI step**

In `.github/workflows/ci.yml`'s `integration-linux` job, after the `go test -tags integration ./...` step, add:

```yaml
      - name: UDF repair against the real kernel (root)
        run: sudo -E env "PATH=$PATH" go test -tags integration -run TestKernelUDFRepair -v ./internal/udfrepair/
```

Run `go run github.com/rhysd/actionlint/cmd/actionlint@latest`. Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/udfrepair/kernel_linux_integration_test.go .github/workflows/ci.yml
git commit -m "CI: prove repair-udf against Linux's real UDF driver"
```

- [ ] **Step 5: Push the branch to see the CI result (with the user's approval)**

Ask the user first. Then run `git push -u origin feat/repair-udf` and watch that commit's CI run. In the `UDF repair against the real kernel` step's `-v` log, check:
- the "before repair" line, which says whether this kernel rejects the flawed image;
- that the test PASSES, or SKIPs with a reason.

Record both in the ledger. If it FAILS, debug before merging.

---

### Task 7: Hand-off on the user's Linux machine

- [ ] **Step 1:** Ask the user to build or install this branch on Linux, then run `zenvik repair-udf --dry-run` on all nine images and share the output.
- [ ] **Step 2:** If Atomic Blonde shows fixes, ask the user to run `zenvik repair-udf` on it, then `zenvik rip` it on Linux, and confirm the rip runs.
- [ ] **Step 3:** For any image that reports "nothing to repair" yet still fails on Linux, collect its kernel log lines. That's a different problem, out of this plan's scope.
