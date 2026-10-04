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
