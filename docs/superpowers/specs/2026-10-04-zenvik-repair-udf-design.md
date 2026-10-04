# zenvik repair-udf — design

Status: approved in conversation 2026-10-04; this document records it.

## Problem

`zenvik rip` on Linux fails for some ISO images:

    zenvik: mounted …/Atomic.Blonde.2017-…-OLDHAM.iso at /run/media/chad/ATOMIC_BLONDE but found no BDMV/index.bdmv

The kernel log says why:

    UDF-fs: error (device loop4): udf_verify_fi: directory (ino 259) has entry where CRC length (31) does not match entry length (32)

Each directory entry is a UDF file identifier descriptor (FID). It starts
with a 16-byte descriptor tag, whose *descriptor CRC length* (bytes 10–11)
is the number of bytes after the tag that its CRC covers. ECMA-167 4/14.4
makes a FID's padding (to a multiple of 4 bytes) part of the descriptor,
so the CRC length should be `padded length − 16`.

This image's authoring tool wrote `unpadded length − 16` instead. That
happens on every named entry in every directory checked so far; the parent
entries have no padding, so they're correct. For example, `BDMV` is
`38 + 9 = 47` bytes, padded to 48: its CRC length is 31 but should be 32.
Linux's UDF driver checks this field strictly, treats the directory as
corrupt and can't see `BDMV`. macOS, and zenvik's own UDF reader (used by
`zenvik info`), don't check it, which is why those work.

zenvik also made the failure harder to read. The post-mount check
(`hasDiscMarker`) treats any `stat` error as "not found", so the kernel's
corruption error became "found no BDMV/index.bdmv".

The user has 8 more ISOs that fail on Linux, possibly for the same reason.

## Goals

1. **Explain it.** On Linux, `zenvik rip` detects the flaw before mounting
   and says what's wrong and how to fix it. The post-mount check reports
   real errors instead of "not found".
2. **Repair it, on request.** `zenvik repair-udf` corrects the faulty
   tags in place. It changes only those 16-byte tags, keeps an exact
   backup, and can undo the repair.

Non-goals:

- copying titles out of the image to dodge the kernel;
- a GUI repair button (the GUI shows the same error, which names the CLI
  command);
- checking or repairing anything else in the image;
- detecting the flaw on Windows. Whether Windows' disk-image mounting is
  strict about this is unknown.

## udf package: detection (read-only, standard library only)

```go
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
// is part of the descriptor). Each patch rewrites only the 16-byte tag: the
// CRC length set to padded length − 16, the CRC recomputed over that length,
// and the tag checksum recomputed. It reads every directory in the file
// system, including deleted entries (Linux checks those too), and never
// writes. A nil slice means nothing needs fixing.
func (f *FS) PaddingCRCFixes() ([]Patch, error)
```

- The walk starts at the root directory. It follows each entry with the
  directory flag that isn't the parent link, and visits each directory's
  file entry only once, so cycles and shared directories can't loop.
- Directory data is read through the directory's allocation descriptors,
  short or long. A tag's 16 bytes may cross an extent boundary. Each
  patch's `Off` is the image byte offset of the tag's first byte, and its
  `Old`/`New` cover 16 contiguous image bytes. If the tag crosses
  extents, it becomes two patches, one per contiguous run.
- An embedded directory (allocation type 3) that contains a faulty FID
  makes the walk fail with `ErrEmbeddedDir`, naming the directory. Clean
  embedded directories are fine.
- An entry whose CRC length is already `padded − 16` isn't patched. One
  whose CRC length is neither `padded − 16` nor `unpadded − 16` isn't
  patched either: that's some other problem, and `PaddingCRCFixes` leaves
  it alone.
- The existing reader is unchanged and still opens these images.

## Linux check in rip

- `source.Source` keeps the `*udf.Image` for ISO sources, as field
  `Image`, alongside `FS`.
- In `Disc.mountRoot`, for ISO sources, when `mountGOOS` (a package
  variable set to `runtime.GOOS`, overridable in tests) is `linux`, zenvik
  calls `PaddingCRCFixes` before `mount.Attach`. If it returns any patch,
  rip fails before attaching anything, with:

      zenvik: Linux can't read this image's directories: their entries' CRC lengths leave out padding, which Linux's UDF driver rejects as corrupt. Run `zenvik repair-udf <path>` to fix them in place (it keeps a backup), or rip it on macOS.

  This is new sentinel `ErrNeedsUDFRepair` in `errors.go`, wrapped with
  `%w`. If `PaddingCRCFixes` itself fails, rip carries on with the mount:
  the check is advisory, and a real problem shows up at mount time.
- `hasDiscMarker` becomes `discMarkerErr(root, f) error`: nil when the
  marker is a regular file. When it's missing, the error reads as
  "found no …". Any other error (permission, I/O, a corruption errno)
  reads as "can't read …: <error>". `mountRoot` uses that text.

## The command

```
zenvik repair-udf [--dry-run | --undo] <image>...
```

The command handles each image on its own; one image failing doesn't stop
the others. At the end it prints one summary line per image. The exit
status is 1 if any image failed, else 0. `--dry-run` and `--undo` can't be
combined (a usage error, exit 2).

Per image, the outcome is one of:
- `repaired N entries`
- `nothing to repair`
- `would repair N entries in: BDMV, BDMV/PLAYLIST, …` (dry run)
- `undone`
- `failed: <reason>`

**Repair:**

1. Open the image read-only with `udf.OpenImage` and call
   `PaddingCRCFixes`. If there are no patches, the image needs nothing.
2. If `<image>.udf-repair-backup` exists, fail without touching anything.
3. Write the backup to `<image>.udf-repair-backup.tmp`, fsync it and
   rename it to `<image>.udf-repair-backup`. The backup is JSON:

   ```json
   {"version":1,"image_size":59551694848,"patches":[{"off":1234,"old":"<hex>","new":"<hex>"}]}
   ```
4. Open the image `O_RDWR`. Re-read every patch's range. If any range
   doesn't equal `Old`, fail without writing; the image changed since
   step 1. The backup stays, and the message says it's safe to delete.
5. Write every `New` and fsync the image.
6. Re-open the image read-only. It must open with `udf.OpenImage`, and
   `PaddingCRCFixes` must return none. If not, write every `Old` back,
   fsync, delete the backup and fail with the reason.

**Undo:**

1. Read the backup. Fail if it's missing, unreadable, or its `image_size`
   differs from the image's size.
2. Open the image `O_RDWR` and check every range equals `New`. If any
   doesn't, fail without writing.
3. Write every `Old`, fsync, then delete the backup.

**Dry run:** step 1 only. It writes nothing and lists the distinct
directories, in walk order.

**Errors:**
- an image that can't be opened for writing fails with
  `can't write <image>: <error>`;
- an image that isn't UDF fails with the existing `udf.ErrNotUDF` text.

**Help text:**
- explains the flaw in one sentence;
- says the image must not be mounted or in use while it's repaired;
- says the video data isn't touched, and that `--undo` restores the
  original exactly.

## Test images

`internal/testdisc/udfimage` gains `Options.UnpaddedFIDCRC bool`. When it's
set, the builder writes each FID's descriptor CRC length as
`unpadded length − 16`, with the CRC computed over that length. It does
this for UDF 1.02 and 2.50 images.

## Testing

- **udf:**
  - a clean image has no fixes;
  - a flawed 1.02 image and a flawed 2.50 image each get exactly one patch
    per named FID, and every `Old` equals the image's bytes at `Off`;
  - after applying the patches to the in-memory image:
    - it opens;
    - every file reads the same as before;
    - `PaddingCRCFixes` returns none;
    - every byte outside the patched ranges is unchanged;
  - the reader opens flawed images;
  - an embedded flawed directory gives `ErrEmbeddedDir`, if the builder
    can embed directories. If it can't, this case is left out and the plan
    says so.
- **rip (fake mount):**
  - with `mountGOOS = "linux"`, a flawed ISO fails with
    `ErrNeedsUDFRepair` and `mount.Attach` is never called;
  - with `mountGOOS = "darwin"`, the same image proceeds to mount;
  - `discMarkerErr` reports a permission error as "can't read …", not
    "found no …".
- **repair-udf (cmd/zenvik, on temporary image files):**
  - `--dry-run` leaves the file and directory unchanged;
  - repair writes the backup, and only the patched bytes differ;
  - a second repair says "nothing to repair";
  - an existing backup makes it refuse;
  - an image changed between steps 1 and 4 is left unchanged (simulated
    through a test hook);
  - `--undo` restores a SHA-256-identical image and removes the backup;
  - a read-only image fails with "can't write";
  - in a batch of a clean image, a flawed one and a non-UDF file:
    - the first is "nothing to repair";
    - the second is repaired;
    - the third fails;
    - the exit status is 1.
- **Real Linux kernel (integration-linux CI job):** a new build-tagged
  test, run as root via `sudo -E`.
  1. Build a flawed UDF 1.02 image.
  2. `mount -t udf -o loop,ro` it and list the root. It records whether
     the listing fails; it should, on kernels with `udf_verify_fi`.
  3. Unmount it and run the repair.
  4. Mount it again: `BDMV/index.bdmv` must be readable.
  5. Unmount it.

  Step 4 is the assertion. Step 2 is logged, not asserted, so a lenient
  kernel doesn't fail the job.
- **By hand, on the user's Linux machine:** `zenvik repair-udf --dry-run`
  on all nine ISOs, then repair Atomic Blonde and rip it.

## Docs

- **README:** a "Linux: images Linux can't mount" note under the rip
  section, naming `zenvik repair-udf`.
- **CLAUDE.md:** a line under Commands.
- **The `udf` package doc:** `PaddingCRCFixes` and `Patch`.
