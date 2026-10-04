# Bundle mkvmerge with the Desktop App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The macOS and Windows desktop downloads ship MKVToolNix's `mkvmerge` 102.0, with its GPL and third-party notices and a self-hosted source release. The app prefers the config's `mkvmerge_path`, then the bundled copy, then `PATH`.

**Architecture:**
- `third_party/mkvtoolnix.env` pins the version and its upstream checksums.
- `scripts/fetch-mkvmerge.sh` downloads and verifies the pinned files and extracts `mkvmerge` plus the license files. `release-gui.sh` puts them in the app: `Contents/Helpers` on macOS, re-signed with the rest of the app, or next to `zenvik-gui.exe` on Windows.
- In the GUI, `resolveMkvmerge` chooses the binary, and the About box reports it through two new bound methods.

**Tech Stack:** bash 3.2-compatible scripts (`curl`, `shasum`/`sha256sum`, `hdiutil`, `tar`, `7z` on the Windows runner); Go (the `gui/` module); React/TypeScript (Vitest); `gh` for the source release.

**Spec:** `docs/superpowers/specs/2026-10-04-zenvik-bundle-mkvmerge-design.md`. Task 1 brings it in line with the Plan decisions below.

## Global Constraints

- **Pinned MKVToolNix:** version `102.0`, macOS DMG build `2`. The upstream SHA-256s are exactly:
  - `MKVToolNix-102.0-2-arm64.dmg`: `10ebd3b424fd5f4987d50563fc43b5956bf3d4f3bbfa24fa870db956c96578b8`
  - `MKVToolNix-102.0-2-x86_64.dmg`: `43ca3ed10f8035552fe38de24d9c40a99af53b40f9caa4dabccabcf58f89bf42`
  - `mkvtoolnix-64-bit-102.0.7z`: `cd63c42caee3d9e5b631e029657b3bd322beba77e150cecbfc9ae4923638b049`
  - `mkvtoolnix-102.0.tar.xz`: `9f0a810f17c7df8adb9064a3a41d5784399be412d19704cf080745ad7d45da30`
- **Upstream URLs:**
  - `https://mkvtoolnix.download/macos/releases/<ver>/MKVToolNix-<ver>-<build>-<arm64|x86_64>.dmg`
  - `https://mkvtoolnix.download/windows/releases/<ver>/mkvtoolnix-64-bit-<ver>.7z`
  - `https://mkvtoolnix.download/sources/mkvtoolnix-<ver>.tar.xz`
- **Checksums:** every downloaded file is checked against its pinned SHA-256 before use. A mismatch fails the build and names the file.
- **Bundled layout:**
  - macOS: `Zenvik.app/Contents/Helpers/mkvmerge` and `Zenvik.app/Contents/Helpers/libs/libQt6Core.6.dylib`;
  - Windows: `mkvmerge.exe` beside `zenvik-gui.exe`;
  - Linux and the CLI archives: nothing bundled.
- **macOS signing:** the helper and its Qt library are always re-signed **together** with `Developer ID Application: Chad Walker (SZUN8RDF5D)`, `--options runtime` and `--timestamp`. Mixing team IDs fails library validation.
- **Every bundling zip** carries `MKVTOOLNIX-NOTICE.txt`, `MKVTOOLNIX-COPYING.txt` and a `MKVTOOLNIX-LICENSES/` folder.
- **Source release:** tag `mkvtoolnix-src-<ver>`, with assets `mkvtoolnix-<ver>.tar.xz` and `mkvtoolnix-<ver>.tar.xz.sha256`. It's never "latest". The notice links `https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-<ver>/mkvtoolnix-<ver>.tar.xz`.
- **Lookup order:** the config's `mkvmerge_path`, if set, always wins and never falls back. Then the bundled copy beside the resolved executable. Then `PATH`.
- **Approvals:** publishing a release (the source release, or `bump-mkvtoolnix.sh --publish`), merging, pushing and tagging all need the user's explicit go-ahead.
- **Code rules:** no TypeScript `any`. Don't edit `gui/frontend/wailsjs` by hand; regenerate it. Scripts pass `shellcheck -x`, and the workflows pass `actionlint`. Commits have descriptive sentence subjects and are signed.

## Plan decisions (refinements of the spec; Task 1 writes them into it)

1. **License files come from the pinned source tarball** (`mkvtoolnix-<ver>/COPYING` and `mkvtoolnix-<ver>/doc/licenses/`) for both platforms, instead of from each binary artifact.
   - That folder covers the third-party code built into `mkvmerge` (Boost, fmt, pugixml, nlohmann-json) and LGPL-3.0, which Qt needs.
   - The FSF's GPLv3 text, which LGPLv3 incorporates, is committed once as `third_party/licenses/GPL-3.0.txt` and copied into `MKVTOOLNIX-LICENSES/`.
   - The spec's `QT-LGPL-3.0.txt` and `QT-GPL-3.0.txt` become `MKVTOOLNIX-LICENSES/LGPL-3.0.txt` and `MKVTOOLNIX-LICENSES/GPL-3.0.txt`.
2. **The app learns the bundled MKVToolNix version at build time** through `-ldflags "-X main.mkvtoolnixVersion=<ver>"`. `release-gui.sh` sets it only for darwin and windows. The About box shows that version, and links the source release, only when the bundled copy is in use.
3. **`Deps.FindMkvmerge` returns the version** that `mux.Find` reports: `func(ctx, path) (string, error)`. A non-bundled copy shows it as `X.Y.Z`.

## Review Focus

1. **The bundled helper fails to load Qt after signing** (team mismatch, or a wrong `libs/` path). The release must fail before packaging, not ship a broken app. Test: the `--version` check after signing and after stapling in `release-gui.sh` (Task 5), and the fake-app signing test (Task 3).
2. **The app runs from an unusual place** (a symlinked app, a translocated download, a folder with spaces). It must still find its bundled copy. Test: `resolveMkvmerge` with a path containing spaces, and a symlink resolved by `defaultDeps` (Task 4).
3. **The config's `mkvmerge_path` points at something broken while a bundled copy exists.** The app must show the banner and must not silently use the bundled copy. Test: `TestBrokenConfiguredMkvmergeDoesNotFallBack` (Task 4).
4. **Upstream changes or removes a file** (a new DMG build number, or a re-uploaded file). The build must fail, naming the file, instead of shipping something unverified. Test: the checksum-mismatch case in `fetch-mkvmerge_test.sh` (Task 1).
5. **The license files are missing from a zip.** Test: Task 5's zip listing and Task 7's check of the downloaded release.

---

### Task 1: Pin, license text, and `fetch-mkvmerge.sh`

**Files:**
- Create: `third_party/mkvtoolnix.env`, `third_party/licenses/GPL-3.0.txt`, `scripts/fetch-mkvmerge.sh`, `scripts/fetch-mkvmerge_test.sh`
- Modify: `docs/superpowers/specs/2026-10-04-zenvik-bundle-mkvmerge-design.md` (Plan decisions 1–3)

**Interfaces:**
- Produces `scripts/fetch-mkvmerge.sh <os/arch> <outdir>`:
  - Writes `<outdir>/mkvmerge` and `<outdir>/libs/libQt6Core.6.dylib` (darwin), or `<outdir>/mkvmerge.exe` (windows).
  - On both, writes `<outdir>/MKVTOOLNIX-NOTICE.txt`, `<outdir>/MKVTOOLNIX-COPYING.txt` and `<outdir>/MKVTOOLNIX-LICENSES/`.
  - Exits 2 on an unsupported target, and 1 on a checksum mismatch or extraction failure.
  - Overrides, for tests: `MKVTOOLNIX_CACHE` (a folder whose files are used instead of downloading) and `MKVTOOLNIX_BASE_URL`.
- Produces `third_party/mkvtoolnix.env` with `MKVTOOLNIX_VERSION`, `MKVTOOLNIX_MACOS_BUILD`, `MKVTOOLNIX_SHA256_MACOS_ARM64`, `MKVTOOLNIX_SHA256_MACOS_X86_64`, `MKVTOOLNIX_SHA256_WINDOWS_64` and `MKVTOOLNIX_SHA256_SOURCE`.

- [ ] **Step 1: Write the pin file and the GPLv3 text**

`third_party/mkvtoolnix.env`:

```sh
# MKVToolNix release whose mkvmerge the desktop app bundles (macOS, Windows).
# Update with scripts/bump-mkvtoolnix.sh; the checksums are upstream's own
# .sha256 values for each file.
MKVTOOLNIX_VERSION=102.0
MKVTOOLNIX_MACOS_BUILD=2
MKVTOOLNIX_SHA256_MACOS_ARM64=10ebd3b424fd5f4987d50563fc43b5956bf3d4f3bbfa24fa870db956c96578b8
MKVTOOLNIX_SHA256_MACOS_X86_64=43ca3ed10f8035552fe38de24d9c40a99af53b40f9caa4dabccabcf58f89bf42
MKVTOOLNIX_SHA256_WINDOWS_64=cd63c42caee3d9e5b631e029657b3bd322beba77e150cecbfc9ae4923638b049
MKVTOOLNIX_SHA256_SOURCE=9f0a810f17c7df8adb9064a3a41d5784399be412d19704cf080745ad7d45da30
```

```bash
mkdir -p third_party/licenses
curl -fsSL https://www.gnu.org/licenses/gpl-3.0.txt -o third_party/licenses/GPL-3.0.txt
head -3 third_party/licenses/GPL-3.0.txt
```

Expected: the file starts with `GNU GENERAL PUBLIC LICENSE` / `Version 3, 29 June 2007`.

- [ ] **Step 2: Write the failing tests**

`scripts/fetch-mkvmerge_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/fetch-mkvmerge.sh. The offline cases run anywhere; the
# real downloads (about 70 MB) run only with ZENVIK_NET_TESTS=1, the darwin
# one only on macOS.
#
#   scripts/fetch-mkvmerge_test.sh
#   ZENVIK_NET_TESTS=1 scripts/fetch-mkvmerge_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
# shellcheck source=third_party/mkvtoolnix.env
source "$root/third_party/mkvtoolnix.env"
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() {
	fail=$((fail + 1))
	echo "FAIL $1"
	if [[ -n ${2:-} ]]; then echo "     $2"; fi
}

out=$("$here/fetch-mkvmerge.sh" linux/amd64 "$(mktemp -d)" 2>&1)
status=$?
if [[ $status -eq 2 && $out == *"mkvmerge is not bundled for linux/amd64"* ]]; then
	ok "unsupported target exits 2"
else
	bad "unsupported target exits 2" "status $status: $out"
fi

cache=$(mktemp -d)
echo junk >"$cache/mkvtoolnix-64-bit-$MKVTOOLNIX_VERSION.7z"
out=$(MKVTOOLNIX_CACHE=$cache "$here/fetch-mkvmerge.sh" windows/amd64 "$(mktemp -d)" 2>&1)
status=$?
if [[ $status -eq 1 && $out == *"checksum mismatch for mkvtoolnix-64-bit-$MKVTOOLNIX_VERSION.7z"* ]]; then
	ok "checksum mismatch fails naming the file"
else
	bad "checksum mismatch fails naming the file" "status $status: $out"
fi
rm -rf "$cache"

out=$("$here/fetch-mkvmerge.sh" darwin/arm64 2>&1)
status=$?
if [[ $status -eq 2 ]]; then ok "missing outdir is a usage error"; else bad "missing outdir is a usage error" "status $status: $out"; fi

if [[ ${ZENVIK_NET_TESTS:-} == 1 ]]; then
	cache=$(mktemp -d)
	if [[ $(uname -s) == Darwin ]]; then
		d=$(mktemp -d)
		if MKVTOOLNIX_CACHE=$cache "$here/fetch-mkvmerge.sh" darwin/arm64 "$d" >/dev/null 2>&1; then
			got=$(cd "$d" && find . -type f -o -type l | sort | tr '\n' ' ')
			want_files=("./MKVTOOLNIX-COPYING.txt" "./MKVTOOLNIX-NOTICE.txt" "./libs/libQt6Core.6.dylib" "./mkvmerge")
			missing=""
			for f in "${want_files[@]}"; do
				if [[ $got != *"$f "* ]]; then missing="$missing $f"; fi
			done
			if [[ -z $missing && -f $d/MKVTOOLNIX-LICENSES/GPL-3.0.txt && -f $d/MKVTOOLNIX-LICENSES/LGPL-3.0.txt ]]; then
				ok "darwin fetch writes the helper, Qt and the license files"
			else
				bad "darwin fetch writes the helper, Qt and the license files" "missing:$missing; got: $got"
			fi
			if "$d/mkvmerge" --version | grep -q "mkvmerge v$MKVTOOLNIX_VERSION"; then
				ok "fetched mkvmerge runs from its new place"
			else
				bad "fetched mkvmerge runs from its new place"
			fi
			if grep -q "Qt 6\." "$d/MKVTOOLNIX-NOTICE.txt" && grep -q "mkvtoolnix-src-$MKVTOOLNIX_VERSION" "$d/MKVTOOLNIX-NOTICE.txt"; then
				ok "darwin notice names Qt and the source release"
			else
				bad "darwin notice names Qt and the source release" "$(cat "$d/MKVTOOLNIX-NOTICE.txt")"
			fi
		else
			bad "darwin fetch succeeds"
		fi
		rm -rf "$d"
	fi
	w=$(mktemp -d)
	if MKVTOOLNIX_CACHE=$cache "$here/fetch-mkvmerge.sh" windows/amd64 "$w" >/dev/null 2>&1 &&
		head -c 2 "$w/mkvmerge.exe" | grep -q MZ && [[ -f $w/MKVTOOLNIX-COPYING.txt ]] && ! grep -q "Qt" "$w/MKVTOOLNIX-NOTICE.txt"; then
		ok "windows fetch writes mkvmerge.exe and the notices"
	else
		bad "windows fetch writes mkvmerge.exe and the notices" "$(ls -la "$w")"
	fi
	rm -rf "$w" "$cache"
else
	echo "skip network fetches (set ZENVIK_NET_TESTS=1)"
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

```bash
chmod 755 scripts/fetch-mkvmerge_test.sh
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `scripts/fetch-mkvmerge_test.sh`
Expected: three FAILs (`fetch-mkvmerge.sh: No such file or directory`) and `0 passed, 3 failed`.

- [ ] **Step 4: Write `scripts/fetch-mkvmerge.sh`**

```bash
#!/usr/bin/env bash
# Fetch the pinned MKVToolNix mkvmerge that the desktop app bundles, with its
# license files, into outdir. Every download is checked against the SHA-256
# pinned in third_party/mkvtoolnix.env.
#
#   scripts/fetch-mkvmerge.sh darwin/arm64 /tmp/mtx
#
# darwin/arm64, darwin/amd64: outdir/mkvmerge, outdir/libs/libQt6Core.6.dylib
# windows/amd64:              outdir/mkvmerge.exe
# both: outdir/MKVTOOLNIX-NOTICE.txt, MKVTOOLNIX-COPYING.txt, MKVTOOLNIX-LICENSES/
#
# MKVTOOLNIX_CACHE=dir uses (and fills) dir instead of a temporary folder, so
# files already there aren't downloaded again; MKVTOOLNIX_BASE_URL overrides
# https://mkvtoolnix.download.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 os/arch outdir" >&2
	exit 2
fi
target=$1
out=$2
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=third_party/mkvtoolnix.env
source "$root/third_party/mkvtoolnix.env"
v=$MKVTOOLNIX_VERSION
base=${MKVTOOLNIX_BASE_URL:-https://mkvtoolnix.download}

case $target in
darwin/arm64)
	file="MKVToolNix-$v-$MKVTOOLNIX_MACOS_BUILD-arm64.dmg"
	url="$base/macos/releases/$v/$file"
	sum=$MKVTOOLNIX_SHA256_MACOS_ARM64
	;;
darwin/amd64)
	file="MKVToolNix-$v-$MKVTOOLNIX_MACOS_BUILD-x86_64.dmg"
	url="$base/macos/releases/$v/$file"
	sum=$MKVTOOLNIX_SHA256_MACOS_X86_64
	;;
windows/amd64)
	file="mkvtoolnix-64-bit-$v.7z"
	url="$base/windows/releases/$v/$file"
	sum=$MKVTOOLNIX_SHA256_WINDOWS_64
	;;
*)
	echo "mkvmerge is not bundled for $target" >&2
	exit 2
	;;
esac
src="mkvtoolnix-$v.tar.xz"

work=$(mktemp -d)
mnt=""
cleanup() {
	if [[ -n $mnt ]]; then hdiutil detach "$mnt" -quiet || true; fi
	rm -rf "$work"
}
trap cleanup EXIT
cache=${MKVTOOLNIX_CACHE:-$work/cache}
mkdir -p "$cache" "$out"

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

# fetch downloads url into the cache (unless it's already there) and checks
# its SHA-256.
fetch() {
	local url=$1 name=$2 want=$3 got
	if [[ ! -f $cache/$name ]]; then
		echo "downloading $name" >&2
		curl -fsSL -o "$cache/$name.part" "$url"
		mv "$cache/$name.part" "$cache/$name"
	fi
	got=$(sha256 "$cache/$name")
	if [[ $got != "$want" ]]; then
		echo "checksum mismatch for $name: got $got, want $want" >&2
		return 1
	fi
}

fetch "$url" "$file" "$sum"
fetch "$base/sources/$src" "$src" "$MKVTOOLNIX_SHA256_SOURCE"

qt=""
case $target in
darwin/*)
	mnt="$work/mnt"
	mkdir -p "$mnt"
	hdiutil attach -nobrowse -readonly -mountpoint "$mnt" "$cache/$file" >/dev/null
	bin="$mnt/MKVToolNix.app/Contents/MacOS"
	cp "$bin/mkvmerge" "$out/mkvmerge"
	mkdir -p "$out/libs"
	real=$(readlink "$bin/libs/libQt6Core.6.dylib" || basename "$bin/libs/libQt6Core.6.dylib")
	cp -L "$bin/libs/libQt6Core.6.dylib" "$out/libs/libQt6Core.6.dylib"
	qt=${real#libQt6Core.}
	qt=${qt%.dylib}
	hdiutil detach "$mnt" -quiet
	mnt=""
	;;
windows/*)
	if command -v 7z >/dev/null; then
		7z e -y -bso0 -o"$work/w" "$cache/$file" mkvtoolnix/mkvmerge.exe
	else
		mkdir -p "$work/w"
		tar -xf "$cache/$file" -C "$work/w" mkvtoolnix/mkvmerge.exe
		mv "$work/w/mkvtoolnix/mkvmerge.exe" "$work/w/mkvmerge.exe"
	fi
	cp "$work/w/mkvmerge.exe" "$out/mkvmerge.exe"
	;;
esac

tar -xJf "$cache/$src" -C "$work" "mkvtoolnix-$v/COPYING" "mkvtoolnix-$v/doc/licenses"
cp "$work/mkvtoolnix-$v/COPYING" "$out/MKVTOOLNIX-COPYING.txt"
rm -rf "$out/MKVTOOLNIX-LICENSES"
cp -R "$work/mkvtoolnix-$v/doc/licenses" "$out/MKVTOOLNIX-LICENSES"
cp "$root/third_party/licenses/GPL-3.0.txt" "$out/MKVTOOLNIX-LICENSES/GPL-3.0.txt"

{
	echo "This download includes mkvmerge from MKVToolNix $v"
	echo "(https://mkvtoolnix.download/), (c) Moritz Bunkus and contributors,"
	echo "licensed under the GNU General Public License version 2 (MKVTOOLNIX-COPYING.txt)."
	if [[ -n $qt ]]; then
		echo
		echo "On macOS it is accompanied by the Qt $qt Core library (libQt6Core),"
		echo "licensed under the GNU Lesser General Public License version 3"
		echo "(MKVTOOLNIX-LICENSES/LGPL-3.0.txt and GPL-3.0.txt). It is dynamically linked:"
		echo "you may replace Zenvik.app/Contents/Helpers/libs/libQt6Core.6.dylib with your own build."
		echo "Qt source: https://download.qt.io/archive/qt/${qt%.*}/$qt/submodules/qtbase-everywhere-src-$qt.tar.xz"
	fi
	echo
	echo "The files are unmodified from the official MKVToolNix $v release, except that"
	echo "on macOS they are re-signed with Zenvik's Developer ID."
	echo
	echo "Source for MKVToolNix $v:"
	echo "https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-$v/mkvtoolnix-$v.tar.xz"
	echo
	echo "Licenses of the libraries built into mkvmerge: MKVTOOLNIX-LICENSES/."
} >"$out/MKVTOOLNIX-NOTICE.txt"
echo "fetched mkvmerge $v for $target into $out" >&2
```

```bash
chmod 755 scripts/fetch-mkvmerge.sh
```

- [ ] **Step 5: Run the tests, offline and with the network**

Run:

```bash
shellcheck -x scripts/fetch-mkvmerge.sh scripts/fetch-mkvmerge_test.sh
scripts/fetch-mkvmerge_test.sh
ZENVIK_NET_TESTS=1 scripts/fetch-mkvmerge_test.sh
hdiutil info | grep -c MKVToolNix
```

Expected:
- `shellcheck` is silent.
- The offline run reports `3 passed, 0 failed`.
- The network run reports `7 passed, 0 failed`.
- `hdiutil info` prints `0`, so no DMG was left attached.

- [ ] **Step 6: Write Plan decisions 1–3 into the spec**

In the spec:
- **§2:** say the license files come from the pinned source tarball (`COPYING`, `doc/licenses/`). Name the folder `MKVTOOLNIX-LICENSES/` (which includes `LGPL-3.0.txt`, and `GPL-3.0.txt` from `third_party/licenses/`), replacing the two `QT-*.txt` files.
- **§3:** the same, for what `fetch-mkvmerge.sh` writes.
- **§4:** the same, for the zip contents.
- **§5:**
  - The bundled MKVToolNix version comes from `-X main.mkvtoolnixVersion`.
  - `FindMkvmerge` returns the version.
  - A copy found through the config or `PATH` shows its version as `X.Y.Z`.

- [ ] **Step 7: Commit**

```bash
git add third_party scripts/fetch-mkvmerge.sh scripts/fetch-mkvmerge_test.sh docs/superpowers/specs/2026-10-04-zenvik-bundle-mkvmerge-design.md
git commit -m "Pin MKVToolNix 102.0 and add fetch-mkvmerge.sh, which downloads, verifies and extracts the bundled mkvmerge and its licenses"
```

---

### Task 2: `bump-mkvtoolnix.sh`

**Files:**
- Create: `scripts/bump-mkvtoolnix.sh`

**Interfaces:**
- Produces `scripts/bump-mkvtoolnix.sh <version> [<macos-build>] [--publish]`.
  - It rewrites `third_party/mkvtoolnix.env` from upstream's `.sha256` files.
  - With `--publish`, it downloads and checks the source tarball, then creates the GitHub release `mkvtoolnix-src-<version>`.
  - Without it, it prints the release command instead.

- [ ] **Step 1: Show the gap (red)**

Run: `scripts/bump-mkvtoolnix.sh 102.0 2; echo "exit $?"`
Expected: `No such file or directory`, then `exit 127`.

- [ ] **Step 2: Write `scripts/bump-mkvtoolnix.sh`**

```bash
#!/usr/bin/env bash
# Point the bundled mkvmerge at another MKVToolNix release: rewrite
# third_party/mkvtoolnix.env from upstream's .sha256 files, and (with
# --publish, which creates a public GitHub release) host that version's
# source as release mkvtoolnix-src-<version>.
#
#   scripts/bump-mkvtoolnix.sh 103.0 1
#   scripts/bump-mkvtoolnix.sh 103.0 1 --publish
set -euo pipefail

publish=0
args=()
for a in "$@"; do
	if [[ $a == --publish ]]; then publish=1; else args+=("$a"); fi
done
if [[ ${#args[@]} -lt 1 || ${#args[@]} -gt 2 ]]; then
	echo "usage: $0 <version> [<macos-build>] [--publish]" >&2
	exit 2
fi
v=${args[0]}
build=${args[1]:-1}
root=$(cd "$(dirname "$0")/.." && pwd)
base=${MKVTOOLNIX_BASE_URL:-https://mkvtoolnix.download}

# upstream_sum prints the SHA-256 from upstream's .sha256 file for url.
upstream_sum() {
	local s
	s=$(curl -fsSL "$1.sha256" | cut -d' ' -f1)
	if [[ ! $s =~ ^[0-9a-f]{64}$ ]]; then
		echo "no SHA-256 at $1.sha256" >&2
		return 1
	fi
	echo "$s"
}

arm=$(upstream_sum "$base/macos/releases/$v/MKVToolNix-$v-$build-arm64.dmg")
x86=$(upstream_sum "$base/macos/releases/$v/MKVToolNix-$v-$build-x86_64.dmg")
win=$(upstream_sum "$base/windows/releases/$v/mkvtoolnix-64-bit-$v.7z")
src=$(upstream_sum "$base/sources/mkvtoolnix-$v.tar.xz")

cat >"$root/third_party/mkvtoolnix.env" <<EOF
# MKVToolNix release whose mkvmerge the desktop app bundles (macOS, Windows).
# Update with scripts/bump-mkvtoolnix.sh; the checksums are upstream's own
# .sha256 values for each file.
MKVTOOLNIX_VERSION=$v
MKVTOOLNIX_MACOS_BUILD=$build
MKVTOOLNIX_SHA256_MACOS_ARM64=$arm
MKVTOOLNIX_SHA256_MACOS_X86_64=$x86
MKVTOOLNIX_SHA256_WINDOWS_64=$win
MKVTOOLNIX_SHA256_SOURCE=$src
EOF
echo "wrote third_party/mkvtoolnix.env for MKVToolNix $v (macOS build $build)" >&2

tag="mkvtoolnix-src-$v"
tarball="mkvtoolnix-$v.tar.xz"
notes="Source of MKVToolNix $v, whose mkvmerge is bundled with the Zenvik desktop app (GPLv2). Unmodified from $base/sources/$tarball."
if [[ $publish != 1 ]]; then
	echo "next, host the source (creates a public release; run with --publish to do it here):" >&2
	echo "  gh release create $tag $tarball $tarball.sha256 --title 'MKVToolNix $v source' --notes '$notes' --latest=false" >&2
	exit 0
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL -o "$work/$tarball" "$base/sources/$tarball"
got=$(if command -v sha256sum >/dev/null; then sha256sum "$work/$tarball"; else shasum -a 256 "$work/$tarball"; fi | cut -d' ' -f1)
if [[ $got != "$src" ]]; then
	echo "checksum mismatch for $tarball: got $got, want $src" >&2
	exit 1
fi
printf '%s  %s\n' "$src" "$tarball" >"$work/$tarball.sha256"
(cd "$work" && gh release create "$tag" "$tarball" "$tarball.sha256" \
	--repo chad3814/zenvik --title "MKVToolNix $v source" --notes "$notes" --latest=false)
```

```bash
chmod 755 scripts/bump-mkvtoolnix.sh
```

- [ ] **Step 3: Check it against the current pin (green)**

Run:

```bash
shellcheck -x scripts/bump-mkvtoolnix.sh
cp third_party/mkvtoolnix.env /tmp/pin.before
scripts/bump-mkvtoolnix.sh 102.0 2
diff /tmp/pin.before third_party/mkvtoolnix.env && echo "pin unchanged"
scripts/bump-mkvtoolnix.sh; echo "exit $?"
rm /tmp/pin.before
```

Expected:
- `shellcheck` is silent.
- The bump prints the `gh release create mkvtoolnix-src-102.0 …` line without running it.
- `diff` prints nothing, then `pin unchanged`.
- Running with no arguments prints the usage line and `exit 2`.

**Do not** run `--publish` here. Task 7 does that, with the user's approval.

- [ ] **Step 4: Commit**

```bash
git add scripts/bump-mkvtoolnix.sh
git commit -m "Add bump-mkvtoolnix.sh to repin the bundled MKVToolNix and, on request, host its source as a release"
```

---

### Task 3: Sign bundled helpers in `macos-sign.sh`

**Files:**
- Modify: `scripts/macos-sign.sh` (`macos_sign`, `macos_verify_signature`), `scripts/macos-sign_test.sh`

**Interfaces:**
- `macos_sign <app>` signs inside out:
  1. `Contents/Helpers/libs/*.dylib`;
  2. the executable files directly in `Contents/Helpers/`;
  3. `Contents/MacOS/*`;
  4. the bundle.
- `macos_verify_signature <app>` also verifies every helper file and checks it carries the Developer ID authority.

- [ ] **Step 1: Write the failing test**

In `scripts/macos-sign_test.sh`, inside the `if [[ -n ${MACOS_SIGN_IDENTITY:-} ]]; then` block, after the "tampered binary" case and before `rm -rf "$work"`, add:

```bash
		app="$work/Fake.app"
		mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers/libs"
		cp /usr/bin/true "$app/Contents/MacOS/fake"
		cp /usr/bin/true "$app/Contents/Helpers/tool"
		printf 'int zq(void) { return 1; }\n' >"$work/zq.c"
		cc -dynamiclib -o "$app/Contents/Helpers/libs/libzq.dylib" "$work/zq.c"
		cat >"$app/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleExecutable</key><string>fake</string><key>CFBundleIdentifier</key><string>dev.cwalker.zenvik.test</string></dict></plist>
EOF
		run_case "app helpers and their libraries are signed with the app's identity" 0 '' -- MACOS_SIGN_IDENTITY="$MACOS_SIGN_IDENTITY" -- \
			"macos_sign_setup; macos_sign '$app'; macos_verify_signature '$app'
			for f in '$app/Contents/Helpers/tool' '$app/Contents/Helpers/libs/libzq.dylib'; do
				codesign -dvv \"\$f\" 2>&1 | grep -qF \"Authority=\$MACOS_SIGN_IDENTITY\"
				codesign -dvv \"\$f\" 2>&1 | grep -q 'flags=.*runtime'
			done; macos_sign_cleanup"
```

Run: `MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/macos-sign_test.sh`
Expected: FAIL on "app helpers and their libraries are signed with the app's identity". Today the helpers are left unsigned, so `codesign --verify` of the bundle fails.

- [ ] **Step 2: Sign and verify the helpers**

In `scripts/macos-sign.sh`, replace the `if [[ -d $path && $path == *.app ]]; then … fi` block inside `macos_sign` with:

```bash
	if [[ -d $path && $path == *.app ]]; then
		# Inside out: bundled libraries, then helper tools, then the app's own
		# executable; the bundle last. Helpers and their libraries must carry
		# the same team ID or library validation stops them loading.
		local f
		if [[ -d $path/Contents/Helpers/libs ]]; then
			while IFS= read -r f; do
				codesign --force --options runtime --timestamp ${kc[@]+"${kc[@]}"} \
					--sign "$MACOS_SIGN_IDENTITY" "$f"
			done < <(find "$path/Contents/Helpers/libs" -type f -name '*.dylib')
		fi
		for f in "$path"/Contents/Helpers/* "$path"/Contents/MacOS/*; do
			if [[ -f $f && -x $f ]]; then
				codesign --force --options runtime --timestamp ${kc[@]+"${kc[@]}"} \
					--sign "$MACOS_SIGN_IDENTITY" "$f"
			fi
		done
	fi
```

In `macos_verify_signature`, after the existing authority check (before the closing `}`), add:

```bash
	if [[ -d $path && $path == *.app && -d $path/Contents/Helpers ]]; then
		local f
		while IFS= read -r f; do
			codesign --verify --strict --verbose=2 "$f"
			if ! codesign -dvv "$f" 2>&1 | grep -qF "Authority=$MACOS_SIGN_IDENTITY"; then
				_ms_die "$f is not signed by $MACOS_SIGN_IDENTITY"
				return 1
			fi
		done < <(find "$path/Contents/Helpers" -type f \( -name '*.dylib' -o -perm -u+x \))
	fi
```

- [ ] **Step 3: Run the tests**

Run:

```bash
shellcheck -x scripts/macos-sign.sh scripts/macos-sign_test.sh
scripts/macos-sign_test.sh
MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/macos-sign_test.sh
```

Expected: `shellcheck` is silent, then `16 passed, 0 failed` and `19 passed, 0 failed`.

- [ ] **Step 4: Commit**

```bash
git add scripts/macos-sign.sh scripts/macos-sign_test.sh
git commit -m "macos-sign.sh: sign an app's bundled helpers and their libraries inside out with the same identity"
```

---

### Task 4: Find the bundled mkvmerge in the app, and report it in About

**Files:**
- Create: `gui/mkvtoolnix.go`, `gui/mkvtoolnix_test.go`
- Modify: `gui/app.go`, `gui/settings.go`, `gui/app_test.go`, `gui/frontend/src/api.ts`, `gui/frontend/src/components/About.tsx`, `gui/frontend/src/__tests__/App.test.tsx`
- Regenerate: `gui/frontend/wailsjs/go/main/App.{js,d.ts}`

**Interfaces:**
- Produces:
  - `var mkvtoolnixVersion string`, set by `-X main.mkvtoolnixVersion`;
  - `resolveMkvmerge(configured, exe, goos string, exists func(string) bool) (path, source string)`, where `source` is one of `config`, `bundled` or `path`;
  - `mkvmergeSourceURL(version string) string`;
  - `mkvmergeInfo(s mkvmergeState, bundledVersion string) string`;
  - `type mkvmergeState struct{ source, path, version string; err error }`;
  - `Deps.Executable string`;
  - `Deps.FindMkvmerge func(ctx context.Context, path string) (string, error)`, which returns the version;
  - bound methods `MkvmergeInfo() string` and `MkvmergeSourceURL() string`.

- [ ] **Step 1: Write the failing Go tests**

`gui/mkvtoolnix_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMkvmerge(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Zen vik.app", "Contents")
	exe := filepath.Join(app, "MacOS", "zenvik-gui")
	bundled := filepath.Join(app, "Helpers", "mkvmerge")
	winExe := filepath.Join(dir, "zen vik", "zenvik-gui.exe")
	winBundled := filepath.Join(dir, "zen vik", "mkvmerge.exe")
	for _, f := range []string{bundled, winBundled} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, configured, exe, goos, wantPath, wantSource string
	}{
		{"config wins", "/opt/mkv/mkvmerge", exe, "darwin", "/opt/mkv/mkvmerge", "config"},
		{"bundled on darwin", "", exe, "darwin", bundled, "bundled"},
		{"bundled on windows", "", winExe, "windows", winBundled, "bundled"},
		{"nothing bundled on linux", "", filepath.Join(dir, "zenvik-gui"), "linux", "", "path"},
		{"no bundled copy beside a dev build", "", filepath.Join(dir, "bin", "zenvik-gui"), "darwin", "", "path"},
		{"unknown executable", "", "", "darwin", "", "path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, source := resolveMkvmerge(c.configured, c.exe, c.goos, fileExists)
			if path != c.wantPath || source != c.wantSource {
				t.Errorf("got %q, %q; want %q, %q", path, source, c.wantPath, c.wantSource)
			}
		})
	}
}

func TestMkvmergeSourceURL(t *testing.T) {
	want := "https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz"
	if got := mkvmergeSourceURL("102.0"); got != want {
		t.Errorf("got %q", got)
	}
}

func TestMkvmergeInfo(t *testing.T) {
	cases := []struct {
		s       mkvmergeState
		bundled string
		want    string
	}{
		{mkvmergeState{source: "bundled", path: "/A/mkvmerge", version: "102.0.0"}, "102.0", "mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)"},
		{mkvmergeState{source: "config", path: "/opt/mkvmerge", version: "101.0.0"}, "102.0", "mkvmerge 101.0.0 (/opt/mkvmerge)"},
		{mkvmergeState{source: "path", version: "100.0.0"}, "", "mkvmerge 100.0.0 (from PATH)"},
		{mkvmergeState{source: "path", err: os.ErrNotExist}, "", "mkvmerge not available — see the message at the top of the window"},
	}
	for _, c := range cases {
		if got := mkvmergeInfo(c.s, c.bundled); got != c.want {
			t.Errorf("mkvmergeInfo(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}
```

In `gui/app_test.go`:
- Change every `FindMkvmerge: func(context.Context, string) error { return X }` to `FindMkvmerge: func(context.Context, string) (string, error) { return "102.0.0", X }`. The same goes for the assignment at line 180.
- Change `mkvmergeAt` to:

```go
// mkvmergeAt fakes FindMkvmerge: only good finds mkvmerge.
func mkvmergeAt(good string) func(context.Context, string) (string, error) {
	return func(_ context.Context, path string) (string, error) {
		if path == good {
			return "102.0.0", nil
		}
		return "", fmt.Errorf("%w: %s", zenvik.ErrMkvmergeNotFound, path)
	}
}
```

Append these tests:

```go
func TestBundledMkvmergeIsUsedAndReported(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{})
	exeDir := filepath.Join(t.TempDir(), "Zenvik.app", "Contents")
	bundled := filepath.Join(exeDir, "Helpers", "mkvmerge")
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	a.deps.Executable = filepath.Join(exeDir, "MacOS", "zenvik-gui")
	a.deps.GOOS = "darwin"
	var checked string
	a.deps.FindMkvmerge = func(_ context.Context, p string) (string, error) { checked = p; return "102.0.0", nil }
	old := mkvtoolnixVersion
	mkvtoolnixVersion = "102.0"
	t.Cleanup(func() { mkvtoolnixVersion = old })

	a.RecheckMkvmerge()
	if checked != bundled || a.mkvmergePath() != bundled {
		t.Errorf("checked %q, rip path %q; want %q", checked, a.mkvmergePath(), bundled)
	}
	if got := a.MkvmergeInfo(); got != "mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)" {
		t.Errorf("info = %q", got)
	}
	if got := a.MkvmergeSourceURL(); got != mkvmergeSourceURL("102.0") {
		t.Errorf("source URL = %q", got)
	}
}

func TestBrokenConfiguredMkvmergeDoesNotFallBack(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{})
	exeDir := filepath.Join(t.TempDir(), "Zenvik.app", "Contents")
	bundled := filepath.Join(exeDir, "Helpers", "mkvmerge")
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	a.deps.Executable = filepath.Join(exeDir, "MacOS", "zenvik-gui")
	a.deps.GOOS = "darwin"
	a.deps.LoadSettings = func() (config.Settings, error) {
		return config.Settings{OutputDir: ".", Template: config.DefaultTemplate, MkvmergePath: "/broken/mkvmerge"}, nil
	}
	a.deps.FindMkvmerge = mkvmergeAt(bundled) // only the bundled copy works
	a.ReloadConfig()
	if _, ok := banners(sh)["mkvmerge"]; !ok || a.queue.Snapshot().Ready {
		t.Errorf("a broken mkvmerge_path must raise the banner and keep the queue stopped; banners %+v", banners(sh))
	}
	if a.mkvmergePath() != "/broken/mkvmerge" || a.MkvmergeSourceURL() != "" {
		t.Errorf("path %q, source URL %q", a.mkvmergePath(), a.MkvmergeSourceURL())
	}
}
```

These need `os`, `path/filepath` and `config` imported in `app_test.go`; add whichever are missing.

Run: `cd gui && go test ./... 2>&1 | head`
Expected: the build fails with `undefined: resolveMkvmerge`, `undefined: mkvmergeInfo` and similar, plus type errors on `FindMkvmerge`.

- [ ] **Step 2: Implement it**

`gui/mkvtoolnix.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// mkvtoolnixVersion is the MKVToolNix release whose mkvmerge this build
// bundles, set with -ldflags "-X main.mkvtoolnixVersion=<ver>" by
// scripts/release-gui.sh on macOS and Windows; empty when nothing is bundled.
var mkvtoolnixVersion = ""

// resolveMkvmerge picks the mkvmerge to run: the config's mkvmerge_path when
// set (even if it's broken — no silent fallback), else the copy bundled beside
// the app's executable exe, else "" for a PATH lookup.
func resolveMkvmerge(configured, exe, goos string, exists func(string) bool) (path, source string) {
	if configured != "" {
		return configured, "config"
	}
	if exe != "" {
		var bundled string
		switch goos {
		case "darwin":
			bundled = filepath.Join(filepath.Dir(exe), "..", "Helpers", "mkvmerge")
		case "windows":
			bundled = filepath.Join(filepath.Dir(exe), "mkvmerge.exe")
		}
		if bundled != "" && exists(bundled) {
			return filepath.Clean(bundled), "bundled"
		}
	}
	return "", "path"
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// mkvmergeSourceURL is where zenvik hosts the source of the MKVToolNix
// release it bundles (GPLv2).
func mkvmergeSourceURL(version string) string {
	return fmt.Sprintf("https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-%s/mkvtoolnix-%s.tar.xz", version, version)
}

// mkvmergeState is the result of the last mkvmerge check.
type mkvmergeState struct {
	source, path, version string
	err                   error
}

// mkvmergeInfo is the About box's mkvmerge line.
func mkvmergeInfo(s mkvmergeState, bundledVersion string) string {
	switch {
	case s.err != nil:
		return "mkvmerge not available — see the message at the top of the window"
	case s.source == "bundled":
		v := bundledVersion
		if v == "" {
			v = s.version
		}
		return fmt.Sprintf("mkvmerge %s (bundled, from MKVToolNix — GPLv2)", v)
	case s.source == "config":
		return fmt.Sprintf("mkvmerge %s (%s)", s.version, s.path)
	}
	return fmt.Sprintf("mkvmerge %s (from PATH)", s.version)
}
```

In `gui/app.go`:
- **`Deps`:** change `FindMkvmerge func(ctx context.Context, path string) error` to `FindMkvmerge func(ctx context.Context, path string) (string, error)`, and add `Executable string // the app's own executable, symlinks resolved`.
- **`App`:** add `mkvmerge mkvmergeState` beside `settings`. It is guarded by `a.mu`.
- **`mkvmergePath` and `recheckMkvmerge`:** replace them with:

```go
// resolvedMkvmerge is the mkvmerge the app uses and where it comes from.
func (a *App) resolvedMkvmerge() (path, source string) {
	a.mu.Lock()
	configured := a.settings.MkvmergePath
	a.mu.Unlock()
	return resolveMkvmerge(configured, a.deps.Executable, a.deps.GOOS, fileExists)
}

func (a *App) mkvmergePath() string {
	path, _ := a.resolvedMkvmerge()
	return path
}

func (a *App) recheckMkvmerge() {
	path, source := a.resolvedMkvmerge()
	version, err := a.deps.FindMkvmerge(a.ctx, path)
	a.mu.Lock()
	a.mkvmerge = mkvmergeState{source: source, path: path, version: version, err: err}
	a.mu.Unlock()
	if err == nil {
		a.clearBanner("mkvmerge")
	} else {
		a.setBanner(mkvmergeBanner(err))
	}
	a.queue.SetReady(err == nil)
}
```

- **New bound methods**, next to `Platform`:

```go
// MkvmergeInfo is the About box's line about the mkvmerge in use.
func (a *App) MkvmergeInfo() string {
	if !a.wait() {
		return ""
	}
	a.mu.Lock()
	s := a.mkvmerge
	a.mu.Unlock()
	return mkvmergeInfo(s, mkvtoolnixVersion)
}

// MkvmergeSourceURL links the bundled MKVToolNix's source, or "" when the
// mkvmerge in use isn't the bundled one.
func (a *App) MkvmergeSourceURL() string {
	if !a.wait() {
		return ""
	}
	a.mu.Lock()
	s := a.mkvmerge
	a.mu.Unlock()
	if s.err != nil || s.source != "bundled" || mkvtoolnixVersion == "" {
		return ""
	}
	return mkvmergeSourceURL(mkvtoolnixVersion)
}
```

In `gui/settings.go`, replace `findMkvmerge` with:

```go
func findMkvmerge(ctx context.Context, path string) (string, error) {
	m, err := mux.Find(ctx, path)
	if err != nil {
		return "", err
	}
	return m.Version.String(), nil
}
```

In `defaultDeps`, before `return Deps{`, add:

```go
	exe, err := os.Executable()
	if err == nil {
		if real, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = real
		}
	} else {
		exe = ""
	}
```

Then add `Executable: exe,` to the returned `Deps`.

Run:

```bash
cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/) 2>&1 | tail -4
go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 generate module
grep -E 'MkvmergeInfo|MkvmergeSourceURL' frontend/wailsjs/go/main/App.d.ts
git -C .. status --short frontend/wailsjs/runtime
```

Expected:
- Every package is `ok`.
- `App.d.ts` declares `MkvmergeInfo():Promise<string>` and `MkvmergeSourceURL():Promise<string>`.
- Nothing under `wailsjs/runtime` changed. Revert it if Wails rewrote it.

- [ ] **Step 3: Write the failing frontend test**

In `gui/frontend/src/__tests__/App.test.tsx`'s `vi.mock('../api', …)` `api` object, add:

```ts
    mkvmergeInfo: vi.fn(() => Promise.resolve('mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)')),
    mkvmergeSourceURL: vi.fn(() =>
      Promise.resolve('https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz'),
    ),
```

Then append inside the `describe('App', …)` block:

```ts
  it('shows the bundled mkvmerge and links its source in About', async () => {
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    expect(await screen.findByText('mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'MKVToolNix source' }));
    expect(api.openURL).toHaveBeenCalledWith(
      'https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz',
    );
  });

  it('shows no source link when mkvmerge is not bundled', async () => {
    vi.mocked(api.mkvmergeInfo).mockResolvedValueOnce('mkvmerge 101.0.0 (from PATH)');
    vi.mocked(api.mkvmergeSourceURL).mockResolvedValueOnce('');
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    expect(await screen.findByText('mkvmerge 101.0.0 (from PATH)')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'MKVToolNix source' })).not.toBeInTheDocument();
  });
```

Run: `cd gui/frontend && npm test 2>&1 | tail -5`
Expected: both new tests FAIL. `api.mkvmergeInfo` isn't part of `api` yet, and About shows no such line.

- [ ] **Step 4: Implement the frontend**

In `gui/frontend/src/api.ts`, add to `api` after `platform`:

```ts
  mkvmergeInfo: (): Promise<string> => App.MkvmergeInfo(),
  mkvmergeSourceURL: (): Promise<string> => App.MkvmergeSourceURL(),
```

Replace `gui/frontend/src/components/About.tsx` with:

```tsx
import { useState } from 'react';
import { api } from '../api';

const repoURL = 'https://github.com/chad3814/zenvik';

interface Info {
  version: string;
  mkvmerge: string;
  sourceURL: string;
}

export function About() {
  const [info, setInfo] = useState<Info | null>(null);
  const [open, setOpen] = useState(false);
  const show = async () => {
    setOpen(true);
    const [version, mkvmerge, sourceURL] = await Promise.all([
      api.version(),
      api.mkvmergeInfo(),
      api.mkvmergeSourceURL(),
    ]);
    setInfo({ version, mkvmerge, sourceURL });
  };
  const link = (url: string, text: string) => (
    <a
      href={url}
      onClick={(e) => {
        e.preventDefault();
        api.openURL(url);
      }}
    >
      {text}
    </a>
  );
  return (
    <>
      <button onClick={() => void show()}>About</button>
      {open && (
        <div role="dialog" aria-label="About Zenvik" className="dialog">
          <p><strong>Zenvik {info?.version ?? ''}</strong></p>
          <p>Rips unencrypted Blu-ray and DVD images to MKV with mkvmerge.</p>
          {info?.mkvmerge && (
            <p>
              {info.mkvmerge}
              {info.sourceURL && <> · {link(info.sourceURL, 'MKVToolNix source')}</>}
            </p>
          )}
          <p>{link(repoURL, 'github.com/chad3814/zenvik')}</p>
          <button onClick={() => setOpen(false)}>Close</button>
        </div>
      )}
    </>
  );
}
```

Run:

```bash
cd gui/frontend && npm test 2>&1 | grep -E 'Tests ' && npm run lint && npm run build >/dev/null && echo build-ok
git -C ../.. checkout gui/frontend/dist/gitkeep 2>/dev/null; true
```

Expected:
- All frontend tests pass, including the two new ones and the existing About tests.
- Lint is silent.
- `build-ok` prints.

- [ ] **Step 5: Commit**

```bash
cd /Users/chad/Projects/zenvik/worktrees/bundle-mkvmerge
git add gui
git commit -m "GUI: prefer mkvmerge_path, then the bundled mkvmerge, then PATH, and show which one in About with a source link"
```

---

### Task 5: Bundle mkvmerge in `release-gui.sh`

**Files:**
- Modify: `scripts/release-gui.sh`

**Interfaces:**
- Consumes: `fetch-mkvmerge.sh` (Task 1), the helper signing (Task 3), and `-X main.mkvtoolnixVersion` (Task 4).

- [ ] **Step 1: Show the gap (red)**

Run:

```bash
scripts/release-gui.sh v0.0.0-test darwin/arm64 >/dev/null 2>&1
unzip -l dist/zenvik-gui_0.0.0-test_darwin_arm64.zip | grep -cE 'Helpers/mkvmerge|MKVTOOLNIX-NOTICE'
rm -rf dist
```

Expected: `0`.

- [ ] **Step 2: Change `release-gui.sh`**

After the `tags=()` / `[[ $goos == linux ]] && tags=(…)` lines, add:

```bash
# The desktop app bundles MKVToolNix's mkvmerge on macOS and Windows (see
# third_party/mkvtoolnix.env); the app learns which release from this flag.
bundle=0
ldextra=""
if [[ $goos == darwin || $goos == windows ]]; then
	bundle=1
	# shellcheck source=third_party/mkvtoolnix.env
	source "$root/third_party/mkvtoolnix.env"
	ldextra=" -X main.mkvtoolnixVersion=$MKVTOOLNIX_VERSION"
fi
```

In the `wails build` command, change `-ldflags "-s -w -X main.version=$tag"` to `-ldflags "-s -w -X main.version=$tag$ldextra"`.

After the `wails build` command (before `bin="$root/gui/build/bin"`), add:

```bash
mtx=""
if [[ $bundle == 1 ]]; then
	mtx=$(mktemp -d)
	"$root/scripts/fetch-mkvmerge.sh" "$target" "$mtx"
fi
```

Also add `rm -rf "${mtx:-}"` as the first line of the `restore()` function. `mtx` must therefore be declared before `trap restore EXIT`: add `mtx=""` just before the `restore() {` line, and keep the assignment above as it is.

In the `darwin)` case, change the first line from `app="$bin/Zenvik.app"` to:

```bash
	app="$bin/Zenvik.app"
	mkdir -p "$app/Contents/Helpers/libs"
	cp "$mtx/mkvmerge" "$app/Contents/Helpers/mkvmerge"
	cp "$mtx/libs/libQt6Core.6.dylib" "$app/Contents/Helpers/libs/libQt6Core.6.dylib"
	check_mkvmerge() {
		local out
		out=$("$app/Contents/Helpers/mkvmerge" --version 2>&1) || true
		if [[ $out != "mkvmerge v$MKVTOOLNIX_VERSION"* ]]; then
			echo "the bundled mkvmerge doesn't run ($1): $out" >&2
			exit 1
		fi
	}
	check_mkvmerge "as fetched"
```

Inside its `if [[ $MACOS_SIGN_ENABLED == 1 ]]; then` block:
- after the first `macos_verify_signature "$app"`, add `check_mkvmerge "after signing"`;
- after `macos_staple "$app"`, add `check_mkvmerge "after stapling"`.

After `cp -R "$app" "$stage/"`, add:

```bash
	cp "$mtx"/MKVTOOLNIX-*.txt "$stage/"
	cp -R "$mtx/MKVTOOLNIX-LICENSES" "$stage/"
```

In the `windows)` case, after `cp "$bin/zenvik-gui.exe" "$stage/"`, add:

```bash
	cp "$mtx/mkvmerge.exe" "$stage/"
	cp "$mtx"/MKVTOOLNIX-*.txt "$stage/"
	cp -R "$mtx/MKVTOOLNIX-LICENSES" "$stage/"
	if [[ $(uname -s) == MINGW* || $(uname -s) == MSYS* ]]; then
		out=$("$stage/mkvmerge.exe" --version 2>&1) || true
		if [[ $out != "mkvmerge v$MKVTOOLNIX_VERSION"* ]]; then
			echo "the bundled mkvmerge.exe doesn't run: $out" >&2
			exit 1
		fi
	fi
```

Update the header comment: the darwin and windows zips include MKVToolNix's `mkvmerge` (see `scripts/fetch-mkvmerge.sh`).

- [ ] **Step 3: Check it locally (green)**

Run:

```bash
shellcheck -x scripts/release-gui.sh
MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-gui.sh v0.0.0-test darwin/arm64 > /tmp/zg.log 2>&1; echo "exit $?"
grep -E "bundled mkvmerge|fetched mkvmerge" /tmp/zg.log
unzip -l dist/zenvik-gui_0.0.0-test_darwin_arm64.zip | grep -E 'Helpers/|MKVTOOLNIX' | awk '{print $4}'
mkdir -p /tmp/zg && unzip -q -o dist/zenvik-gui_0.0.0-test_darwin_arm64.zip -d /tmp/zg
A=/tmp/zg/zenvik-gui_0.0.0-test_darwin_arm64/Zenvik.app
codesign --verify --strict --deep --verbose=2 "$A" 2>&1 | tail -1
for f in "$A/Contents/Helpers/mkvmerge" "$A/Contents/Helpers/libs/libQt6Core.6.dylib"; do codesign -dvv "$f" 2>&1 | grep -E 'Authority=Developer ID Application|TeamIdentifier'; done
"$A/Contents/Helpers/mkvmerge" --version
strings "$A/Contents/MacOS/zenvik-gui" | grep -c 'mkvtoolnix-src-%s' 
git status --short
rm -rf /tmp/zg /tmp/zg.log dist
```

Expected:
- `shellcheck` is silent, and the build reports `exit 0`.
- The log shows `fetched mkvmerge 102.0 for darwin/arm64`.
- The zip lists `Zenvik.app/Contents/Helpers/mkvmerge`, `Contents/Helpers/libs/libQt6Core.6.dylib`, `MKVTOOLNIX-NOTICE.txt`, `MKVTOOLNIX-COPYING.txt` and the `MKVTOOLNIX-LICENSES/…` files.
- `codesign --verify` reports `satisfies its Designated Requirement`.
- Both helper files show `Authority=Developer ID Application: Chad Walker (SZUN8RDF5D)` and `TeamIdentifier=SZUN8RDF5D`.
- The helper prints `mkvmerge v102.0 ('Little Houses') 64-bit`.
- The `strings` count is `1`.
- `git status` is clean.

Then check that `scripts/release-gui.sh v0.0.0-test linux/amd64` doesn't fetch anything. On a Mac this fails at the Wails Linux build, which is expected, so run only up to the bundle decision: `bash -c 'goos=linux; [[ $goos == darwin || $goos == windows ]] && echo bundles || echo "no bundle"'`. It prints `no bundle`.

- [ ] **Step 4: Commit**

```bash
git add scripts/release-gui.sh
git commit -m "release-gui.sh: bundle MKVToolNix's mkvmerge and its notices in the macOS and Windows apps, and prove it runs after signing"
```

---

### Task 6: Docs

**Files:**
- Modify: `gui/README.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: Update the docs**

In `gui/README.md`, replace the "Requirements:" paragraph with:

```markdown
The macOS and Windows apps include `mkvmerge` from [MKVToolNix](https://mkvtoolnix.download) (GPLv2; see `MKVTOOLNIX-NOTICE.txt` in the download for the license and source). On Linux, install MKVToolNix (mkvmerge) and WebKitGTK 4.1 (`libwebkit2gtk-4.1-0` on Debian/Ubuntu). Setting `mkvmerge_path` in the config makes the app use that mkvmerge instead of the bundled one.
```

In `README.md`, in the "Desktop app" paragraph, change `It uses your zenvik config.` to `It uses your zenvik config, and the macOS and Windows downloads include mkvmerge from MKVToolNix.`

In `CLAUDE.md`, add after the macOS signing bullet:

```markdown
- Bundled mkvmerge (desktop app, macOS and Windows): `third_party/mkvtoolnix.env` pins the MKVToolNix release and its upstream SHA-256s; `scripts/fetch-mkvmerge.sh os/arch outdir` downloads, verifies and extracts it with its license files (tests: `scripts/fetch-mkvmerge_test.sh`, add `ZENVIK_NET_TESTS=1` for real downloads); `release-gui.sh` puts it in `Zenvik.app/Contents/Helpers` (re-signed together with its Qt library — never re-sign one without the other) or beside `zenvik-gui.exe`. Repin with `scripts/bump-mkvtoolnix.sh <ver> [<macos-build>]`; its `--publish` creates the public source release `mkvtoolnix-src-<ver>`, so only run it with the user's approval.
```

- [ ] **Step 2: Verify everything and commit**

Run:

```bash
go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/*.yml && shellcheck -x scripts/*.sh && echo lint-ok
CGO_ENABLED=0 go build ./... && go test -race ./... >/dev/null && echo root-ok
(cd gui && go test -race $(go list ./... | grep -v /node_modules/) >/dev/null && echo gui-ok)
scripts/macos-sign_test.sh | tail -1; scripts/fetch-mkvmerge_test.sh | tail -1
git add gui/README.md README.md CLAUDE.md && git commit -m "Docs: the macOS and Windows desktop apps bundle mkvmerge from MKVToolNix"
```

Expected: `lint-ok`, `root-ok` and `gui-ok` print, and both test scripts report `0 failed`.

---

### Task 7: Source release and the first bundled release, `v1.1.0-rc3` (with the user)

**Files:** none. Every step here is outward-facing.

- [ ] **Step 1: Ask the user** for approval for all of the following. Do nothing until they say yes.
  1. Create the source release `mkvtoolnix-src-102.0` with `scripts/bump-mkvtoolnix.sh 102.0 2 --publish`. It must leave `third_party/mkvtoolnix.env` unchanged; check with `git diff --exit-code third_party`.
  2. Merge `feat/bundle-mkvmerge` to `main`, with all suites green on the merged result, and push.
  3. Create a signed tag `v1.1.0-rc3` and push it.

- [ ] **Step 2: Check the source release**

```bash
gh release view mkvtoolnix-src-102.0 -R chad3814/zenvik --json isPrerelease,isLatest,assets --jq '"prerelease=\(.isPrerelease) assets=\([.assets[].name]|join(","))"'
cd "$(mktemp -d)" && curl -fsSLO https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz && shasum -a 256 mkvtoolnix-102.0.tar.xz
gh release list -R chad3814/zenvik --limit 3
```

Expected:
- `prerelease=false assets=mkvtoolnix-102.0.tar.xz,mkvtoolnix-102.0.tar.xz.sha256`.
- The SHA-256 is `9f0a810f17c7df8adb9064a3a41d5784399be412d19704cf080745ad7d45da30`.
- `v1.1.0-rc2` (or rc3 later) is still listed as latest, never the source release.

- [ ] **Step 3: Watch the release run** with `gh run watch <id> --exit-status`.

Expected:
- All seven jobs succeed.
- The `gui-darwin` logs show `fetched mkvmerge 102.0` and `accepted`, and no "doesn't run" line.
- The `gui-other` Windows log shows `fetched mkvmerge 102.0 for windows/amd64`.

- [ ] **Step 4: Verify the published downloads**

```bash
cd "$(mktemp -d)" && gh release download v1.1.0-rc3 -R chad3814/zenvik -p SHA256SUMS -p 'zenvik-gui_*'
shasum -a 256 -c --ignore-missing SHA256SUMS
for a in arm64 amd64; do
  unzip -q "zenvik-gui_1.1.0-rc3_darwin_$a.zip"; A="zenvik-gui_1.1.0-rc3_darwin_$a/Zenvik.app"
  spctl -a -vv -t exec "$A" 2>&1 | tail -2; xcrun stapler validate "$A" | tail -1
  codesign --verify --strict "$A/Contents/Helpers/mkvmerge" && codesign -dvv "$A/Contents/Helpers/libs/libQt6Core.6.dylib" 2>&1 | grep TeamIdentifier
  ls "zenvik-gui_1.1.0-rc3_darwin_$a" | grep MKVTOOLNIX
done
"zenvik-gui_1.1.0-rc3_darwin_arm64/Zenvik.app/Contents/Helpers/mkvmerge" --version
unzip -l zenvik-gui_1.1.0-rc3_windows_amd64.zip | grep -E 'mkvmerge.exe|MKVTOOLNIX'
grep -o 'https://github.com/[^ ]*mkvtoolnix-102.0.tar.xz' zenvik-gui_1.1.0-rc3_darwin_arm64/MKVTOOLNIX-NOTICE.txt
```

Expected:
- Every checksum is OK.
- Both apps are `accepted` with `source=Notarized Developer ID`, and `stapler` reports `The validate action worked!`.
- Both helpers verify, with `TeamIdentifier=SZUN8RDF5D`, and each zip lists the `MKVTOOLNIX-*` files.
- The arm64 helper prints `mkvmerge v102.0`.
- The Windows zip lists `mkvmerge.exe` and the notices.
- The notice prints the source URL from Step 2.

- [ ] **Step 5: The user's check.** The user downloads the arm64 GUI zip in a browser, opens `Zenvik.app` from Finder, and rips one disc with `mkvmerge_path` unset. About shows `mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)` with a working "MKVToolNix source" link. Record the result.
