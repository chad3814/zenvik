#!/usr/bin/env bash
# Check that zenvik hosts the source of MKVToolNix <version> (GPLv2): the
# release mkvtoolnix-src-<version> must serve mkvtoolnix-<version>.tar.xz,
# which every bundled mkvmerge's notice links to.
#
#   scripts/check-mkvtoolnix-source.sh 102.0
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <version>" >&2
	exit 2
fi
v=$1
url="https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-$v/mkvtoolnix-$v.tar.xz"
if ! curl -fsIL -o /dev/null "$url"; then
	echo "the source release mkvtoolnix-src-$v is missing ($url); create it with scripts/bump-mkvtoolnix.sh $v <macos-build> --publish" >&2
	exit 1
fi
echo "source for MKVToolNix $v is hosted at $url" >&2
