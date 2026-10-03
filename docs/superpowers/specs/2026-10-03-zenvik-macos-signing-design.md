# zenvik macOS signing and notarization — design

**Status:** approved in conversation on 2026-10-03 (sections 1–3). Written for review.
**Builds on:**
- `scripts/release-build.sh` (CLI archives), `scripts/release-gui.sh` (GUI archives) and `.github/workflows/release.yml`, as they stand at `8b7e5ba`;
- `2026-10-02-zenvik-gui-design.md` §6 (build and release).

## 1. Goal and scope

Every macOS artifact in a `v*` release is signed with the account's **Developer ID Application** certificate and notarized by Apple, so it opens and runs with no Gatekeeper warning and no `xattr` workaround.

**In scope:**
- `Zenvik.app` in `zenvik-gui_<ver>_darwin_arm64.zip` and `zenvik-gui_<ver>_darwin_amd64.zip`;
- the `zenvik` CLI binary in `zenvik_<ver>_darwin_arm64.tar.gz` and `zenvik_<ver>_darwin_amd64.tar.gz`.

**Out of scope:**
- Windows code signing (needs a different certificate);
- a `.dmg` installer (zips stay);
- Homebrew;
- auto-updates.

## 2. Credentials (one-time setup, done by the user, 2026-10-03)

No credential value ever passes through an assistant conversation, a log, or a file in the repo.

| Item | Where | Kind |
|---|---|---|
| Developer ID Application certificate + private key | the user's login keychain; exported once as `.p12` (strong password) for CI, then deleted locally | — |
| `MACOS_CERT_P12` | GitHub environment `release` | secret: base64 of the `.p12` |
| `MACOS_CERT_PASSWORD` | environment `release` | secret |
| App Store Connect **Team** API key (role Developer), `.p8` downloaded once, then deleted locally | — | — |
| `APPLE_API_KEY_P8` | environment `release` | secret: base64 of the `.p8` |
| `APPLE_API_KEY_ID` | environment `release` | secret |
| `APPLE_API_ISSUER_ID` | environment `release` | secret |
| `MACOS_SIGN_IDENTITY` = `Developer ID Application: Chad Walker (SZUN8RDF5D)` | repository variable | not secret |

- **Access control:** the `release` environment allows deployments only from tags matching `v*`. Only release-workflow jobs that set `environment: release` can read these secrets. Ordinary CI, pull requests and forks can't.
- **If the certificate is compromised:** revoke it on developer.apple.com. Revoke the API key in App Store Connect.

## 3. Release workflow changes

### 3.1 Jobs

- **`cli` (ubuntu-latest).** It builds the `linux/amd64` and `windows/amd64` CLI archives only.
  - `scripts/release-build.sh vX.Y.Z [os/arch …]` takes an optional target list. With no list it builds all four, as today, so local use is unchanged.
- **`cli-darwin` (new, macos-latest, `environment: release`).** It builds `darwin/arm64` and `darwin/amd64` with `release-build.sh`, then signs and notarizes them (§3.3).
- **`gui` matrix.**
  - The two `macos-latest` legs get `environment: release` and sign and notarize (§3.2).
  - The Windows and Linux legs don't use the environment, so they can't read the secrets.
- **`publish`.** It now `needs: [cli, cli-darwin, gui]`. It still makes one combined `SHA256SUMS` and uses the same `gh release create` flags.
- **`SIGN_REQUIRED=1`.** It is set on both macOS jobs, so a missing secret fails the release instead of shipping it unsigned.

### 3.2 `scripts/macos-sign.sh` (new; macOS only)

It is a library script sourced by `release-build.sh` and `release-gui.sh`. It provides:

- **`macos_sign_setup`**
  - With `MACOS_CERT_P12` set:
    - creates a temporary keychain with a random password and imports the `.p12` into it;
    - allows `codesign` to use the key without a prompt (`security set-key-partition-list`);
    - puts the keychain first in the search list;
    - installs a `trap` that deletes the keychain and the decoded key files on exit, whether the job succeeds or fails.
  - Without it: signs with `MACOS_SIGN_IDENTITY` from the user's own keychain. This is how local runs work.
  - With no identity at all: skips signing and warns. If `SIGN_REQUIRED=1`, it fails instead.
- **`macos_sign <path>`**
  - Runs `codesign --force --options runtime --timestamp --sign "$MACOS_SIGN_IDENTITY" <path>`.
  - A `.app` is signed inside out: first `Contents/MacOS/zenvik-gui`, then the bundle.
  - No entitlements file is used. Zenvik isn't sandboxed, the hardened runtime still lets it spawn `mkvmerge` and `hdiutil`, and Wails' WKWebView needs no JIT entitlement.
- **`macos_verify_signature <path>`**
  - Runs `codesign --verify --strict --verbose=2`.
  - Checks that `codesign -dvv` shows the `runtime` flag and the expected `Authority=Developer ID Application: …`.
- **`macos_notarize <submission.zip>`**
  - Runs only when `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID` and `APPLE_API_ISSUER_ID` are all set. Otherwise it skips with a warning, or fails if `SIGN_REQUIRED=1`.
  - Decodes the key to a file inside the temporary directory.
  - Runs `xcrun notarytool submit <zip> --key … --key-id … --issuer … --wait --timeout 30m`.
  - If the status isn't `Accepted`, it prints `xcrun notarytool log <submission-id>` and fails.
- **`macos_staple <app>`** runs `xcrun stapler staple` and then `xcrun stapler validate`.
- **`macos_verify_gatekeeper <path>`** runs `spctl -a -vv -t exec`, which must report `source=Notarized Developer ID`. It runs only when notarization ran.

### 3.3 Order of operations

**GUI** (`release-gui.sh`, darwin targets):
1. `wails build`
2. `macos_sign`, inside out
3. `macos_verify_signature`
4. Zip the `.app` with `ditto` for submission, then `macos_notarize`
5. `macos_staple`
6. `macos_verify_gatekeeper`
7. Build the release zip from the **stapled** app, exactly as today: `ditto --norsrc --noextattr --noacl --keepParent`, followed by the AppleDouble guard.

**CLI** (`release-build.sh`, darwin targets):
1. `go build`
2. `macos_sign`
3. `macos_verify_signature`
4. Zip the bare binary for submission, then `macos_notarize`. A bare Mach-O can't hold a stapled ticket; Gatekeeper fetches it online the first time the binary runs.
5. `macos_verify_gatekeeper`
6. `tar.gz` as today, with the same layout and names.

On non-darwin targets nothing changes. Darwin CLI targets built on a non-macOS host (`release-build.sh` with no target list, run on Linux) have no `codesign`. They're built unsigned with a warning, and with `SIGN_REQUIRED=1` the build fails instead.

### 3.4 Failure behaviour

| Situation | Result |
|---|---|
| A required secret is missing and `SIGN_REQUIRED=1` | Fail with the name of the missing item. Never print its value. |
| Signing, verification, notarization or Gatekeeper check fails | The job fails, so `publish` never runs and nothing is released. |
| Notarization is rejected | The job log contains Apple's per-file reasons (from `notarytool log`). |
| Notarization takes longer than 30 minutes | The job fails. Re-running the workflow resubmits. |
| Local run without secrets | Signs with the keychain identity if `MACOS_SIGN_IDENTITY` is set, otherwise unsigned with a warning. Notarization is skipped. Artifact names and layout are the same either way. |

## 4. Verification

- **In CI:** the §3.2 checks run on every macOS artifact before it is packaged.
- **Locally, before merging:**
  - Run `MACOS_SIGN_IDENTITY="Developer ID Application: Chad Walker (SZUN8RDF5D)" scripts/release-gui.sh v0.0.0-test darwin/arm64`, and `release-build.sh` for both darwin targets.
  - Expect `codesign --verify --strict` to pass, the runtime flag and the Developer ID authority to show, and `spctl` to report the expected "Unnotarized Developer ID" (notarization is skipped locally).
  - `shellcheck` the new and changed scripts, and run `actionlint` on the workflow.
- **First real run:** the pre-release tag `v1.1.0-rc2` after merging. Check the published assets:
  - `spctl -a -vv -t exec` shows `source=Notarized Developer ID` for `Zenvik.app` from both darwin zips and for both darwin CLI binaries;
  - `stapler validate` passes for both apps;
  - the downloads match `SHA256SUMS`;
  - the user opens the downloaded app with no Gatekeeper prompt.

## 5. Documentation

- **`gui/README.md`:** replace the "not signed yet" note with "Signed with a Developer ID and notarized by Apple."
- **`README.md`:**
  - Replace the CLI's Gatekeeper/`xattr` paragraph with the same statement.
  - Drop "the macOS app isn't signed yet".
- **`CLAUDE.md`:**
  - The signing secrets and variable, and that they live in the `release` environment, which only `v*` tags can use.
  - `SIGN_REQUIRED`.
  - The local signed-build command.
  - Never put credential values in files, logs or output.
