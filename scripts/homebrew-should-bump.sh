#!/usr/bin/env bash
# Decide whether a release may update chad3814/homebrew-tap: only when it is
# newer than what the tap already has, so re-running an old release's push
# (or two releases racing) never downgrades it.
#
#   scripts/homebrew-should-bump.sh vX.Y.Z <tap checkout>
#
# Exit 0: bump. Exit 3: skip (the tap already has this version or a newer
# one; the message names it). Exit 2: usage or a non-final tag.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vX.Y.Z <tap checkout>" >&2
	exit 2
fi
new=${1#v}
cask="$2/Casks/zenvik-gui.rb"
have=""
if [[ -f $cask ]]; then
	have=$(sed -n 's/^  version "\(.*\)"$/\1/p' "$cask" | head -1)
fi
if [[ -z $have ]]; then
	exit 0
fi
newest=$(printf '%s\n%s\n' "$have" "$new" | sort -V | tail -1)
if [[ $new == "$have" || $newest != "$new" ]]; then
	echo "the tap already has zenvik $have; not replacing it with $new" >&2
	exit 3
fi
