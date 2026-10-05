#!/usr/bin/env bash
# Open microsoft/winget-pkgs pull requests for the manifests winget-render.sh
# wrote (the release workflow's winget-submit job runs this):
#
#   GH_TOKEN=… scripts/winget-submit.sh v1.3.0 <rendered dir>
#
# For each package under <dir>/manifests/c/chad3814/: skip it if winget-pkgs
# already has this version or a newer one, or an open PR from our branch;
# otherwise sync the fork's master with upstream, commit the package's three
# files to <ID>-<ver> on the fork in one commit, and open a PR. gh reads the
# token from GH_TOKEN; this script never prints it.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
upstream=${WINGET_UPSTREAM:-microsoft/winget-pkgs}
fork=${WINGET_FORK:-chad3814/winget-pkgs}
owner=${fork%%/*}
if [[ $# -ne 2 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vX.Y.Z <rendered dir>" >&2
	exit 2
fi
tag=$1
dir=$2
ver=${tag#v}
[[ -n ${GH_TOKEN:-} ]] || { echo "winget-submit: GH_TOKEN is not set" >&2; exit 1; }

for pkgdir in "$dir"/manifests/c/chad3814/*/"$ver"; do
	[[ -d $pkgdir ]] || continue
	pkg=$(basename "$(dirname "$pkgdir")")
	id="chad3814.$pkg"
	path="manifests/c/chad3814/$pkg"

	# Versions upstream already has (a 404 means a new package).
	new=0
	if ! existing=$(gh api "repos/$upstream/contents/$path" --jq '.[] | select(.type == "dir") | .name' 2>"$dir/.err"); then
		if grep -q 'HTTP 404' "$dir/.err"; then
			new=1
			existing=""
		else
			cat "$dir/.err" >&2
			exit 1
		fi
	fi
	highest=$(printf '%s\n' "$existing" | grep -v '^$' | sort -V | tail -1 || true)
	st=0
	"$here/newer-version.sh" "$tag" "$highest" || st=$?
	if [[ $st -eq 3 ]]; then
		echo "$id: winget-pkgs already has $highest; skipping"
		continue
	elif [[ $st -ne 0 ]]; then
		exit "$st"
	fi

	branch="$id-$ver"
	open=$(gh api "repos/$upstream/pulls?state=open&head=$owner:$branch" --jq length)
	if [[ $open != 0 ]]; then
		echo "$id: a PR for $ver is already open; skipping"
		continue
	fi

	gh api -X POST "repos/$fork/merge-upstream" -f branch=master >/dev/null
	base=$(gh api "repos/$fork/git/ref/heads/master" --jq .object.sha)
	basetree=$(gh api "repos/$fork/git/commits/$base" --jq .tree.sha)
	entries="[]"
	for f in "$pkgdir"/*.yaml; do
		blob=$(base64 <"$f" | tr -d '\n' | jq -Rs '{encoding: "base64", content: .}' |
			gh api "repos/$fork/git/blobs" --input - --jq .sha)
		entries=$(jq --arg p "$path/$ver/$(basename "$f")" --arg s "$blob" \
			'. + [{path: $p, mode: "100644", type: "blob", sha: $s}]' <<<"$entries")
	done
	tree=$(jq -n --arg b "$basetree" --argjson t "$entries" '{base_tree: $b, tree: $t}' |
		gh api "repos/$fork/git/trees" --input - --jq .sha)
	if [[ $new == 1 ]]; then
		title="New package: $id version $ver"
	else
		title="Update: $id to $ver"
	fi
	commit=$(jq -n --arg m "$title" --arg t "$tree" --arg p "$base" '{message: $m, tree: $t, parents: [$p]}' |
		gh api "repos/$fork/git/commits" --input - --jq .sha)
	if ! gh api "repos/$fork/git/refs" -f ref="refs/heads/$branch" -f sha="$commit" >/dev/null 2>&1; then
		gh api -X PATCH "repos/$fork/git/refs/heads/$branch" -f sha="$commit" -F force=true >/dev/null
	fi
	body="## 📖 Description
$title, from https://github.com/chad3814/zenvik/releases/tag/$tag (zip-wrapped portable).

## 📦 Manifest Checklist

- [x] Checked that there aren't other open [pull requests](https://github.com/microsoft/winget-pkgs/pulls) for the same manifest update/change
- [x] This PR only modifies one (1) manifest
- [x] Validated manifest locally with \`winget validate --manifest <path>\`
- [x] Tested manifest locally with \`winget install --manifest <path>\`
- [x] Manifest conforms to the [1.12 schema](https://github.com/microsoft/winget-pkgs/tree/master/doc/manifest/schema/1.12.0)"
	url=$(gh api "repos/$upstream/pulls" -f title="$title" -f head="$owner:$branch" -f base=master -f body="$body" --jq .html_url)
	echo "$id: opened $url"
done
rm -f "$dir/.err"
