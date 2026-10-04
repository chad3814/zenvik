#!/usr/bin/env bash
# Tests for scripts/macos-sign.sh. Runs on any host; the "real signing" case
# runs only on macOS with MACOS_SIGN_IDENTITY set to an identity in the
# keychain (it signs a throwaway binary, nothing in the repo).
#
#   scripts/macos-sign_test.sh
# The test bodies are single-quoted on purpose: they are expanded by the
# bash -c that run_case starts, not here.
# shellcheck disable=SC2016
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
	envs=(MACOS_SIGN_UNAME=Darwin SIGN_REQUIRED=1 MACOS_CERT_P12=c2Vrcml0 MACOS_CERT_PASSWORD=sekrit MACOS_SIGN_IDENTITY=x
		APPLE_API_KEY_P8=c2Vrcml0 APPLE_API_KEY_ID=sekrit APPLE_API_ISSUER_ID=sekrit)
	keep=()
	for e in "${envs[@]}"; do [[ $e == "$missing="* ]] || keep+=("$e"); done
	run_case "p12 without $missing fails naming it" 1 "$missing" -- "${keep[@]}" -- 'macos_sign_setup'
done

for missing in MACOS_CERT_P12 APPLE_API_KEY_P8 APPLE_API_KEY_ID APPLE_API_ISSUER_ID; do
	envs=(MACOS_SIGN_UNAME=Darwin SIGN_REQUIRED=1 MACOS_SIGN_IDENTITY=x MACOS_CERT_P12=c2Vrcml0 MACOS_CERT_PASSWORD=sekrit
		APPLE_API_KEY_P8=c2Vrcml0 APPLE_API_KEY_ID=sekrit APPLE_API_ISSUER_ID=sekrit)
	keep=()
	for e in "${envs[@]}"; do [[ $e == "$missing="* ]] || keep+=("$e"); done
	run_case "SIGN_REQUIRED without $missing fails naming it" 1 "$missing" -- "${keep[@]}" -- 'macos_sign_setup'
done

run_case "notary key decoded into the temp dir only" 0 '' -- MACOS_SIGN_UNAME=Darwin MACOS_SIGN_IDENTITY=x MACOS_SIGN_SKIP_IDENTITY_CHECK=1 \
	APPLE_API_KEY_P8=c2Vrcml0LWtleQ== APPLE_API_KEY_ID=sekrit APPLE_API_ISSUER_ID=sekrit -- \
	'macos_sign_setup; [[ $MACOS_NOTARIZE == 1 && -f $MACOS_SIGN_TMP/AuthKey.p8 ]]; t=$MACOS_SIGN_TMP; macos_sign_cleanup; [[ ! -e $t ]]'

run_case "SIGN_REQUIRED lists every missing credential at once" 1 'MACOS_CERT_P12.*APPLE_API_KEY_ID|APPLE_API_KEY_ID.*MACOS_CERT_P12' -- \
	MACOS_SIGN_UNAME=Darwin SIGN_REQUIRED=1 MACOS_SIGN_IDENTITY=x MACOS_CERT_PASSWORD=sekrit APPLE_API_KEY_P8=c2Vrcml0 APPLE_API_ISSUER_ID=sekrit -- \
	'macos_sign_setup'

if [[ $(uname -s) == Darwin ]]; then
	run_case "bad p12 fails and cleans up" 1 'import' -- MACOS_SIGN_IDENTITY=x MACOS_CERT_P12=bm90LWEtcDEy MACOS_CERT_PASSWORD=sekrit -- \
		'trap macos_sign_cleanup EXIT; macos_sign_setup'
	left=$(security list-keychains -d user | grep -c 'zenvik-sign' || true)
	if [[ $left == 0 ]]; then ok "no temporary keychain left in the search list"; else bad "temporary keychain left in the search list"; fi

	run_case "an identity that isn't in the keychain fails at setup" 1 "not found" -- \
		MACOS_SIGN_IDENTITY="Developer ID Application: Nobody (ZZZZZZZZZZ)" -- \
		'macos_sign_setup'

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
	run_case "rejected notarization fails with Apple's log" 1 'Invalid.*|not signed' -- PATH="$fake:$PATH" MACOS_SIGN_IDENTITY=x MACOS_SIGN_SKIP_IDENTITY_CHECK=1 \
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
		app="$work/Fake.app"
		mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers/libs"
		cp /usr/bin/true "$app/Contents/MacOS/fake"
		cp /usr/bin/true "$app/Contents/Helpers/tool"
		printf 'int zq(void) { return 1; }\n' >"$work/zq.c"
		cc -dynamiclib -o "$app/Contents/Helpers/libs/libzq.dylib" "$work/zq.c"
		cat >"$app/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleExecutable</key><string>fake</string><key>CFBundleIdentifier</key><string>dev.cwalker.zenvik.test</string></dict></plist>
EOF
		run_case "app helpers and their libraries are signed with the app's identity" 0 '' -- MACOS_SIGN_IDENTITY="$MACOS_SIGN_IDENTITY" -- \
			"macos_sign_setup; macos_sign '$app'; macos_verify_signature '$app'
			for f in '$app/Contents/Helpers/tool' '$app/Contents/Helpers/libs/libzq.dylib'; do
				i=\$(codesign -dvv \"\$f\" 2>&1)
				grep -qF \"Authority=\$MACOS_SIGN_IDENTITY\" <<<\"\$i\"
				grep -q 'flags=.*runtime' <<<\"\$i\"
			done; macos_sign_cleanup"
		mkdir -p "$work/dmgsrc"
		echo x >"$work/dmgsrc/file"
		hdiutil create -quiet -volname T -srcfolder "$work/dmgsrc" -format UDZO "$work/t.dmg"
		run_case "an unsigned disk image fails verification" 1 '' -- MACOS_SIGN_IDENTITY="$MACOS_SIGN_IDENTITY" -- \
			"macos_sign_setup; macos_verify_signature '$work/t.dmg'"
		run_case "a disk image is signed with the identity, without the hardened runtime" 0 '' -- MACOS_SIGN_IDENTITY="$MACOS_SIGN_IDENTITY" -- \
			"macos_sign_setup; macos_sign '$work/t.dmg'; macos_verify_signature '$work/t.dmg'
			i=\$(codesign -dvv '$work/t.dmg' 2>&1)
			grep -qF \"Authority=\$MACOS_SIGN_IDENTITY\" <<<\"\$i\"
			grep -q 'Timestamp=' <<<\"\$i\"
			if grep -q 'flags=.*runtime' <<<\"\$i\"; then echo 'image has the runtime flag'; exit 1; fi
			macos_sign_cleanup"
		rm -rf "$work"
	else
		echo "skip real signing (set MACOS_SIGN_IDENTITY to an identity in your keychain to run it)"
	fi
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
