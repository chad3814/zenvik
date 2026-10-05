#!/usr/bin/env bash
# Tests for scripts/winget-submit.sh, with a fake gh that logs its calls
# and answers from FAKE_* variables. Offline.
#
#   scripts/winget-submit_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
command -v jq >/dev/null || { echo "needs jq" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
# fake gh: logs "gh <args>" (and any --input body) to $FAKE_LOG
args="$*"
echo "gh $args" >>"$FAKE_LOG"
if [[ $args == *"--input -"* ]]; then jq -c . >>"$FAKE_LOG"; fi
case $args in
*"repos/microsoft/winget-pkgs/contents/manifests/c/chad3814/Zenvik "*|*"contents/manifests/c/chad3814/Zenvik --jq"*)
	v=${FAKE_ZENVIK:-404}
	if [[ $v == 404 ]]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
	printf '%s\n' $v ;;
*"contents/manifests/c/chad3814/ZenvikGUI"*)
	v=${FAKE_GUI:-404}
	if [[ $v == 404 ]]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
	printf '%s\n' $v ;;
*"merge-upstream"*) echo '{}' ;;
*"pulls?state=open"*) echo "${FAKE_OPEN:-0}" ;;
*"git/ref/heads/master"*) echo base123 ;;
*"git/commits/base123"*) echo tree123 ;;
*"git/blobs"*) echo "blob$RANDOM" ;;
*"git/trees"*) echo newtree ;;
*"git/commits"*) echo newcommit ;;
*"git/refs"*) echo '{}' ;;
*"repos/microsoft/winget-pkgs/pulls"*) echo "https://github.com/microsoft/winget-pkgs/pull/1" ;;
*) echo "fake gh: unexpected: $args" >&2; exit 9 ;;
esac
STUB
chmod +x "$work/bin/gh"

# rendered tree for 9.9.9
r="$work/rendered"
for p in Zenvik ZenvikGUI; do
	mkdir -p "$r/manifests/c/chad3814/$p/9.9.9"
	for f in "" .installer .locale.en-US; do echo "PackageIdentifier: chad3814.$p" >"$r/manifests/c/chad3814/$p/9.9.9/chad3814.$p$f.yaml"; done
done

# run NAME env...: run the submitter, leaving output in $out and the log in $log
run() {
	shift # the first argument only labels the call
	rm -f "$work/log"
	out=$(env PATH="$work/bin:$PATH" FAKE_LOG="$work/log" GH_TOKEN=sekrit-token-value "$@" "$here/winget-submit.sh" v9.9.9 "$r" 2>&1)
	st=$?
	log=$(cat "$work/log" 2>/dev/null)
}

run "new" FAKE_ZENVIK=404 FAKE_GUI=404
if [[ $st -eq 0 ]]; then ok "new packages: exit 0"; else bad "new packages: exit 0" "$out"; fi
if grep -q 'title=New package: chad3814.Zenvik version 9.9.9' <<<"$log" && grep -q 'title=New package: chad3814.ZenvikGUI version 9.9.9' <<<"$log"; then ok "new packages get 'New package' titles"; else bad "new packages get 'New package' titles" "$log"; fi
if grep -q 'head=chad3814:chad3814.Zenvik-9.9.9' <<<"$log"; then ok "PR head is the fork branch"; else bad "PR head is the fork branch" "$log"; fi
if grep -q 'refs/heads/chad3814.Zenvik-9.9.9' <<<"$log"; then ok "branch named <ID>-<ver>"; else bad "branch named <ID>-<ver>" "$log"; fi
n=$(grep -E '^\{"base_tree' <<<"$log" | head -1 | jq '.tree | length' 2>/dev/null)
if [[ $n == 3 ]]; then ok "one tree with exactly three files"; else bad "one tree with exactly three files" "got $n; $log"; fi
if grep -q 'manifests/c/chad3814/Zenvik/9.9.9/chad3814.Zenvik.installer.yaml' <<<"$log"; then ok "tree paths are the manifest paths"; else bad "tree paths are the manifest paths" "$log"; fi
if grep -q sekrit-token-value <<<"$out$log"; then bad "the token is never printed"; else ok "the token is never printed"; fi

run "update" FAKE_ZENVIK="9.9.8" FAKE_GUI="9.9.7 9.9.8"
if grep -q 'title=Update: chad3814.Zenvik to 9.9.9' <<<"$log"; then ok "an older upstream version gets an 'Update' title"; else bad "an older upstream version gets an 'Update' title" "$log"; fi

run "same" FAKE_ZENVIK="9.9.9" FAKE_GUI="9.9.10"
if [[ $st -eq 0 ]] && ! grep -q 'repos/microsoft/winget-pkgs/pulls -f' <<<"$log"; then ok "same or newer upstream: skipped, no PR"; else bad "same or newer upstream: skipped, no PR" "$log"; fi

run "open" FAKE_ZENVIK=9.9.8 FAKE_GUI=9.9.8 FAKE_OPEN=1
if [[ $st -eq 0 ]] && ! grep -q 'git/blobs' <<<"$log"; then ok "an open PR: skipped before any commit"; else bad "an open PR: skipped before any commit" "$log"; fi

if msg=$(GH_TOKEN=x "$here/winget-submit.sh" v9.9.9-rc1 "$r" 2>&1); then bad "a pre-release tag is refused" "$msg"; else ok "a pre-release tag is refused"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
