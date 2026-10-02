# zenvik — flattened BDMV folders

- **Date:** 2026-10-02
- **Status:** Draft
- **Extends:** [zenvik v1 design](2026-10-01-zenvik-v1-design.md). Everything there still applies unless this document says otherwise.

## 1. Purpose and scope

Some releases arrive with the Blu-ray directory tree flattened. The files from `BDMV/PLAYLIST`, `BDMV/CLIPINF`, `BDMV/STREAM`, `BDMV/BACKUP` and `BDMV/META/DL` all sit in the release folder, and only a few files keep their directories. `zenvik info` reports `no playlists found` because the scanner reads only `BDMV/PLAYLIST`.

The motivating release is *Forbidden Planet (1956)* UHD. Its folder holds:

- `BDMV/index.bdmv`, `BDMV/MovieObject.bdmv` and `CERTIFICATE/id.bdmv`, in their usual places;
- `00000.mpls`–`00005.mpls`, `00000.clpi`–`00009.clpi` and `00000.m2ts`–`00009.m2ts` at the top level;
- the `BDMV/BACKUP` copies at the top level with `.1` before the extension: `00000.1.mpls`, `00000.1.clpi`, `MovieObject.1.bdmv`;
- `BDMV_index.bdmv` (the backup `index.bdmv`) and `CERTIFICATE_id.1.bdmv` (the backup `id.bdmv`);
- `bdmt_eng.xml` and its two JPEG thumbnails, from `BDMV/META/DL`.

Every flattened file is byte-identical to its backup copy and to the copy that kept its directory. Rebuilding the tree by hand (hardlinks into `BDMV/PLAYLIST` etc.) makes `info` list the 1:38:28 feature, playlist 00001.

### Goals

- `info` and `rip` accept a flattened folder, passed either as the release folder or as its `BDMV` subfolder.
- Nothing in the release folder is written, moved or renamed.
- Folders that open today open exactly as before.

### Non-goals

- Flattened ISO images. A UDF image has the directory tree it was authored with.
- Files the scanner and mkvmerge don't read: `AUXDATA`, `BDJO`, `JAR`, `CERTIFICATE`, the `META/DL` thumbnails, 3D `.ssif` streams.
- Partly flattened trees that have a `BDMV/PLAYLIST` directory. If it exists, the folder is a normal BDMV folder.

## 2. Detection

`source.Open` on a directory first picks the candidate root as today: the given folder, or its parent when the given folder is named `BDMV` and holds `index.bdmv`. It then classifies the root, taking the first case that matches:

1. `BDMV/index.bdmv` exists and `BDMV/PLAYLIST` is a directory: `BDMVDir`.
2. The root is flattened (below): `FlatBDMVDir`.
3. `BDMV/index.bdmv` exists: `BDMVDir`, as today (such a folder scans to `no playlists found`).
4. A `VIDEO_TS` directory holds `VIDEO_TS.IFO`: `VideoTSDir`, as today.
5. Otherwise `ErrUnsupported` with the existing message.

The root is flattened when all of these hold:

1. it has no `BDMV/PLAYLIST` directory;
2. its top level holds at least one playlist, `NNNNN.mpls` or `NNNNN.1.mpls` (five ASCII digits, lower-case extension, the same strict form as `isPlaylistName`);
3. an index exists, found in this order: `BDMV/index.bdmv`, `BDMV_index.bdmv`, `index.bdmv`, `index.1.bdmv`.

A flattened folder opens as `Source{Kind: FlatBDMVDir, Format: Bluray, Path: <root>, Label: <its base name>}`. Every folder that opened before opens with the same kind, except one with `BDMV/index.bdmv`, no `BDMV/PLAYLIST` and top-level playlists, which used to open as an empty `BDMVDir`. Forbidden Planet is such a folder.

## 3. The path map

`internal/source/flat.go` lists the root's top level and its `BDMV` directory once and builds a map from standard tree paths (slash-separated, as `fs.FS` uses) to paths relative to the root. AppleDouble `._` entries and directories are skipped. Names match exactly; a release that changed case is not handled.

| Standard path | Source, in order of preference |
|---|---|
| `BDMV/index.bdmv` | `BDMV/index.bdmv`, `BDMV_index.bdmv`, `index.bdmv`, `index.1.bdmv` |
| `BDMV/MovieObject.bdmv` | `BDMV/MovieObject.bdmv`, `BDMV_MovieObject.bdmv`, `MovieObject.bdmv`, `MovieObject.1.bdmv` |
| `BDMV/PLAYLIST/NNNNN.mpls` | `NNNNN.mpls`, `NNNNN.1.mpls` |
| `BDMV/CLIPINF/NNNNN.clpi` | `NNNNN.clpi`, `NNNNN.1.clpi` |
| `BDMV/STREAM/NNNNN.m2ts` | `NNNNN.m2ts` |
| `BDMV/META/DL/bdmt_<lang>.xml` | top-level `bdmt_<lang>.xml` |

The backup copies are used only when the primary copy is missing; when both exist they are not compared.

### The `fs.FS`

`flatFS` serves the map. It implements `fs.FS`, `fs.ReadDirFS` and `fs.StatFS`:

- `Open` of a mapped file opens the real file through `os.DirFS(root)`.
- `Open`, `Stat` and `ReadDir` of `.`, `BDMV`, and any directory that is a prefix of a mapped path (`BDMV/PLAYLIST`, `BDMV/META`, `BDMV/META/DL`, …) return a synthesized directory whose entries are the mapped children, sorted by name. Directory entries report `fs.ModeDir`; file entries report the real file's `fs.FileInfo` under the standard name.
- Anything else returns `fs.ErrNotExist` wrapped in `*fs.PathError`; invalid paths return `fs.ErrInvalid` the same way.

`fstest.TestFS` must pass on a `flatFS`.

The scanner is unchanged: it reads `BDMV/PLAYLIST`, `BDMV/STREAM`, `BDMV/index.bdmv`, `BDMV/MovieObject.bdmv` and `BDMV/META/DL` through the `fs.FS` as before.

`Source` gains an unexported field holding the map, and a method `Files() map[string]string` returning a copy of it (standard path → absolute real path) for the rip step. It returns nil for other kinds.

## 4. Source kind

- `source.FlatBDMVDir Kind = 4`, re-exported as `zenvik.FlatBDMVDir`.
- `Kind.String()` is `"BDMV folder (flattened)"`, which `info`'s header shows.
- `kindName` in `info --json` and in `rip --jsonl` `start` events is `"bdmv_flat"`. The README's list of `kind` values gains it.

## 5. Ripping

mkvmerge is given `<root>/BDMV/PLAYLIST/<id>.mpls` and finds the clips through the directories beside it, so it needs a real tree. `Disc.mountRoot` gains a case for `FlatBDMVDir`, beside the ISO case:

1. `os.MkdirTemp("", "zenvik-bdmv-*")`.
2. For every entry of `Source.Files()`, create the parent directories and link `<temp>/<standard path>` to the absolute real path with `os.Symlink`, called through an unexported package variable so tests can make it fail. Symlinks take no space and work across filesystems.
3. Return the temp directory as the root, and a release function that removes it with `os.RemoveAll`. `RemoveAll` removes the links, never their targets.

If any step fails, everything created so far is removed and `Rip` returns the error. A failed `os.Symlink` (on Windows without Developer Mode or admin rights) is wrapped as `zenvik: can't link <file> into a temporary BDMV tree (rebuild the BDMV folder layout and pass that instead): <err>`.

There is no new progress phase: linking a few dozen files is instant. The release function's error is handled as the ISO detach error is today (a warning on success, joined to the error on failure).

`--dry-run` builds and removes the tree like a real rip, so the printed mkvmerge command names a temp path that no longer exists. That matches the ISO dry run, whose command names the mount point. If zenvik is killed hard, the temp tree is left in the system temp directory; it holds only links.

## 6. Testing

Fixtures come from `internal/testdisc`, flattened in the test by renaming files into the release's naming scheme. A helper `flatten(t, root)` does this, keeping `BDMV/index.bdmv` and `BDMV/MovieObject.bdmv` in place and writing the `.1` backups and `BDMV_index.bdmv` as copies.

Unit tests (`go test -race ./...`):

- `internal/source`:
  - the flattened folder and its `BDMV` subfolder both open as `FlatBDMVDir` with the folder's path and label;
  - a flattened folder with no `BDMV` directory, whose index is `BDMV_index.bdmv` or `index.bdmv`, opens;
  - a playlist present only as `NNNNN.1.mpls` is mapped; when both copies exist the primary wins;
  - a folder with `.mpls` files but no index, and one with an index but no playlists, still fail with `ErrUnsupported`;
  - the existing `TestOpenPathVariants` cases still open as `BDMVDir`;
  - `fstest.TestFS` passes on the flattened FS;
  - `Files()` returns absolute paths, and nil for other kinds.
- `zenvik`:
  - `Open` on a flattened `SampleMovie` returns the same titles, ranks and metadata as on the original tree;
  - `mountRoot` for a flattened disc returns a directory whose `BDMV/PLAYLIST`, `BDMV/CLIPINF` and `BDMV/STREAM` entries resolve to the original files, and the release function removes it while the originals remain;
  - a link failure (injected through the package variable) returns the wrapped error and leaves no temp directory.
- `cmd/zenvik`: `kindName(zenvik.FlatBDMVDir)` is `"bdmv_flat"`; `info` on a flattened sample prints `BDMV folder (flattened)`.

Integration test (`-tags integration`): rip a flattened `SampleMovie` with mkvmerge and check the output's duration and tracks match a rip of the original tree.
