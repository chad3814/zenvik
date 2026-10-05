#!/usr/bin/env bash
# Build release archives into dist/: all supported platforms, or the given
# targets.
#
#   scripts/release-build.sh v0.1.0
#   scripts/release-build.sh v0.1.0 darwin/arm64 darwin/amd64
#
# Produces dist/zenvik_<version>_<os>_<arch>.tar.gz (macOS, Linux),
# dist/zenvik_<version>_windows_amd64.zip, and dist/SHA256SUMS. zenvik has
# no cgo, so every target cross-compiles from any host. On macOS the darwin
# binaries are signed and notarized via scripts/macos-sign.sh (see there for
# the environment it reads); elsewhere they're built unsigned with a warning.
set -euo pipefail

if [[ $# -lt 1 || ! $1 =~ ^v[0-9] ]]; then
	echo "usage: $0 vX.Y.Z [os/arch ...]" >&2
	exit 2
fi
tag=$1
shift
ver=${tag#v}

root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
rm -rf "$dist"
mkdir -p "$dist"

targets=(darwin/arm64 darwin/amd64 linux/amd64 windows/amd64)
if [[ $# -gt 0 ]]; then
	targets=("$@")
fi

signing=0
for target in "${targets[@]}"; do
	# (an if, not `[[ ]] &&`: a loop ending in a false test fails under set -e)
	if [[ $target == darwin/* ]]; then signing=1; fi
done
if [[ $signing == 1 ]]; then
	# shellcheck source=scripts/macos-sign.sh
	source "$root/scripts/macos-sign.sh"
	trap macos_sign_cleanup EXIT
	macos_sign_setup
fi

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
	if [[ $goos == darwin && $MACOS_SIGN_ENABLED == 1 ]]; then
		macos_sign "$stage/$bin"
		macos_verify_signature "$stage/$bin"
		# A bare Mach-O can't hold a stapled ticket; Gatekeeper fetches it
		# online on first run. Zip it only to submit it.
		ditto -c -k "$stage/$bin" "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_notarize "$MACOS_SIGN_TMP/$name-notary.zip"
	fi
	cp "$root/README.md" "$root/LICENSE" "$stage/"
	# The Windows CLI bundles MKVToolNix's mkvmerge.exe, as the desktop app
	# does (see third_party/mkvtoolnix.env); zenvik finds it beside itself.
	if [[ $goos == windows ]]; then
		# shellcheck source=third_party/mkvtoolnix.env
		source "$root/third_party/mkvtoolnix.env"
		mtx=$(mktemp -d)
		"$root/scripts/fetch-mkvmerge.sh" "$target" "$mtx"
		if ! "$root/scripts/check-mkvtoolnix-source.sh" "$MKVTOOLNIX_VERSION"; then
			if [[ ${CI:-} == true ]]; then
				rm -rf "$mtx"
				exit 1
			fi
			echo "warning: building anyway (not CI); don't publish this build" >&2
		fi
		cp "$mtx/mkvmerge.exe" "$mtx"/MKVTOOLNIX-*.txt "$stage/"
		cp -R "$mtx/MKVTOOLNIX-LICENSES" "$stage/"
		rm -rf "$mtx"
	fi
	if [[ $goos == windows ]]; then
		(cd "$dist" && zip -qr "$name.zip" "$name")
	else
		tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	fi
	rm -rf "$stage"
done

# Only the archives this run built: a target list may have no .zip (Windows)
# or no .tar.gz, and an unmatched glob would make the checksum tool fail.
shopt -s nullglob
archives=("$dist"/*.tar.gz "$dist"/*.zip)
shopt -u nullglob
(cd "$dist" && if command -v sha256sum >/dev/null; then sha256sum "${archives[@]##*/}"; else shasum -a 256 "${archives[@]##*/}"; fi \
	>SHA256SUMS)
echo "wrote:"
ls -l "$dist"
