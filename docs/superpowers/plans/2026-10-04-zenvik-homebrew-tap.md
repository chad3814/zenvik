# zenvik Homebrew tap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish `chad3814/homebrew-tap` with a `zenvik` formula (built from source, depends on MKVToolNix) and a `zenvik-gui` cask, and have every final zenvik release update both after proving they install.

**Architecture:** `scripts/homebrew-render.sh` in the zenvik repo writes the formula and cask for a tag from the published release (source tarball hash plus `SHA256SUMS`). The tap repo owns `check.sh`, the single definition of "this tap works", which its own CI runs. Three new release jobs then do the bump: render, run the tap's `check.sh` on the rendered files, and push with a deploy key.

**Tech Stack:** bash (must run on macOS bash 3.2 and Linux), Homebrew (formula/cask Ruby DSL, `brew style`/`audit`/`install`/`test`), GitHub Actions, `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-10-04-zenvik-homebrew-tap-design.md`

## Global Constraints

- Tap repo: `chad3814/homebrew-tap` (public). Users run `brew install chad3814/tap/zenvik` and `brew install --cask chad3814/tap/zenvik-gui`.
- Formula `zenvik`: source `https://github.com/chad3814/zenvik/archive/refs/tags/vX.Y.Z.tar.gz`; `depends_on "go" => :build`; `depends_on "mkvtoolnix"`; ldflags `-s -w -X main.version=v#{version}`; builds `./cmd/zenvik`.
- Cask `zenvik-gui`: `https://github.com/chad3814/zenvik/releases/download/v#{version}/zenvik-gui_#{version}_darwin_#{arch}.dmg` with `arch arm: "arm64", intel: "amd64"`; `depends_on macos: :ventura`; no MKVToolNix dependency.
- Only final tags `^v[0-9]+\.[0-9]+\.[0-9]+$` bump the tap; pre-releases never do.
- Secret `HOMEBREW_TAP_DEPLOY_KEY` (in the zenvik `release` environment) is readable only by the `homebrew-push` job. Never print, log, write to the workspace, or commit key material.
- `zap` must not remove `~/Library/Application Support/zenvik` (shared CLI config) or anything in `~/Library/Caches/zenvik` except `gui-queue.json`.
- Scripts: `set -euo pipefail`; capture command output into variables rather than piping into `grep -q` (SIGPIPE under pipefail); lint with `shellcheck -x`.
- Creating the public repo, pushing to it, adding the deploy key/secret, and merging/pushing zenvik are outward-facing: confirm with the user immediately before each.
- `brew style` turns on Homebrew developer mode as a side effect; on the user's machine, run `brew developer off` afterwards.

## Review Focus

1. **Swapped architecture hashes:** the arm64 DMG hash must land on `arm:` and the amd64 hash on `intel:`. Task 1's test uses distinct hashes for each and asserts each key.
2. **Version-prefix decoys in `SHA256SUMS`:** `zenvik-gui_9.9.90_darwin_arm64.dmg` must not match version 9.9.9. Task 1's test adds decoy lines.
3. **Re-rendering over existing files:** a bump writes into a tap that already has older files, and must replace them. Task 1's test pre-fills `<outdir>`.
4. **A machine where `chad3814/tap` is already tapped normally** (a real directory, not our link): `check.sh` must refuse rather than delete it. Task 2 has a step that proves this.
5. **Re-running `homebrew-push` after a successful push** must succeed as a no-op, not fail on an empty commit. Task 3's push script checks `git diff --cached --quiet` first; Task 3 has a local dry-run step for it.

---

### Task 1: `scripts/homebrew-render.sh` and its test

**Files:**
- Create: `scripts/homebrew-render.sh`
- Test: `scripts/homebrew-render_test.sh`

**Interfaces:**
- Produces: `scripts/homebrew-render.sh <tag> <outdir>`.
  - Writes `<outdir>/Formula/zenvik.rb` and `<outdir>/Casks/zenvik-gui.rb`.
  - Exit 0 on success. Exit 2 for usage errors or a non-final tag. Exit 1 for a download, `SHA256SUMS` or hash failure; in that case nothing in `<outdir>` is changed.
  - Env `ZENVIK_RELEASE_BASE_URL` (default `https://github.com/chad3814/zenvik`) sets where it downloads `archive/refs/tags/<tag>.tar.gz` and `releases/download/<tag>/SHA256SUMS` from.

- [ ] **Step 1: Write the failing test**

`scripts/homebrew-render_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/homebrew-render.sh. Offline: downloads come from file://
# fixtures. When brew is installed, the rendered files must pass brew style
# (which turns on Homebrew developer mode; run `brew developer off` after).
#
#   scripts/homebrew-render_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

sha() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

arm=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
intel=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

# fixture TAG SUMS-CONTENT: a file:// release with a source tarball and SHA256SUMS
fixture() {
	local tag=$1 sums=$2 fx="$work/fx-$1"
	rm -rf "$fx"
	mkdir -p "$fx/archive/refs/tags" "$fx/releases/download/$tag"
	echo "source of $tag" >"$fx/archive/refs/tags/$tag.tar.gz"
	printf '%s' "$sums" >"$fx/releases/download/$tag/SHA256SUMS"
	echo "file://$fx"
}

good_sums="$decoy  zenvik-gui_9.9.90_darwin_arm64.dmg
$intel  zenvik-gui_9.9.9_darwin_amd64.dmg
$decoy  zenvik_9.9.9_darwin_arm64.tar.gz
$arm  zenvik-gui_9.9.9_darwin_arm64.dmg
$decoy  zenvik-gui_9.9.90_darwin_amd64.dmg
"

out="$work/out"
mkdir -p "$out/Formula" "$out/Casks"
echo old >"$out/Formula/zenvik.rb"
echo old >"$out/Casks/zenvik-gui.rb"
base=$(fixture v9.9.9 "$good_sums")
src_sha=$(sha "$work/fx-v9.9.9/archive/refs/tags/v9.9.9.tar.gz")
if msg=$(ZENVIK_RELEASE_BASE_URL=$base "$here/homebrew-render.sh" v9.9.9 "$out" 2>&1); then
	ok "renders a final release"
else
	bad "renders a final release" "$msg"
fi
formula=$(cat "$out/Formula/zenvik.rb" 2>/dev/null)
cask=$(cat "$out/Casks/zenvik-gui.rb" 2>/dev/null)

has() { # has NAME HAYSTACK NEEDLE
	if [[ $2 == *"$3"* ]]; then ok "$1"; else bad "$1" "missing: $3"; fi
}
has "formula points at the tag's source" "$formula" 'url "https://github.com/chad3814/zenvik/archive/refs/tags/v9.9.9.tar.gz"'
has "formula pins the source hash" "$formula" "sha256 \"$src_sha\""
has "formula depends on mkvtoolnix" "$formula" 'depends_on "mkvtoolnix"'
has "formula stamps the version" "$formula" '-X main.version=v#{version}'
has "cask has the version" "$cask" 'version "9.9.9"'
has "cask has the arm64 hash on arm" "$cask" "sha256 arm:   \"$arm\","
has "cask has the amd64 hash on intel" "$cask" "intel: \"$intel\""
has "cask downloads the DMG" "$cask" 'url "https://github.com/chad3814/zenvik/releases/download/v#{version}/zenvik-gui_#{version}_darwin_#{arch}.dmg"'
if [[ $formula == *old* || $cask == *old* || $cask == *"$decoy"* ]]; then
	bad "replaces old files and ignores decoys"
else
	ok "replaces old files and ignores decoys"
fi
if [[ $formula == *"$work"* || $cask == *"$work"* || $formula == *file:* ]]; then
	bad "written URLs never use the download override"
else
	ok "written URLs never use the download override"
fi

# expect_fail NAME WANT_STATUS TAG BASE: fails with that status and leaves a
# fresh outdir empty
expect_fail() {
	local name=$1 want=$2 tag=$3 b=$4 o="$work/fail-$RANDOM" st
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$b "$here/homebrew-render.sh" "$tag" "$o" 2>&1)
	st=$?
	if [[ $st -ne $want ]]; then
		bad "$name" "status $st (want $want): $msg"
	elif [[ -n $(ls -A "$o") ]]; then
		bad "$name" "wrote into outdir: $(ls -R "$o")"
	else
		ok "$name"
	fi
}
expect_fail "pre-release tag is refused" 2 v9.9.9-rc1 "$base"
expect_fail "tag without v is refused" 2 9.9.9 "$base"
expect_fail "missing arm64 DMG line fails" 1 v9.9.9 "$(fixture v9.9.9 "$intel  zenvik-gui_9.9.9_darwin_amd64.dmg
")"
expect_fail "malformed hash fails" 1 v9.9.9 "$(fixture v9.9.9 "xyz  zenvik-gui_9.9.9_darwin_arm64.dmg
$intel  zenvik-gui_9.9.9_darwin_amd64.dmg
")"
b=$(fixture v9.9.9 "$good_sums")
rm "$work/fx-v9.9.9/archive/refs/tags/v9.9.9.tar.gz"
expect_fail "missing source tarball fails" 1 v9.9.9 "$b"
b=$(fixture v9.9.9 "$good_sums")
rm "$work/fx-v9.9.9/releases/download/v9.9.9/SHA256SUMS"
expect_fail "missing SHA256SUMS fails" 1 v9.9.9 "$b"

if msg=$("$here/homebrew-render.sh" 2>&1); then
	bad "no arguments is a usage error" "$msg"
else
	has "no arguments is a usage error" "$msg" usage
fi

if command -v brew >/dev/null; then
	if msg=$(brew style "$out/Formula/zenvik.rb" "$out/Casks/zenvik-gui.rb" 2>&1); then
		ok "brew style accepts the rendered files"
	else
		bad "brew style accepts the rendered files" "$msg"
	fi
else
	echo "skip brew style (no brew)"
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/homebrew-render_test.sh && scripts/homebrew-render_test.sh`
Expected: FAIL lines ("renders a final release" … "No such file or directory"), non-zero exit.

- [ ] **Step 3: Write the implementation**

`scripts/homebrew-render.sh`:

```bash
#!/usr/bin/env bash
# Write the Homebrew formula and cask for a final zenvik release, for
# chad3814/homebrew-tap (the release workflow's homebrew jobs run this):
#
#   scripts/homebrew-render.sh v1.2.0 <outdir>
#
# writes <outdir>/Formula/zenvik.rb (built from the tag's source) and
# <outdir>/Casks/zenvik-gui.rb (the release's DMGs, hashes from its
# SHA256SUMS). Nothing in <outdir> changes unless both render. Downloads come
# from ZENVIK_RELEASE_BASE_URL (default the GitHub repo; tests use file://);
# the URLs written into the files are always GitHub's.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 vX.Y.Z <outdir>" >&2
	exit 2
fi
tag=$1
out=$2
if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "homebrew-render: $tag is not a final release tag (vX.Y.Z)" >&2
	exit 2
fi
ver=${tag#v}
base=${ZENVIK_RELEASE_BASE_URL:-https://github.com/chad3814/zenvik}

die() { echo "homebrew-render: $*" >&2; exit 1; }
sha() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
hex64() { [[ $1 =~ ^[0-9a-f]{64}$ ]]; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -fsSL "$base/archive/refs/tags/$tag.tar.gz" -o "$work/src.tar.gz" ||
	die "couldn't download the $tag source tarball"
src=$(sha "$work/src.tar.gz")
curl -fsSL "$base/releases/download/$tag/SHA256SUMS" -o "$work/SHA256SUMS" ||
	die "couldn't download $tag's SHA256SUMS"

dmg_sha() {
	awk -v f="zenvik-gui_${ver}_darwin_$1.dmg" '$2 == f || $2 == "*" f { print $1; exit }' "$work/SHA256SUMS"
}
arm=$(dmg_sha arm64)
intel=$(dmg_sha amd64)
[[ -n $arm ]] || die "SHA256SUMS has no zenvik-gui_${ver}_darwin_arm64.dmg"
[[ -n $intel ]] || die "SHA256SUMS has no zenvik-gui_${ver}_darwin_amd64.dmg"
for h in "$src" "$arm" "$intel"; do
	hex64 "$h" || die "not a SHA-256: $h"
done

mkdir -p "$work/out/Formula" "$work/out/Casks"
cat >"$work/out/Formula/zenvik.rb" <<EOF
class Zenvik < Formula
  desc "Remux Blu-ray and DVD disc images to MKV"
  homepage "https://github.com/chad3814/zenvik"
  url "https://github.com/chad3814/zenvik/archive/refs/tags/$tag.tar.gz"
  sha256 "$src"
  license "MIT"
  head "https://github.com/chad3814/zenvik.git", branch: "main"

  livecheck do
    url :stable
    strategy :github_latest
  end

  depends_on "go" => :build
  depends_on "mkvtoolnix"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=v#{version}"), "./cmd/zenvik"
  end

  test do
    assert_match "zenvik v#{version}", shell_output("#{bin}/zenvik --version")
    assert_match "no such file", shell_output("#{bin}/zenvik info #{testpath}/missing.iso 2>&1", 1)
  end
end
EOF
cat >"$work/out/Casks/zenvik-gui.rb" <<EOF
cask "zenvik-gui" do
  arch arm: "arm64", intel: "amd64"

  version "$ver"
  sha256 arm:   "$arm",
         intel: "$intel"

  url "https://github.com/chad3814/zenvik/releases/download/v#{version}/zenvik-gui_#{version}_darwin_#{arch}.dmg"
  name "Zenvik"
  desc "Desktop app to remux Blu-ray and DVD disc images to MKV"
  homepage "https://github.com/chad3814/zenvik"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: :ventura

  app "Zenvik.app"

  zap trash: [
    "~/Library/Caches/dev.cwalker.zenvik",
    "~/Library/Caches/zenvik/gui-queue.json",
    "~/Library/HTTPStorages/dev.cwalker.zenvik",
    "~/Library/Preferences/dev.cwalker.zenvik.plist",
    "~/Library/Saved Application State/dev.cwalker.zenvik.savedState",
    "~/Library/WebKit/dev.cwalker.zenvik",
  ]
end
EOF

mkdir -p "$out/Formula" "$out/Casks"
mv "$work/out/Formula/zenvik.rb" "$out/Formula/zenvik.rb"
mv "$work/out/Casks/zenvik-gui.rb" "$out/Casks/zenvik-gui.rb"
echo "rendered zenvik $ver into $out" >&2
```

- [ ] **Step 4: Run the tests and lint**

Run: `chmod +x scripts/homebrew-render.sh && scripts/homebrew-render_test.sh; brew developer off; shellcheck -x scripts/homebrew-render.sh scripts/homebrew-render_test.sh`
Expected: `19 passed, 0 failed` (18 when brew is absent), and shellcheck prints nothing.

- [ ] **Step 5: Render the real v1.1.1 as a smoke test**

Run: `scripts/homebrew-render.sh v1.1.1 "$TMPDIR/hb-v111" && grep -E 'sha256|version "' "$TMPDIR/hb-v111"/*/*.rb`
Expected: one formula `sha256` and two cask hashes. The cask hashes must equal the `zenvik-gui_1.1.1_darwin_*.dmg` lines of `gh release download v1.1.1 -R chad3814/zenvik -p SHA256SUMS -O -`. Keep `$TMPDIR/hb-v111` for Task 2.

- [ ] **Step 6: Commit**

```bash
git add scripts/homebrew-render.sh scripts/homebrew-render_test.sh
git commit -m "homebrew-render.sh: write the tap's zenvik formula and zenvik-gui cask for a final release"
```

---

### Task 2: The tap repo `chad3814/homebrew-tap`

**Files** (in a new scratch checkout `$TMPDIR/homebrew-tap`, not in the zenvik repo):
- Create: `check.sh`, `.github/workflows/ci.yml`, `README.md`
- Create from Task 1's output: `Formula/zenvik.rb`, `Casks/zenvik-gui.rb` (v1.1.1)

**Interfaces:**
- Consumes: the files rendered by Task 1 into `$TMPDIR/hb-v111`.
- Produces: `check.sh [--formula-only]` at the tap's root.
  - Exit 0 when everything passes. Exit 2 on a usage error. Exit 1 on the first failed step, printing `check.sh: failed at: <step>`.
  - Task 3's `homebrew-check` job runs it from a clone of the tap with the rendered files copied over.

- [ ] **Step 1: Create the scratch checkout with the rendered files**

```bash
tap="$TMPDIR/homebrew-tap"
rm -rf "$tap" && mkdir -p "$tap/.github/workflows" && cd "$tap" && git init -q -b main
cp -R "$TMPDIR/hb-v111/." "$tap/"
```

- [ ] **Step 2: Write `check.sh`**

```bash
#!/usr/bin/env bash
# Prove this tap's formula and cask install and work. CI runs it on every
# push; zenvik's release workflow runs it on a freshly rendered bump before
# pushing that bump here.
#
#   ./check.sh                 # formula and cask (macOS)
#   ./check.sh --formula-only  # formula only (Linux)
#
# It links this checkout in as the chad3814/tap tap (so the files on disk,
# committed or not, are what's checked), then audits, installs and tests.
set -Eeuo pipefail

formula_only=0
case ${1:-} in
"") ;;
--formula-only) formula_only=1 ;;
*)
	echo "usage: $0 [--formula-only]" >&2
	exit 2
	;;
esac

here=$(cd "$(dirname "$0")" && pwd -P)
current=setup
step() {
	current=$*
	echo "==> $*"
}
trap 'echo "check.sh: failed at: $current" >&2' ERR
export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ENV_HINTS=1

step "link $here as chad3814/tap"
taps="$(brew --repository)/Library/Taps/chad3814"
link="$taps/homebrew-tap"
if [[ -e $link || -L $link ]]; then
	if [[ $(cd "$link" 2>/dev/null && pwd -P) == "$here" ]]; then
		: # already this checkout (setup-homebrew links the repo it runs in)
	elif [[ -L $link ]]; then
		rm "$link"
	else
		echo "check.sh: $link is a real tap, not a link; run \`brew untap chad3814/tap\` first" >&2
		exit 1
	fi
fi
if [[ ! -e $link ]]; then
	mkdir -p "$taps"
	ln -s "$here" "$link"
fi

step "brew style"
brew style chad3814/tap

step "audit the zenvik formula"
brew audit --strict --online --formula chad3814/tap/zenvik
step "install the zenvik formula from source"
brew install --build-from-source chad3814/tap/zenvik
step "test the zenvik formula"
brew test chad3814/tap/zenvik

if [[ $formula_only == 0 ]]; then
	step "audit the zenvik-gui cask"
	brew audit --strict --online --cask chad3814/tap/zenvik-gui
	step "install the zenvik-gui cask"
	brew install --cask chad3814/tap/zenvik-gui
	app=/Applications/Zenvik.app
	step "Gatekeeper accepts $app"
	out=$(spctl -a -vv -t exec "$app" 2>&1 || true)
	if ! grep -q 'source=Notarized Developer ID' <<<"$out"; then
		echo "$out" >&2
		false
	fi
	step "the app's bundled mkvmerge runs"
	out=$("$app/Contents/Helpers/mkvmerge" --version 2>&1)
	if [[ $out != "mkvmerge v"* ]]; then
		echo "$out" >&2
		false
	fi
	step "uninstall the zenvik-gui cask with --zap"
	brew uninstall --cask --zap chad3814/tap/zenvik-gui
fi

echo "check.sh: all checks passed"
```

Then: `chmod +x "$tap/check.sh" && shellcheck "$tap/check.sh"`. Expected: no output.

- [ ] **Step 3: Write `.github/workflows/ci.yml`**

```yaml
name: ci

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  check:
    strategy:
      fail-fast: false
      matrix:
        include:
          - { os: macos-latest, args: "" }
          - { os: ubuntu-latest, args: "--formula-only" }
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v5
      - uses: Homebrew/actions/setup-homebrew@main
      - run: ./check.sh ${{ matrix.args }}
```

Then run `actionlint "$tap/.github/workflows/ci.yml"` (or `go run github.com/rhysd/actionlint/cmd/actionlint@latest` from inside `$tap`). Expected: no output.

- [ ] **Step 4: Write `README.md`**

````markdown
# chad3814/homebrew-tap

Homebrew formulae and casks for [zenvik](https://github.com/chad3814/zenvik), which remuxes Blu-ray and DVD disc images to MKV.

```sh
brew install chad3814/tap/zenvik              # the zenvik command-line tool
brew install --cask chad3814/tap/zenvik-gui   # the Zenvik desktop app (macOS 13+)
```

- `zenvik` builds from source and installs [MKVToolNix](https://mkvtoolnix.download) (for `mkvmerge`) with it. It works on macOS and Linux.
- `zenvik-gui` installs the signed, notarized `Zenvik.app` from the release's disk image. The app includes its own `mkvmerge`.

zenvik's release workflow updates these files on every release, after `check.sh` has proved they install. To check by hand: `./check.sh` (macOS), or `./check.sh --formula-only` (Linux).
````

- [ ] **Step 5: Run `check.sh --formula-only` locally, then undo its installs**

This installs `zenvik` into your Homebrew; MKVToolNix is already installed. The cask half is left to CI, because it would install into `/Applications` over the Zenvik.app you already have there.

```bash
cd "$tap" && git add -A && git commit -qm "zenvik 1.1.1" && ./check.sh --formula-only
brew uninstall chad3814/tap/zenvik
rm "$(brew --repository)/Library/Taps/chad3814/homebrew-tap" && rmdir "$(brew --repository)/Library/Taps/chad3814"
brew developer off
```

Expected: `check.sh: all checks passed`.

- [ ] **Step 6: Prove `check.sh` refuses a real tap (Review Focus 4)**

```bash
mkdir -p "$(brew --repository)/Library/Taps/chad3814/homebrew-tap/Formula"
(cd "$tap" && ./check.sh --formula-only); echo "exit $?"
rm -rf "$(brew --repository)/Library/Taps/chad3814"
```

Expected: `check.sh: … is a real tap, not a link; run \`brew untap chad3814/tap\` first` and `exit 1`, with the directory still there until the `rm`.

- [ ] **Step 7: Ask the user, then create and push the public repo**

Confirm with the user first. Then:

```bash
cd "$tap" && gh repo create chad3814/homebrew-tap --public \
  --description "Homebrew formula and cask for zenvik" --source . --push
```

Watch its CI:

```bash
sleep 15
run=$(gh run list -R chad3814/homebrew-tap -L 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$run" -R chad3814/homebrew-tap --exit-status
```

Expected: both jobs succeed, which proves the cask installs, passes Gatekeeper, and runs its mkvmerge on macOS. If CI fails, fix the cause in the scratch checkout and push again. If the fix is in the formula or cask text, fix `scripts/homebrew-render.sh` (and its test) in the zenvik worktree too, then re-render.

- [ ] **Step 8: Replace the scratch checkout with the standard local layout**

```bash
cd ~/Projects && "$HOME/.claude/skills/project-setup/project-setup.sh" git@github.com:chad3814/homebrew-tap.git
rm -rf "$TMPDIR/homebrew-tap" "$TMPDIR/hb-v111"
```

Expected: `~/Projects/homebrew-tap/worktrees/main` contains the five files.

---

### Task 3: Release jobs and docs (zenvik repo)

**Files:**
- Modify: `.github/workflows/release.yml` (append three jobs after `publish`)
- Modify: `README.md` (install section), `CLAUDE.md` (Commands list)

**Interfaces:**
- Consumes: `scripts/homebrew-render.sh <tag> <outdir>` (Task 1); the tap's `check.sh [--formula-only]` (Task 2); secret `HOMEBREW_TAP_DEPLOY_KEY` (Task 4).
- Produces: jobs `homebrew-render`, `homebrew-check`, `homebrew-push`, and an artifact named `homebrew`.

- [ ] **Step 1: Append the jobs to `.github/workflows/release.yml`**

```yaml
  # Homebrew (chad3814/homebrew-tap), final releases only: render the formula
  # and cask from what was just published, prove them with the tap's own
  # check.sh, then push. Only homebrew-push can read the deploy key. A failure
  # leaves the release published and the tap on the previous version; re-run
  # the failed job to retry.
  homebrew-render:
    if: ${{ !contains(github.ref_name, '-') }}
    needs: publish
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - run: scripts/homebrew-render.sh "$GITHUB_REF_NAME" homebrew
      - uses: actions/upload-artifact@v4
        with:
          name: homebrew
          path: homebrew

  homebrew-check:
    needs: homebrew-render
    strategy:
      fail-fast: false
      matrix:
        include:
          - { os: macos-latest, args: "" }
          - { os: ubuntu-latest, args: "--formula-only" }
    runs-on: ${{ matrix.os }}
    steps:
      - uses: Homebrew/actions/setup-homebrew@main
      - run: git clone --depth 1 https://github.com/chad3814/homebrew-tap.git "$RUNNER_TEMP/tap"
      - uses: actions/download-artifact@v5
        with:
          name: homebrew
          path: ${{ runner.temp }}/rendered
      - run: |
          cp -R "$RUNNER_TEMP/rendered/." "$RUNNER_TEMP/tap/"
          "$RUNNER_TEMP/tap/check.sh" ${{ matrix.args }}

  homebrew-push:
    needs: homebrew-check
    runs-on: ubuntu-latest
    environment: release
    steps:
      - uses: actions/download-artifact@v5
        with:
          name: homebrew
          path: ${{ runner.temp }}/rendered
      - name: Push the bump to chad3814/homebrew-tap
        env:
          DEPLOY_KEY: ${{ secrets.HOMEBREW_TAP_DEPLOY_KEY }}
        run: |
          if [[ -z $DEPLOY_KEY ]]; then
            echo "HOMEBREW_TAP_DEPLOY_KEY is not set in the release environment" >&2
            exit 1
          fi
          eval "$(ssh-agent -s)" >/dev/null
          trap 'ssh-agent -k >/dev/null' EXIT
          ssh-add - <<<"$DEPLOY_KEY" >/dev/null 2>&1
          unset DEPLOY_KEY
          mkdir -p ~/.ssh
          curl -fsSL https://api.github.com/meta | jq -r '.ssh_keys[] | "github.com " + .' >~/.ssh/known_hosts
          git clone git@github.com:chad3814/homebrew-tap.git "$RUNNER_TEMP/tap"
          cp -R "$RUNNER_TEMP/rendered/." "$RUNNER_TEMP/tap/"
          cd "$RUNNER_TEMP/tap"
          git add Formula Casks
          if git diff --cached --quiet; then
            echo "the tap already has zenvik ${GITHUB_REF_NAME#v}"
            exit 0
          fi
          git -c user.name='github-actions[bot]' \
            -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
            commit -m "zenvik ${GITHUB_REF_NAME#v}"
          git push origin HEAD:main
```

- [ ] **Step 2: Lint**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest` (in the worktree root).
Expected: no output. It shellchecks the `run:` blocks too.

- [ ] **Step 3: Dry-run the push script's no-op path (Review Focus 5)**

The job can't run locally with the key. Instead, prove the "nothing changed" branch against a copy of the real tap:

```bash
d=$(mktemp -d) && git clone -q https://github.com/chad3814/homebrew-tap.git "$d/tap"
scripts/homebrew-render.sh v1.1.1 "$d/rendered" && cp -R "$d/rendered/." "$d/tap/"
(cd "$d/tap" && git add Formula Casks && git diff --cached --quiet && echo "no-op: nothing to push")
rm -rf "$d"
```

Expected: `no-op: nothing to push`. The tap holds exactly the v1.1.1 render from Task 2, so this also proves the render is reproducible.

- [ ] **Step 4: Docs**

`README.md`: in the install section that starts "Download the archive for your platform", add before that paragraph:

```markdown
With [Homebrew](https://brew.sh) (macOS or Linux), `brew install chad3814/tap/zenvik` builds zenvik and installs MKVToolNix with it; on macOS, `brew install --cask chad3814/tap/zenvik-gui` installs the desktop app.
```

In the desktop-app paragraph, after "open the `.dmg` and drag Zenvik to Applications", add `, or \`brew install --cask chad3814/tap/zenvik-gui\``.

`CLAUDE.md`: add this bullet after the "Bundled mkvmerge" bullet:

```markdown
- Homebrew tap (`chad3814/homebrew-tap`: formula `zenvik` from source with `depends_on "mkvtoolnix"`, cask `zenvik-gui` from the DMGs; its `check.sh` is the test): `scripts/homebrew-render.sh vX.Y.Z outdir` writes both from a published release (tests: `scripts/homebrew-render_test.sh`). For final tags only, the release workflow's `homebrew-render`, `homebrew-check` (runs the tap's `check.sh` on the render) and `homebrew-push` jobs update the tap; only `homebrew-push` reads the `release` environment secret `HOMEBREW_TAP_DEPLOY_KEY`, a write deploy key on the tap. Never print or commit it. Re-run a failed job to retry a bump.
```

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml README.md CLAUDE.md
git commit -m "Release: bump the Homebrew tap's zenvik formula and zenvik-gui cask on final releases, after the tap's check.sh passes on them"
```

---

### Task 4: Deploy key and secret

**Files:** none in either repo.

**Interfaces:**
- Produces:
  - a write-enabled deploy key titled `zenvik release` on `chad3814/homebrew-tap`;
  - the environment secret `HOMEBREW_TAP_DEPLOY_KEY` in `chad3814/zenvik`'s `release` environment.

- [ ] **Step 1: Ask the user, then create and install the key in one step**

Confirm with the user first. Then run this as one shell command. No key material is printed: `ssh-keygen -q` prints nothing, and the secret is read from the file.

```bash
k=$(mktemp -d) && chmod 700 "$k" \
  && ssh-keygen -q -t ed25519 -N '' -C zenvik-release -f "$k/key" \
  && gh repo deploy-key add "$k/key.pub" -R chad3814/homebrew-tap --title "zenvik release" --allow-write \
  && gh secret set HOMEBREW_TAP_DEPLOY_KEY --env release -R chad3814/zenvik <"$k/key"; \
  rm -rf "$k"
```

- [ ] **Step 2: Verify without reading the secret**

```bash
gh repo deploy-key list -R chad3814/homebrew-tap
gh secret list --env release -R chad3814/zenvik | cut -f1
```

Expected: one key titled `zenvik release` with read-write access, and `HOMEBREW_TAP_DEPLOY_KEY` among the secret names.

---

### Task 5: Merge, push, and the first automated bump

- [ ] **Step 1: Full local checks in the worktree**

Run, with `brew developer off` afterwards:
- `scripts/homebrew-render_test.sh`
- `shellcheck -x scripts/*.sh`
- `go run github.com/rhysd/actionlint/cmd/actionlint@latest`

Expected: all pass.

- [ ] **Step 2: Ask the user, then merge and push**

Confirm with the user first. Then fast-forward `main` to `feat/homebrew-tap` from `worktrees/main`, run `git push origin main`, and watch CI on main.

- [ ] **Step 3: First automated bump**

This happens at the next final release, whose tag the user chooses (`v1.1.2` or `v1.2.0`):
- watch `homebrew-render`, both `homebrew-check` jobs and `homebrew-push`;
- confirm the tap has a `zenvik X.Y.Z` commit by `github-actions[bot]` and that the tap's CI passes on it;
- run `brew update && brew upgrade chad3814/tap/zenvik` (after tapping, if needed) and check `zenvik --version`.

If a job fails, fix the cause, then re-run the failed job (before a new tag) or release again.
