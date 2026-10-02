#!/usr/bin/env bash
# Build release archives for every supported platform into dist/.
#
#   scripts/release-build.sh v0.1.0
#
# Produces dist/zenvik_<version>_<os>_<arch>.tar.gz (macOS, Linux),
# dist/zenvik_<version>_windows_amd64.zip, and dist/SHA256SUMS. zenvik has
# no cgo, so every target cross-compiles from any host.
set -euo pipefail

if [[ $# -ne 1 || ! $1 =~ ^v[0-9] ]]; then
	echo "usage: $0 vX.Y.Z" >&2
	exit 2
fi
tag=$1
ver=${tag#v}

root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
rm -rf "$dist"
mkdir -p "$dist"

targets=(darwin/arm64 darwin/amd64 linux/amd64 windows/amd64)
for target in "${targets[@]}"; do
	goos=${target%/*}
	goarch=${target#*/}
	name="zenvik_${ver}_${goos}_${goarch}"
	stage="$dist/$name"
	mkdir -p "$stage"
	bin=zenvik
	[[ $goos == windows ]] && bin=zenvik.exe
	echo "building $name"
	(cd "$root" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
		go build -trimpath -ldflags "-s -w -X main.version=$tag" -o "$stage/$bin" ./cmd/zenvik)
	cp "$root/README.md" "$root/LICENSE" "$stage/"
	if [[ $goos == windows ]]; then
		(cd "$dist" && zip -qr "$name.zip" "$name")
	else
		tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	fi
	rm -rf "$stage"
done

(cd "$dist" && if command -v sha256sum >/dev/null; then sha256sum ./*.tar.gz ./*.zip; else shasum -a 256 ./*.tar.gz ./*.zip; fi |
	sed 's| \./| |' >SHA256SUMS)
echo "wrote:"
ls -l "$dist"
