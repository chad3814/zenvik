# zenvik: bundle mkvmerge with the desktop app — design

**Status:** approved in conversation on 2026-10-04 (sections 1–3). Written for review.
**Builds on:**
- `2026-10-02-zenvik-gui-design.md` (the GUI);
- `2026-10-03-zenvik-macos-signing-design.md` (`scripts/macos-sign.sh`, the release workflow's `release` environment);
- `scripts/release-gui.sh` as it stands at `5e842b7`.

## 1. Goal and scope

The macOS and Windows desktop downloads ship MKVToolNix's `mkvmerge`, so Zenvik works with nothing else installed. A user's own `mkvmerge` still wins when they point the config at it.

**In scope:**
- `Zenvik.app` in `zenvik-gui_<ver>_darwin_arm64.zip` and `zenvik-gui_<ver>_darwin_amd64.zip`;
- the Windows GUI zip `zenvik-gui_<ver>_windows_amd64.zip`.

**Out of scope:**
- the CLI archives;
- the Linux GUI, where `mkvmerge` stays a system dependency;
- any MKVToolNix tool other than `mkvmerge`;
- building MKVToolNix from source.

**Facts this design rests on** (checked against MKVToolNix 102.0, 2026-10-04):
- **macOS DMGs:**
  - `mkvtoolnix.download/macos/releases/<ver>/` publishes per-architecture DMGs, `MKVToolNix-<ver>-<build>-arm64.dmg` and `…-x86_64.dmg`, with `.sha256` files. 102.0's current build is `2`.
  - `MKVToolNix.app/Contents/MacOS/mkvmerge` (7.1 MB, arm64) links only one non-system library, `@executable_path/libs/libQt6Core.6.dylib`. That's a symlink to `libQt6Core.6.11.1.dylib`, 5.8 MB, which has no further non-system dependencies.
  - Upstream signs both with Graham Thompson's Developer ID (team `H4MM26UAYB`), uses the hardened runtime, notarizes them, and targets macOS 13 minimum.
- **Copying and re-signing:**
  - Copied out with the library at `libs/libQt6Core.6.dylib` beside it, `mkvmerge --version` runs.
  - Re-signed with `Developer ID Application: Chad Walker (SZUN8RDF5D)` and `--options runtime --timestamp`, it still runs.
  - Re-signing `mkvmerge` but not the library fails library validation ("different Team IDs"). The two must always carry the same team's signature.
- **Windows:** `mkvtoolnix.download/windows/releases/<ver>/mkvtoolnix-64-bit-<ver>.7z` has a `.sha256`. Its `mkvtoolnix/mkvmerge.exe` (23 MB) imports only DLLs that ship with Windows.
- **Source:** `mkvtoolnix.download/sources/mkvtoolnix-<ver>.tar.xz` (11 MB) has `.sha256` and `.sig` files.

## 2. Licensing (GPLv2)

`mkvmerge` is GPLv2. zenvik (MIT) only runs it as a separate program and never links it, so shipping them together is mere aggregation, and zenvik's license doesn't change. Each GUI zip that bundles `mkvmerge` also includes:

- **`MKVTOOLNIX-COPYING.txt`:** upstream's GPLv2 text, copied from the same upstream artifact as the binary. That's `COPYING.txt` at the DMG root, or the 7z's `mkvtoolnix/COPYING.txt`. If the 7z has none, it is taken from the pinned source tarball instead.
- **`MKVTOOLNIX-NOTICE.txt`**, which says:
  - that the zip bundles `mkvmerge` from MKVToolNix `<ver>` (© Moritz Bunkus and contributors, GPLv2);
  - on macOS, together with the Qt Core library it needs (LGPLv3);
  - that it is unmodified, except that on macOS both files are re-signed with zenvik's Developer ID;
  - the source URL: `https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-<ver>/mkvtoolnix-<ver>.tar.xz`;
  - MKVToolNix's homepage, `https://mkvtoolnix.download/`.

**The source home:** a source-only GitHub release, one per bundled version.
- **Tag:** `mkvtoolnix-src-<ver>`. It doesn't start with `v`, so it never triggers the release workflow.
- **Assets:** `mkvtoolnix-<ver>.tar.xz` and `mkvtoolnix-<ver>.tar.xz.sha256`, both exactly as published upstream.
- **Settings:** marked neither "latest" nor pre-release. Its notes say which Zenvik versions bundle it.

**Qt Core (macOS only, LGPLv3).** The bundled `libQt6Core` comes from the same MKVToolNix build, and the LGPLv3 brings obligations of its own:
- **License texts:** the macOS zips also include `QT-LGPL-3.0.txt` and `QT-GPL-3.0.txt`. These are the FSF's LGPLv3 and GPLv3 texts, committed once under `third_party/licenses/`, since license texts don't change.
- **Notice:** `MKVTOOLNIX-NOTICE.txt` names the Qt version, taken from the library's file name (`libQt6Core.6.11.1` → Qt 6.11.1), and says it is dynamically linked and replaceable.
- **Source:** the `qtbase` source for that version. The notice links to the Qt Project's archive, `https://download.qt.io/archive/qt/<major.minor>/<version>/submodules/qtbase-everywhere-src-<version>.tar.xz`. It isn't copied into the source-only release, because it is about 50 MB and Qt keeps its archive online. If the user later wants it self-hosted too, `bump-mkvtoolnix.sh` can attach it to the same `mkvtoolnix-src-<ver>` release.

## 3. Pinning and fetching

- **`third_party/mkvtoolnix.env`** is sourced by the scripts:

  ```sh
  MKVTOOLNIX_VERSION=102.0
  MKVTOOLNIX_MACOS_BUILD=2
  MKVTOOLNIX_SHA256_MACOS_ARM64=<sha256 of MKVToolNix-102.0-2-arm64.dmg>
  MKVTOOLNIX_SHA256_MACOS_X86_64=<sha256 of MKVToolNix-102.0-2-x86_64.dmg>
  MKVTOOLNIX_SHA256_WINDOWS_64=<sha256 of mkvtoolnix-64-bit-102.0.7z>
  MKVTOOLNIX_SHA256_SOURCE=<sha256 of mkvtoolnix-102.0.tar.xz>
  ```

  The checksums are the upstream `.sha256` values, recorded when the version is bumped.
- **`scripts/fetch-mkvmerge.sh <os/arch> <outdir>`** downloads the pinned upstream file with `curl -fL` and checks it against its pinned SHA-256. A mismatch fails the script and says which file failed.
  - **darwin/arm64 and darwin/amd64:** it attaches the DMG read-only (`hdiutil attach -nobrowse -readonly`). It copies `Contents/MacOS/mkvmerge` to `<outdir>/mkvmerge`. It copies the real file behind `Contents/MacOS/libs/libQt6Core.6.dylib` to `<outdir>/libs/libQt6Core.6.dylib`, which also records the Qt version for the notice. It copies `COPYING.txt` to `<outdir>/MKVTOOLNIX-COPYING.txt`. The image is detached on exit, even if the script fails.
  - **windows/amd64:** it extracts `mkvtoolnix/mkvmerge.exe` and the license (§2) into `<outdir>`, using `7z` on the Windows runner or `tar` (libarchive) elsewhere.
  - **Any other target:** it exits 2 with "mkvmerge is not bundled for <target>".
  - It writes `<outdir>/MKVTOOLNIX-NOTICE.txt` from the pinned version (and, on macOS, the Qt version).
- **`scripts/bump-mkvtoolnix.sh <version> [<macos-build>]`** downloads upstream's `.sha256` files for the four artifacts and rewrites `third_party/mkvtoolnix.env`. It then prints the `gh release create mkvtoolnix-src-<version> …` command, but runs it only with `--publish`, because publishing a release needs the user's explicit go-ahead.

## 4. Packaging

- **macOS (`release-gui.sh`, darwin targets).** After `wails build`, and before signing, the script runs `fetch-mkvmerge.sh` into a temp folder. It then copies:
  - `mkvmerge` to `Zenvik.app/Contents/Helpers/mkvmerge`;
  - `libs/libQt6Core.6.dylib` to `Zenvik.app/Contents/Helpers/libs/libQt6Core.6.dylib`;
  - the two `MKVTOOLNIX-*.txt` files into the zip's top folder, beside `LICENSE` and `README.md`.

  `mkvmerge` finds Qt through `@executable_path/libs/`, so that layout needs no changes.
- **macOS signing.** `macos_sign <app>` signs inside out:
  1. every Mach-O file under `Contents/Helpers/libs/`;
  2. every executable directly in `Contents/Helpers/`;
  3. everything in `Contents/MacOS/`;
  4. the bundle.

  Each signature uses the Developer ID, `--options runtime` and `--timestamp`, which replaces upstream's signature. `macos_verify_signature <app>` also verifies each helper file and checks it carries the Developer ID authority. Notarizing and stapling then work as today.
- **macOS proof.** After signing, and again after stapling, `release-gui.sh` runs `Zenvik.app/Contents/Helpers/mkvmerge --version` and requires `mkvmerge v<ver>`. This proves the re-signed helper still loads Qt under library validation.
- **Windows.** `mkvmerge.exe` goes in the zip beside `zenvik-gui.exe`, along with the two `MKVTOOLNIX-*.txt` files. `release-gui.sh` runs `mkvmerge.exe --version` on the Windows runner. The file isn't code-signed, and neither is `zenvik-gui.exe`.
- **Linux, the CLI scripts, and local `wails build`/`wails dev`** don't change.
- **Size:** about 13 MB more per macOS app before compression, and about 23 MB more for the Windows zip.

## 5. How the app finds mkvmerge

- **`resolveMkvmerge(configured, exe, goos string, exists func(string) bool) (path, source string)`** in `gui/` returns the path to use and where it came from: `config`, `bundled` or `path`.
  1. If `configured` (the config's `mkvmerge_path`) isn't empty, that path is used, with source `config`. If it's broken, the mkvmerge banner says so; there is no fallback.
  2. Otherwise, the bundled copy is used if it exists, with source `bundled`:
     - on darwin, `<dir of exe>/../Helpers/mkvmerge`;
     - on windows, `<dir of exe>/mkvmerge.exe`.

     `exe` is `os.Executable()` with symlinks resolved.
  3. Otherwise the result is `""`, meaning a `PATH` lookup, with source `path`.
- **One shared answer.** `App.mkvmergePath()` returns the resolved path, so the mkvmerge check (startup, Recheck, config reload) and every rip use the same answer.
- **About box.** The new bound method `MkvmergeInfo() string` returns, for example:
  - `mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)`
  - `mkvmerge 101.0 (/usr/local/bin/mkvmerge)`
  - `mkvmerge not found`

  The version comes from `mux.Find`, which runs once per check and caches the result. The About dialog shows this line, and for the bundled copy it links to the source release.
- **No change** to the CLI or to `zenvik doctor`.

## 6. Testing and verification

- **Go unit tests:**
  - `resolveMkvmerge` across a configured path, a bundled copy on darwin and on windows, a missing bundled copy, and a `PATH` fallback (fake app layouts in a temp dir);
  - the `MkvmergeInfo` text for each source;
  - that the App's rips and checks receive the resolved path.
- **Frontend test:** the About box shows the mkvmerge line, and the source link when the copy is bundled.
- **Shell tests (`scripts/fetch-mkvmerge_test.sh`):**
  - a checksum mismatch fails, naming the file (using a pinned-file override that points at a local fixture, not the network);
  - an unsupported target exits 2;
  - on macOS, a real fetch of the pinned DMG produces exactly `mkvmerge`, `libs/libQt6Core.6.dylib`, `MKVTOOLNIX-COPYING.txt` and `MKVTOOLNIX-NOTICE.txt`, and `mkvmerge --version` runs. This test is network-tagged: it runs only with `ZENVIK_NET_TESTS=1`.
- **Lint:** `shellcheck -x` and `actionlint` stay clean.
- **Local signed build:** `MACOS_SIGN_IDENTITY=… scripts/release-gui.sh v0.0.0-test darwin/arm64` produces an app whose helpers verify, carry the Developer ID, and run `--version` from inside the zip's copy.
- **First real run, `v1.1.0-rc3`**, after merging. Before it, the source release `mkvtoolnix-src-102.0` is created, with the user's go-ahead. Then:
  - for both darwin apps: `spctl` reports `accepted` and `source=Notarized Developer ID`, `stapler validate` passes, and the helpers pass `codesign --verify` and run `--version`;
  - the Windows zip holds `mkvmerge.exe` and both notice files;
  - the notice's source URL downloads, and the file matches the pinned SHA-256;
  - the user rips one disc with the downloaded app with `mkvmerge_path` unset. A Finder-launched app doesn't get Homebrew's `PATH`, so this proves the bundled copy is used.

## 7. Documentation

- **`gui/README.md`:** say that the macOS and Windows apps include `mkvmerge` from MKVToolNix (GPLv2; source at the release link). Linux still needs MKVToolNix installed. `mkvmerge_path` in the config overrides the bundled copy.
- **`README.md`:** in the desktop-app paragraph, say MKVToolNix is bundled on macOS and Windows.
- **`CLAUDE.md`:**
  - `third_party/mkvtoolnix.env`, `fetch-mkvmerge.sh` and `bump-mkvtoolnix.sh`;
  - that `bump-mkvtoolnix.sh --publish` creates a public release, so it needs explicit approval;
  - that the bundled `mkvmerge` and its Qt library are always re-signed together.
