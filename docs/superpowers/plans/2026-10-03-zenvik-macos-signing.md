# zenvik macOS Signing and Notarization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every macOS artifact in a `v*` release — `Zenvik.app` in both darwin GUI zips and the `zenvik` CLI in both darwin tarballs — is signed with the Developer ID Application certificate and notarized, so it runs with no Gatekeeper warning.

**Architecture:**
- A sourced bash library, `scripts/macos-sign.sh`, does keychain setup, signing, verification, notarization and stapling.
- `release-build.sh` (CLI) and `release-gui.sh` (GUI) call it for darwin targets, between building and packaging.
- The release workflow moves the darwin builds onto macOS jobs in a protected `release` environment that holds the secrets, with `SIGN_REQUIRED=1` so a release can't ship unsigned.

**Tech Stack:**
- bash 3.2-compatible scripts (macOS `/bin/bash`), `codesign`, `security`, `xcrun notarytool`, `xcrun stapler`, `spctl`, `ditto`, `PlistBuddy`;
- GitHub Actions;
- `shellcheck` and `actionlint` for linting.

**Spec:** `docs/superpowers/specs/2026-10-03-zenvik-macos-signing-design.md`. Task 1 brings it in line with the Plan decisions below.

## Global Constraints

- **Credential handling:** no credential value is ever printed, logged, written into the repo, or put in a command shown in output.
  - The scripts never use `set -x`.
  - Decoded key files live only in a `mktemp -d` directory that is deleted on exit.
  - Error messages name a missing variable, never a value.
- **Secrets** live in the GitHub environment `release`: `MACOS_CERT_P12` (base64 `.p12`), `MACOS_CERT_PASSWORD`, `APPLE_API_KEY_P8` (base64 `.p8`), `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`.
- **Signing identity:** the repository variable `MACOS_SIGN_IDENTITY` = `Developer ID Application: Chad Walker (SZUN8RDF5D)`.
- **Which jobs see the secrets:** only jobs with `environment: release` (the macOS release jobs) get them. Secrets are passed in `env:` on the single step that needs them.
- **Signing command:** `codesign --force --options runtime --timestamp --sign "$MACOS_SIGN_IDENTITY"`. No entitlements. A `.app` is signed inside out: first `Contents/MacOS/zenvik-gui`, then the bundle.
- **Notarization:** `xcrun notarytool submit <zip> --key <file> --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER_ID" --wait --timeout 30m`. Anything but `Accepted` prints `notarytool log <id>` and fails.
- **`SIGN_REQUIRED=1`** (set only in the release workflow) turns every "skip signing or notarizing" into a failure.
- **Artifact names, layouts, the AppleDouble guard and the combined `SHA256SUMS` are unchanged:** `zenvik-gui_<ver>_darwin_<arch>.zip` holding `zenvik-gui_<ver>_darwin_<arch>/Zenvik.app`, and `zenvik_<ver>_darwin_<arch>.tar.gz` holding `zenvik_<ver>_darwin_<arch>/zenvik`.
- **Script compatibility:** scripts run under macOS's bash 3.2, Ubuntu's bash and Git Bash, with `set -euo pipefail`. Empty arrays are expanded with `${a[@]+"${a[@]}"}`.
- **Commits:** descriptive sentence subjects, signed; if signing fails, commit unsigned and say so. Never push, tag or create a release without the user's explicit approval.

## Plan decisions (refinements of the spec; Task 1 writes them into it)

1. **Cleanup ownership.** `macos-sign.sh` installs no `trap`. It provides `macos_sign_cleanup`, which deletes the temporary keychain and key files and restores the keychain search list. Each caller runs it from its own `EXIT` trap. A second trap would replace `release-gui.sh`'s existing one, which restores `wails.json`.
2. **The GUI job splits.** A job's `environment` can't be set per matrix row, so the release workflow has `gui-darwin` (macOS matrix, `environment: release`) and `gui-other` (Windows and Linux, no environment) instead of one `gui` matrix.
3. **The CLI Gatekeeper check.** `spctl -t exec` doesn't reliably assess a bare command-line Mach-O ("does not seem to be an app").
   - In CI, the CLI's notarization proof is notarytool's `Accepted` status plus `codesign --verify`.
   - `macos_verify_gatekeeper` is used for the `.app` only.
   - After release, the downloaded CLI is run with a quarantine flag set (Task 5). Gatekeeper blocks that run unless the binary is notarized.

## Review Focus

1. **A secret present but malformed** (a wrong `.p12` password, bad base64). It must fail with a message naming the step (import or decode), never echo the value, and always remove the temporary keychain. Test: `macos-sign_test.sh` "bad p12 fails and cleans up" (Task 1).
2. **A local run with no identity** (contributors, or Linux hosts building darwin CLI targets). It must build unsigned with one clear warning, and the artifacts must be identical in layout. Test: the "no identity warns and disables" and "non-Darwin host disables" cases (Task 1), and the Linux-style dry run (Task 2).
3. **`SIGN_REQUIRED=1` with any single item missing.** It must fail before anything is built or uploaded, naming the item. Test: one case per required variable (Task 1).
4. **Notarization rejected or timed out.** The job fails, nothing is published, and the log shows Apple's reasons. Test: a `macos_notarize` case with a fake `xcrun` that reports `Invalid` (Task 1).
5. **Stapling changes the bundle after verification.** The zip must be built from the stapled app, and the signature must still verify. Test: a `codesign --verify` of the app inside the final zip, in Task 3's local check.

---

### Task 1: `scripts/macos-sign.sh` and its tests

**Files:**
- Create: `scripts/macos-sign.sh`, `scripts/macos-sign_test.sh`
- Modify: `docs/superpowers/specs/2026-10-03-zenvik-macos-signing-design.md` (Plan decisions 1–3)

**Interfaces:**
- Produces, as functions defined by sourcing `scripts/macos-sign.sh`:
  - `macos_sign_setup`: sets `MACOS_SIGN_ENABLED`, `MACOS_NOTARIZE` (each `0` or `1`) and `MACOS_SIGN_TMP`.
  - `macos_sign <path>`
  - `macos_verify_signature <path>`
  - `macos_notarize <zip>`
  - `macos_staple <app>`
  - `macos_verify_gatekeeper <app>`
  - `macos_sign_cleanup`
- Inputs (environment): `MACOS_SIGN_IDENTITY`, `MACOS_CERT_P12`, `MACOS_CERT_PASSWORD`, `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`, `SIGN_REQUIRED`, and the test-only override `MACOS_SIGN_UNAME` (defaults to `$(uname -s)`).

- [ ] **Step 1: Write the failing tests**

`scripts/macos-sign_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/macos-sign.sh. Runs on any host; the "real signing" case
# runs only on macOS with MACOS_SIGN_IDENTITY set to an identity in the
# keychain (it signs a throwaway binary, nothing in the repo).
#
#   scripts/macos-sign_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0

ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

# run_case NAME EXPECT_STATUS GREP_OUTPUT -- env assignments... -- shell code
# runs the code in a clean subshell with only the given variables set.
run_case() {
	local name=$1 want=$2 pattern=$3
	shift 3
	[[ $1 == -- ]] && shift
	local -a envs=()
	while [[ $# -gt 0 && $1 != -- ]]; do envs+=("$1"); shift; done
	shift
	local code=$1 out status
	out=$(env -i PATH="$PATH" HOME="$HOME" TMPDIR="${TMPDIR:-/tmp}" ${envs[@]+"${envs[@]}"} \
		bash -c "set -euo pipefail; source '$here/macos-sign.sh'; $code" 2>&1)
	status=$?
	if [[ $want == 0 && $status -ne 0 ]] || [[ $want != 0 && $status -eq 0 ]]; then
		bad "$name" "status $status, output: $out"
	elif [[ -n $pattern ]] && ! grep -qE "$pattern" <<<"$out"; then
		bad "$name" "output did not match /$pattern/: $out"
	elif grep -q 'sekrit' <<<"$out"; then
		bad "$name" "a secret value appeared in the output: $out"
	else
		ok "$name"
	fi
}

run_case "no identity warns and disables" 0 'warning: no signing identity' -- -- \
	'macos_sign_setup; [[ $MACOS_SIGN_ENABLED == 0 && $MACOS_NOTARIZE == 0 ]]; macos_sign_cleanup'

run_case "no identity with SIGN_REQUIRED fails" 1 'MACOS_SIGN_IDENTITY' -- SIGN_REQUIRED=1 -- \
	'macos_sign_setup'

run_case "non-Darwin host disables" 0 'warning: .*not macOS' -- MACOS_SIGN_UNAME=Linux MACOS_SIGN_IDENTITY=x -- \
	'macos_sign_setup; [[ $MACOS_SIGN_ENABLED == 0 ]]; macos_sign_cleanup'

run_case "non-Darwin host with SIGN_REQUIRED fails" 1 'not macOS' -- MACOS_SIGN_UNAME=Linux SIGN_REQUIRED=1 MACOS_SIGN_IDENTITY=x -- \
	'macos_sign_setup'

for missing in MACOS_CERT_PASSWORD MACOS_SIGN_IDENTITY; do
	envs=(MACOS_SIGN_UNAME=Darwin SIGN_REQUIRED=1 MACOS_CERT_P12=c2Vrcml0 MACOS_CERT_PASSWORD=sekrit MACOS_SIGN_IDENTITY=x)
	keep=()
	for e in "${envs[@]}"; do [[ $e == "$missing="* ]] || keep+=("$e"); done
	run_case "p12 without $missing fails naming it" 1 "$missing" -- "${keep[@]}" -- 'macos_sign_setup'
done

for missing in APPLE_API_KEY_P8 APPLE_API_KEY_ID APPLE_API_ISSUER_ID; do
	envs=(MACOS_SIGN_UNAME=Darwin SIGN_REQUIRED=1 MACOS_SIGN_IDENTITY=x APPLE_API_KEY_P8=c2Vrcml0 APPLE_API_KEY_ID=sekrit APPLE_API_ISSUER_ID=sekrit)
	keep=()
	for e in "${envs[@]}"; do [[ $e == "$missing="* ]] || keep+=("$e"); done
	run_case "notary without $missing fails naming it" 1 "$missing" -- "${keep[@]}" -- 'macos_sign_setup'
done

run_case "notary key decoded into the temp dir only" 0 '' -- MACOS_SIGN_UNAME=Darwin MACOS_SIGN_IDENTITY=x \
	APPLE_API_KEY_P8=c2Vrcml0LWtleQ== APPLE_API_KEY_ID=sekrit APPLE_API_ISSUER_ID=sekrit -- \
	'macos_sign_setup; [[ $MACOS_NOTARIZE == 1 && -f $MACOS_SIGN_TMP/AuthKey.p8 ]]; t=$MACOS_SIGN_TMP; macos_sign_cleanup; [[ ! -e $t ]]'

if [[ $(uname -s) == Darwin ]]; then
	run_case "bad p12 fails and cleans up" 1 'import' -- MACOS_SIGN_IDENTITY=x MACOS_CERT_P12=bm90LWEtcDEy MACOS_CERT_PASSWORD=sekrit -- \
		'trap macos_sign_cleanup EXIT; macos_sign_setup'
	left=$(security list-keychains -d user | grep -c 'zenvik-sign' || true)
	if [[ $left == 0 ]]; then ok "no temporary keychain left in the search list"; else bad "temporary keychain left in the search list"; fi

	fake=$(mktemp -d)
	cat >"$fake/xcrun" <<'EOF'
#!/bin/bash
if [[ $1 == notarytool && $2 == submit ]]; then
	printf '<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>id</key><string>abc-123</string><key>status</key><string>Invalid</string></dict></plist>\n'
	exit 0
fi
if [[ $1 == notarytool && $2 == log ]]; then echo '{"issues":[{"message":"The binary is not signed."}]}'; exit 0; fi
exit 1
EOF
	chmod +x "$fake/xcrun"
	echo x >"$fake/a.zip"
	run_case "rejected notarization fails with Apple's log" 1 'Invalid.*|not signed' -- PATH="$fake:$PATH" MACOS_SIGN_IDENTITY=x \
		APPLE_API_KEY_P8=c2Vrcml0 APPLE_API_KEY_ID=sekrit APPLE_API_ISSUER_ID=sekrit -- \
		"trap macos_sign_cleanup EXIT; macos_sign_setup; macos_notarize '$fake/a.zip'"
	rm -rf "$fake"

	if [[ -n ${MACOS_SIGN_IDENTITY:-} ]]; then
		work=$(mktemp -d)
		cp /usr/bin/true "$work/tool"
		run_case "real identity signs and verifies with the hardened runtime" 0 '' -- MACOS_SIGN_IDENTITY="$MACOS_SIGN_IDENTITY" -- \
			"macos_sign_setup; macos_sign '$work/tool'; macos_verify_signature '$work/tool'; macos_sign_cleanup"
		printf 'x' >>"$work/tool"
		run_case "tampered binary fails verification" 1 '' -- MACOS_SIGN_IDENTITY="$MACOS_SIGN_IDENTITY" -- \
			"macos_sign_setup; macos_verify_signature '$work/tool'"
		rm -rf "$work"
	else
		echo "skip real signing (set MACOS_SIGN_IDENTITY to an identity in your keychain to run it)"
	fi
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

```bash
chmod 755 scripts/macos-sign_test.sh
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `scripts/macos-sign_test.sh`
Expected: every case FAILs, because `scripts/macos-sign.sh` does not exist (the output shows `No such file or directory`), and the final line reports `0 passed`.

- [ ] **Step 3: Write `scripts/macos-sign.sh`**

```bash
#!/usr/bin/env bash
# macOS code signing and notarization for the release scripts. Source it:
#
#   source "$root/scripts/macos-sign.sh"
#   trap macos_sign_cleanup EXIT   # or call it from your own EXIT trap
#   macos_sign_setup
#   macos_sign "$path"; macos_verify_signature "$path"
#   macos_notarize "$zip"; macos_staple "$app"; macos_verify_gatekeeper "$app"
#
# Inputs (environment):
#   MACOS_SIGN_IDENTITY   "Developer ID Application: Name (TEAMID)"
#   MACOS_CERT_P12        base64 .p12 (CI); without it the identity comes
#                         from your own keychain (local runs)
#   MACOS_CERT_PASSWORD   the .p12's password
#   APPLE_API_KEY_P8      base64 App Store Connect API key (.p8)
#   APPLE_API_KEY_ID, APPLE_API_ISSUER_ID
#   SIGN_REQUIRED=1       fail instead of skipping signing or notarization
#
# Never prints a credential value; error messages name the missing variable.
# Decoded key material lives only in $MACOS_SIGN_TMP, which
# macos_sign_cleanup removes.

MACOS_SIGN_ENABLED=0
MACOS_NOTARIZE=0
MACOS_SIGN_TMP=""
MACOS_SIGN_KEYCHAIN=""
MACOS_SIGN_OLD_KEYCHAINS=()

_ms_warn() { echo "macos-sign: warning: $*" >&2; }
_ms_die() { echo "macos-sign: $*" >&2; return 1; }

# _ms_skip_or_fail explains why signing or notarization is skipped; with
# SIGN_REQUIRED=1 that is an error.
_ms_skip_or_fail() {
	if [[ ${SIGN_REQUIRED:-} == 1 ]]; then
		_ms_die "$* (SIGN_REQUIRED=1)"
		return 1
	fi
	_ms_warn "$*"
}

_ms_need() {
	local v
	for v in "$@"; do
		if [[ -z ${!v:-} ]]; then
			_ms_die "$v is not set"
			return 1
		fi
	done
}

macos_sign_setup() {
	MACOS_SIGN_TMP=$(mktemp -d "${TMPDIR:-/tmp}/zenvik-sign.XXXXXX")
	if [[ ${MACOS_SIGN_UNAME:-$(uname -s)} != Darwin ]]; then
		_ms_skip_or_fail "this host is not macOS; darwin builds will be unsigned" || return 1
		return 0
	fi

	if [[ -n ${MACOS_CERT_P12:-} ]]; then
		_ms_need MACOS_CERT_PASSWORD MACOS_SIGN_IDENTITY || return 1
		local kcpass
		kcpass=$(openssl rand -hex 24)
		MACOS_SIGN_KEYCHAIN="$MACOS_SIGN_TMP/zenvik-sign.keychain-db"
		security create-keychain -p "$kcpass" "$MACOS_SIGN_KEYCHAIN" >/dev/null
		security set-keychain-settings -lut 21600 "$MACOS_SIGN_KEYCHAIN"
		security unlock-keychain -p "$kcpass" "$MACOS_SIGN_KEYCHAIN"
		if ! printf '%s' "$MACOS_CERT_P12" | base64 --decode >"$MACOS_SIGN_TMP/cert.p12" 2>/dev/null; then
			_ms_die "MACOS_CERT_P12 is not valid base64"
			return 1
		fi
		if ! security import "$MACOS_SIGN_TMP/cert.p12" -k "$MACOS_SIGN_KEYCHAIN" \
			-P "$MACOS_CERT_PASSWORD" -T /usr/bin/codesign >/dev/null 2>&1; then
			rm -f "$MACOS_SIGN_TMP/cert.p12"
			_ms_die "couldn't import MACOS_CERT_P12 (wrong MACOS_CERT_PASSWORD, or not a .p12)"
			return 1
		fi
		rm -f "$MACOS_SIGN_TMP/cert.p12"
		security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$kcpass" \
			"$MACOS_SIGN_KEYCHAIN" >/dev/null
		local line
		while IFS= read -r line; do
			line=${line#"${line%%[![:space:]]*}"}
			line=${line#\"}
			line=${line%\"}
			if [[ -n $line ]]; then MACOS_SIGN_OLD_KEYCHAINS+=("$line"); fi
		done < <(security list-keychains -d user)
		security list-keychains -d user -s "$MACOS_SIGN_KEYCHAIN" \
			${MACOS_SIGN_OLD_KEYCHAINS[@]+"${MACOS_SIGN_OLD_KEYCHAINS[@]}"}
		MACOS_SIGN_ENABLED=1
	elif [[ -n ${MACOS_SIGN_IDENTITY:-} ]]; then
		MACOS_SIGN_ENABLED=1 # from the user's own keychain
	else
		_ms_skip_or_fail "no signing identity (set MACOS_SIGN_IDENTITY); darwin builds will be unsigned" || return 1
		return 0
	fi

	if [[ -n ${APPLE_API_KEY_P8:-}${APPLE_API_KEY_ID:-}${APPLE_API_ISSUER_ID:-} || ${SIGN_REQUIRED:-} == 1 ]]; then
		_ms_need APPLE_API_KEY_P8 APPLE_API_KEY_ID APPLE_API_ISSUER_ID || return 1
		if ! printf '%s' "$APPLE_API_KEY_P8" | base64 --decode >"$MACOS_SIGN_TMP/AuthKey.p8" 2>/dev/null; then
			_ms_die "APPLE_API_KEY_P8 is not valid base64"
			return 1
		fi
		chmod 600 "$MACOS_SIGN_TMP/AuthKey.p8"
		MACOS_NOTARIZE=1
	else
		_ms_warn "no notary API key (APPLE_API_KEY_*); builds will be signed but not notarized"
	fi
}

macos_sign() {
	local path=$1
	[[ $MACOS_SIGN_ENABLED == 1 ]] || return 0
	local -a kc=()
	[[ -n $MACOS_SIGN_KEYCHAIN ]] && kc=(--keychain "$MACOS_SIGN_KEYCHAIN")
	if [[ -d $path && $path == *.app ]]; then
		local exe
		for exe in "$path"/Contents/MacOS/*; do
			codesign --force --options runtime --timestamp ${kc[@]+"${kc[@]}"} \
				--sign "$MACOS_SIGN_IDENTITY" "$exe"
		done
	fi
	codesign --force --options runtime --timestamp ${kc[@]+"${kc[@]}"} \
		--sign "$MACOS_SIGN_IDENTITY" "$path"
}

macos_verify_signature() {
	local path=$1
	[[ $MACOS_SIGN_ENABLED == 1 ]] || return 0
	codesign --verify --strict --deep --verbose=2 "$path"
	local info
	info=$(codesign -dvv "$path" 2>&1)
	if ! grep -q 'flags=.*runtime' <<<"$info"; then
		_ms_die "$path is not signed with the hardened runtime"
		return 1
	fi
	if ! grep -qF "Authority=$MACOS_SIGN_IDENTITY" <<<"$info"; then
		_ms_die "$path is not signed by $MACOS_SIGN_IDENTITY"
		return 1
	fi
}

macos_notarize() {
	local zip=$1
	if [[ $MACOS_NOTARIZE != 1 ]]; then
		[[ $MACOS_SIGN_ENABLED == 1 ]] && echo "macos-sign: skipping notarization of $(basename "$zip")" >&2
		return 0
	fi
	echo "macos-sign: notarizing $(basename "$zip") (this can take a few minutes)" >&2
	local result="$MACOS_SIGN_TMP/notary.plist"
	xcrun notarytool submit "$zip" --key "$MACOS_SIGN_TMP/AuthKey.p8" \
		--key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER_ID" \
		--wait --timeout 30m --output-format plist >"$result" || true
	local id status
	id=$(/usr/libexec/PlistBuddy -c 'Print :id' "$result" 2>/dev/null || true)
	status=$(/usr/libexec/PlistBuddy -c 'Print :status' "$result" 2>/dev/null || true)
	if [[ $status != Accepted ]]; then
		echo "macos-sign: notarization of $(basename "$zip") ended with status '${status:-unknown}' (submission ${id:-unknown})" >&2
		if [[ -n $id ]]; then
			xcrun notarytool log "$id" --key "$MACOS_SIGN_TMP/AuthKey.p8" \
				--key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER_ID" >&2 || true
		fi
		return 1
	fi
	echo "macos-sign: $(basename "$zip") accepted (submission $id)" >&2
}

macos_staple() {
	local app=$1
	[[ $MACOS_NOTARIZE == 1 ]] || return 0
	xcrun stapler staple "$app"
	xcrun stapler validate "$app"
}

macos_verify_gatekeeper() {
	local app=$1
	[[ $MACOS_NOTARIZE == 1 ]] || return 0
	local out
	out=$(spctl -a -vv -t exec "$app" 2>&1 || true)
	if ! grep -q 'source=Notarized Developer ID' <<<"$out"; then
		_ms_die "Gatekeeper does not accept $app: $out"
		return 1
	fi
}

macos_sign_cleanup() {
	if [[ -n $MACOS_SIGN_KEYCHAIN ]]; then
		security list-keychains -d user -s ${MACOS_SIGN_OLD_KEYCHAINS[@]+"${MACOS_SIGN_OLD_KEYCHAINS[@]}"} 2>/dev/null || true
		security delete-keychain "$MACOS_SIGN_KEYCHAIN" 2>/dev/null || true
		MACOS_SIGN_KEYCHAIN=""
	fi
	if [[ -n $MACOS_SIGN_TMP ]]; then
		rm -rf "$MACOS_SIGN_TMP"
		MACOS_SIGN_TMP=""
	fi
}
```

- [ ] **Step 4: Run the tests and shellcheck**

Run:

```bash
shellcheck scripts/macos-sign.sh scripts/macos-sign_test.sh
scripts/macos-sign_test.sh
MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/macos-sign_test.sh
```

Expected:
- `shellcheck` prints nothing.
- Both test runs end with `N passed, 0 failed`, and the second also includes the two "real identity" cases.
- Signing with the login-keychain identity for the first time may show a macOS prompt asking to allow `codesign` to use the key. The user clicks **Always Allow**; if no one is there to click, say so in the report.

- [ ] **Step 5: Write Plan decisions 1–3 into the spec**

In `docs/superpowers/specs/2026-10-03-zenvik-macos-signing-design.md`:
- **§3.1:** replace the single `gui` matrix with `gui-darwin` (both macOS legs, `environment: release`) and `gui-other` (Windows and Linux, no environment). `publish` needs `[cli, cli-darwin, gui-darwin, gui-other]`.
- **§3.2 `macos_sign_setup`:** replace "installs a trap" with "callers run `macos_sign_cleanup` from their own EXIT trap".
- **§3.2 / §3.3 / §4:** state that `macos_verify_gatekeeper` is for the `.app`. The CLI's CI proof is the `Accepted` status plus `codesign --verify`, and the published CLI is checked by running it with a quarantine flag set.

- [ ] **Step 6: Commit**

```bash
git add scripts/macos-sign.sh scripts/macos-sign_test.sh docs/superpowers/specs/2026-10-03-zenvik-macos-signing-design.md
git commit -m "Add macos-sign.sh: keychain setup, signing, notarization and stapling for the release scripts, with tests"
```

---

### Task 2: Sign and notarize the darwin CLI in `release-build.sh`

**Files:**
- Modify: `scripts/release-build.sh`

**Interfaces:**
- Consumes (Task 1): `macos_sign_setup`, `macos_sign`, `macos_verify_signature`, `macos_notarize`, `macos_sign_cleanup`, `MACOS_SIGN_ENABLED`, `MACOS_SIGN_TMP`.
- Produces: `scripts/release-build.sh vX.Y.Z [os/arch …]`. With no targets it builds all four, as before.

- [ ] **Step 1: Show the gap (red)**

Run: `scripts/release-build.sh v0.0.0-test darwin/arm64; echo "exit $?"`
Expected: `usage: … vX.Y.Z` and `exit 2`, because the script doesn't accept a target list yet.

- [ ] **Step 2: Change `release-build.sh`**

Replace lines 1–23 (the header, argument handling and target list) with:

```bash
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
```

Inside the loop, replace the `go build` line and the line after it (`cp "$root/README.md" …`) with:

```bash
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
```

Leave the packaging (`tar`/`zip`), the `SHA256SUMS` step and the final listing unchanged.

- [ ] **Step 3: Check it locally (green)**

Run:

```bash
shellcheck scripts/release-build.sh
scripts/release-build.sh v0.0.0-test linux/amd64 && ls dist
MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-build.sh v0.0.0-test darwin/arm64 darwin/amd64
mkdir -p /tmp/zcli && tar -C /tmp/zcli -xzf dist/zenvik_0.0.0-test_darwin_amd64.tar.gz
codesign -dvv /tmp/zcli/zenvik_0.0.0-test_darwin_amd64/zenvik 2>&1 | grep -E 'Authority=Developer ID Application|flags='
file /tmp/zcli/zenvik_0.0.0-test_darwin_amd64/zenvik
MACOS_SIGN_UNAME=Linux scripts/release-build.sh v0.0.0-test darwin/arm64 2>&1 | grep 'not macOS'
SIGN_REQUIRED=1 scripts/release-build.sh v0.0.0-test darwin/arm64; echo "exit $?"
rm -rf /tmp/zcli dist
```

Expected:
- `shellcheck` is silent.
- The linux run writes `zenvik_0.0.0-test_linux_amd64.tar.gz` and `SHA256SUMS` with no signing output.
- The signed darwin run prints the warning "signed but not notarized" (no API key is set locally). It writes both tarballs.
- `codesign -dvv` shows `Authority=Developer ID Application: Chad Walker (SZUN8RDF5D)` and `flags=0x10000(runtime)`.
- `file` reports `Mach-O 64-bit executable x86_64`.
- The `MACOS_SIGN_UNAME=Linux` run warns that the host is not macOS.
- The `SIGN_REQUIRED=1` run (with no identity in the environment) fails before building, names `MACOS_SIGN_IDENTITY` and mentions `SIGN_REQUIRED=1`, with `exit 1`.

- [ ] **Step 4: Commit**

```bash
git add scripts/release-build.sh
git commit -m "release-build.sh: take an optional target list, and sign and notarize the darwin CLI on macOS"
```

---

### Task 3: Sign, notarize and staple `Zenvik.app` in `release-gui.sh`

**Files:**
- Modify: `scripts/release-gui.sh`

**Interfaces:**
- Consumes (Task 1): `macos_sign_setup`, `macos_sign`, `macos_verify_signature`, `macos_notarize`, `macos_staple`, `macos_verify_gatekeeper`, `macos_sign_cleanup`, `MACOS_SIGN_ENABLED`, `MACOS_SIGN_TMP`.

- [ ] **Step 1: Show the gap (red)**

Run:

```bash
MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-gui.sh v0.0.0-test darwin/arm64
mkdir -p /tmp/zgui && unzip -q -o dist/zenvik-gui_0.0.0-test_darwin_arm64.zip -d /tmp/zgui
codesign -dvv /tmp/zgui/zenvik-gui_0.0.0-test_darwin_arm64/Zenvik.app 2>&1 | grep -E 'Authority|Signature='
rm -rf /tmp/zgui dist
```

Expected: `Signature=adhoc` and no `Authority=Developer ID…` line. Wails ad-hoc signs the app, which is why Gatekeeper rejects it today.

- [ ] **Step 2: Change `release-gui.sh`**

The existing `restore` (lines 40–44) also has to clean up signing. Replace lines 40–45 with:

```bash
restore() {
	cp "$saved" "$wailsjson"
	rm -f "$saved"
	touch "$root/gui/frontend/dist/gitkeep"
	if declare -F macos_sign_cleanup >/dev/null; then
		macos_sign_cleanup
	fi
}
trap restore EXIT
```

Directly after the `if [[ $goos == darwin ]]; then … fi` block that sets the deployment target (line 63), add:

```bash
if [[ $goos == darwin ]]; then
	# shellcheck source=scripts/macos-sign.sh
	source "$root/scripts/macos-sign.sh"
	macos_sign_setup
fi
```

In the `darwin)` case, replace `cp -R "$bin/Zenvik.app" "$stage/"` with:

```bash
	app="$bin/Zenvik.app"
	if [[ $MACOS_SIGN_ENABLED == 1 ]]; then
		macos_sign "$app"
		macos_verify_signature "$app"
		ditto -c -k --keepParent "$app" "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_notarize "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_staple "$app"
		macos_verify_signature "$app"
		macos_verify_gatekeeper "$app"
	fi
	cp -R "$app" "$stage/"
```

Leave the `ditto` packaging line and the AppleDouble guard exactly as they are. The zip is now built from the signed and stapled app.

- [ ] **Step 3: Check it locally (green)**

Run:

```bash
shellcheck scripts/release-gui.sh
MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-gui.sh v0.0.0-test darwin/arm64
mkdir -p /tmp/zgui && unzip -q -o dist/zenvik-gui_0.0.0-test_darwin_arm64.zip -d /tmp/zgui
A=/tmp/zgui/zenvik-gui_0.0.0-test_darwin_arm64/Zenvik.app
codesign --verify --strict --deep --verbose=2 "$A"
codesign -dvv "$A" 2>&1 | grep -E 'Authority=Developer ID Application|flags=|Identifier='
codesign -dvv "$A/Contents/MacOS/zenvik-gui" 2>&1 | grep -E 'Authority=Developer ID Application|flags='
spctl -a -vv -t exec "$A" 2>&1 | tail -2
git status --short
rm -rf /tmp/zgui dist
```

Expected:
- `shellcheck` is silent.
- The build prints the "signed but not notarized" warning.
- `codesign --verify` prints `valid on disk` and `satisfies its Designated Requirement`.
- Both the bundle and its executable show `Authority=Developer ID Application: Chad Walker (SZUN8RDF5D)` and `flags=0x10000(runtime)`. The bundle shows `Identifier=dev.cwalker.zenvik`.
- `spctl` reports `rejected` with `source=Unnotarized Developer ID`, which is expected locally.
- `git status` is clean: `wails.json` and `gitkeep` are restored, and no keychain or key files are left.

- [ ] **Step 4: Commit**

```bash
git add scripts/release-gui.sh
git commit -m "release-gui.sh: sign, notarize and staple Zenvik.app before zipping it"
```

---

### Task 4: Release workflow and docs

**Files:**
- Modify: `.github/workflows/release.yml`, `README.md`, `gui/README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: `scripts/release-build.sh vX.Y.Z [os/arch …]` (Task 2) and `scripts/release-gui.sh` (Task 3), both reading the Task 1 environment.

- [ ] **Step 1: Rewrite `.github/workflows/release.yml`**

```yaml
name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  cli:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...
      - run: scripts/release-build.sh "$GITHUB_REF_NAME" linux/amd64 windows/amd64
      - uses: actions/upload-artifact@v4
        with:
          name: cli
          path: |
            dist/*.tar.gz
            dist/*.zip

  cli-darwin:
    runs-on: macos-latest
    environment: release
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - name: Build, sign and notarize the darwin CLI
        env:
          SIGN_REQUIRED: "1"
          MACOS_SIGN_IDENTITY: ${{ vars.MACOS_SIGN_IDENTITY }}
          MACOS_CERT_P12: ${{ secrets.MACOS_CERT_P12 }}
          MACOS_CERT_PASSWORD: ${{ secrets.MACOS_CERT_PASSWORD }}
          APPLE_API_KEY_P8: ${{ secrets.APPLE_API_KEY_P8 }}
          APPLE_API_KEY_ID: ${{ secrets.APPLE_API_KEY_ID }}
          APPLE_API_ISSUER_ID: ${{ secrets.APPLE_API_ISSUER_ID }}
        run: scripts/release-build.sh "$GITHUB_REF_NAME" darwin/arm64 darwin/amd64
      - uses: actions/upload-artifact@v4
        with:
          name: cli-darwin
          path: dist/*.tar.gz

  gui-darwin:
    strategy:
      fail-fast: true
      matrix:
        target: [darwin/arm64, darwin/amd64]
    runs-on: macos-latest
    environment: release
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: gui/go.mod
          cache-dependency-path: gui/go.sum
      - uses: actions/setup-node@v5
        with:
          node-version-file: gui/frontend/.nvmrc
          cache: npm
          cache-dependency-path: gui/frontend/package-lock.json
      - name: Build, sign, notarize and staple Zenvik.app
        env:
          SIGN_REQUIRED: "1"
          MACOS_SIGN_IDENTITY: ${{ vars.MACOS_SIGN_IDENTITY }}
          MACOS_CERT_P12: ${{ secrets.MACOS_CERT_P12 }}
          MACOS_CERT_PASSWORD: ${{ secrets.MACOS_CERT_PASSWORD }}
          APPLE_API_KEY_P8: ${{ secrets.APPLE_API_KEY_P8 }}
          APPLE_API_KEY_ID: ${{ secrets.APPLE_API_KEY_ID }}
          APPLE_API_ISSUER_ID: ${{ secrets.APPLE_API_ISSUER_ID }}
        run: scripts/release-gui.sh "$GITHUB_REF_NAME" "${{ matrix.target }}"
      - uses: actions/upload-artifact@v4
        with:
          name: gui-darwin-${{ strategy.job-index }}
          path: dist/zenvik-gui_*

  gui-other:
    strategy:
      fail-fast: true
      matrix:
        include:
          - { os: windows-latest, target: windows/amd64 }
          - { os: ubuntu-latest, target: linux/amd64 }
    runs-on: ${{ matrix.os }}
    defaults:
      run:
        shell: bash
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: gui/go.mod
          cache-dependency-path: gui/go.sum
      - uses: actions/setup-node@v5
        with:
          node-version-file: gui/frontend/.nvmrc
          cache: npm
          cache-dependency-path: gui/frontend/package-lock.json
      - if: runner.os == 'Linux'
        run: sudo apt-get update && sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
      - run: scripts/release-gui.sh "$GITHUB_REF_NAME" "${{ matrix.target }}"
      - uses: actions/upload-artifact@v4
        with:
          name: gui-other-${{ strategy.job-index }}
          path: dist/zenvik-gui_*

  publish:
    needs: [cli, cli-darwin, gui-darwin, gui-other]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/download-artifact@v5
        with:
          path: dist
          merge-multiple: true
      - run: cd dist && sha256sum ./*.tar.gz ./*.zip | sed 's| \./| |' > SHA256SUMS && cat SHA256SUMS
      - name: Create the GitHub release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          flags=(--generate-notes --verify-tag)
          if [[ $GITHUB_REF_NAME == *-* ]]; then
            flags+=(--prerelease)
          fi
          gh release create "$GITHUB_REF_NAME" "${flags[@]}" dist/*.tar.gz dist/*.zip dist/SHA256SUMS
```

- [ ] **Step 2: Lint the workflow and both scripts**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml .github/workflows/ci.yml && shellcheck scripts/*.sh`
Expected: no output (exit 0). `actionlint` runs shellcheck on the `run:` blocks too, now that shellcheck is installed.

- [ ] **Step 3: Update the docs**

In `README.md`, replace the paragraph starting "The macOS binaries are not signed or notarized" (it runs through the `xattr -d com.apple.quarantine zenvik` code block) with:

```markdown
The macOS binaries are signed with a Developer ID and notarized by Apple, so they run without a Gatekeeper prompt.
```

In the same file's "Desktop app" paragraph, replace `; the macOS app isn't signed yet.` with `. The macOS app is signed and notarized.`

In `gui/README.md`, replace the line starting "The macOS app is not signed yet" with:

```markdown
The macOS app is signed with a Developer ID and notarized by Apple (macOS 13 or later).
```

In `CLAUDE.md`, add after the "GUI (separate module…)" bullet:

```markdown
- macOS signing (`scripts/macos-sign.sh`, tested by `scripts/macos-sign_test.sh`): the release workflow's macOS jobs run in the GitHub environment `release` (deployable only from `v*` tags) with secrets `MACOS_CERT_P12`, `MACOS_CERT_PASSWORD`, `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`, the repository variable `MACOS_SIGN_IDENTITY`, and `SIGN_REQUIRED=1` (missing credentials fail the release). Local signed build (no notarization): `MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-gui.sh vX.Y.Z darwin/arm64`. Never print, log or commit credential values.
```

In the same file, update the release-archives bullet:
- It should say the darwin CLI and GUI builds run on macOS and are signed and notarized.
- It should say `release-build.sh` takes an optional `os/arch …` target list.

- [ ] **Step 4: Run the full suites**

Run:

```bash
CGO_ENABLED=0 go build ./... && go vet ./... && go test -race ./...
(cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/))
scripts/macos-sign_test.sh
git status --short
```

Expected: everything passes, `macos-sign_test.sh` ends `0 failed`, and the tree is clean apart from this task's files.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml README.md gui/README.md CLAUDE.md
git commit -m "Release workflow: sign and notarize the darwin CLI and GUI in the protected release environment; docs"
```

---

### Task 5: First signed release, `v1.1.0-rc2` (with the user)

**Files:** none. Every step that is outward-facing needs the user's explicit go-ahead first: merging to `main`, pushing, tagging, and the release itself.

- [ ] **Step 1: Ask the user** to merge `feat/macos-signing` into `main`, push, and tag `v1.1.0-rc2`. Wait for a yes, then do it: a fast-forward or merge commit, all suites green on the merged result, a signed tag on the merged head, and push the branch and the tag.

- [ ] **Step 2: Watch the release run**

Run: `gh run watch <release run id> --exit-status`
Expected:
- All jobs succeed: `cli`, `cli-darwin`, `gui-darwin` ×2, `gui-other` ×2 and `publish`.
- The `cli-darwin` and `gui-darwin` logs show `accepted (submission …)` for each submission.

If a job fails, the log carries `notarytool log` output. Fix it on a branch, then re-tag with `v1.1.0-rc3`; never move an existing tag.

- [ ] **Step 3: Verify the published downloads**

```bash
cd "$(mktemp -d)" && gh release download v1.1.0-rc2 -R chad3814/zenvik -p SHA256SUMS -p '*darwin*'
shasum -a 256 -c --ignore-missing SHA256SUMS
for a in arm64 amd64; do
  unzip -q "zenvik-gui_1.1.0-rc2_darwin_$a.zip"
  A="zenvik-gui_1.1.0-rc2_darwin_$a/Zenvik.app"
  spctl -a -vv -t exec "$A" 2>&1 | tail -2
  xcrun stapler validate "$A"
  tar xzf "zenvik_1.1.0-rc2_darwin_$a.tar.gz"
  codesign -dvv "zenvik_1.1.0-rc2_darwin_$a/zenvik" 2>&1 | grep -E 'Authority=Developer ID Application|flags='
done
B=zenvik_1.1.0-rc2_darwin_arm64/zenvik
xattr -w com.apple.quarantine "0081;$(printf %x "$(date +%s)");Safari;" "$B" && "$B" --version
```

Expected:
- Every checksum is OK.
- Both apps show `accepted` and `source=Notarized Developer ID`, and `stapler validate` reports `The validate action worked!`.
- Both CLI binaries show the Developer ID authority and the `runtime` flag.
- The quarantined arm64 CLI prints `zenvik v1.1.0-rc2` with no Gatekeeper dialog.

- [ ] **Step 4: The user's check.** The user downloads the arm64 GUI zip in a browser, unzips it and double-clicks `Zenvik.app`. It must open with no "unidentified developer" warning, at most the standard "downloaded from the Internet" confirmation. Record the result.
