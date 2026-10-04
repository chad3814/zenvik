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
