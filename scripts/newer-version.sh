#!/usr/bin/env bash
# Whether a release is newer than what a package repository (the Homebrew
# tap, the Scoop bucket) already has, so a bump never downgrades it.
#
#   scripts/newer-version.sh vX.Y.Z <have-version>
#
# Exit 0: newer, or <have-version> is empty. Exit 3: not newer (the message
# names <have-version>). Exit 2: usage, or a non-final tag.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vX.Y.Z <have-version>" >&2
	exit 2
fi
new=${1#v}
have=$2
if [[ -z $have ]]; then
	exit 0
fi
newest=$(printf '%s\n%s\n' "$have" "$new" | sort -V | tail -1)
if [[ $new == "$have" || $newest != "$new" ]]; then
	echo "the package repository already has zenvik $have; not replacing it with $new" >&2
	exit 3
fi
