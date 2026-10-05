# zenvik Scoop bucket — design

Status: approved in conversation 2026-10-05; this document records it.

## Goal

Windows users install zenvik with [Scoop](https://scoop.sh):

    scoop bucket add extras
    scoop bucket add chad3814 https://github.com/chad3814/scoop-bucket
    scoop install chad3814/zenvik        # CLI; pulls in MKVToolNix from Extras
    scoop install chad3814/zenvik-gui    # desktop app (bundles mkvmerge)

Every final zenvik release updates the bucket automatically, but only after
both packages have been proven to install on Windows. This follows the
Homebrew tap's pattern.

## Decisions

| Question | Decision |
|---|---|
| Package manager | Scoop now; winget later, as a separate piece; Chocolatey not planned |
| Where | Own bucket, public repo `chad3814/scoop-bucket` |
| CLI | Manifest `zenvik`: the release's Windows CLI zip; `bin: zenvik.exe`; `depends: extras/mkvtoolnix` |
| GUI | Manifest `zenvik-gui`: the release's Windows GUI zip, whole folder (bundled `mkvmerge.exe` and notices); Start-menu shortcut "Zenvik"; no MKVToolNix dependency |
| Which releases | Final tags only, `^v[0-9]+\.[0-9]+\.[0-9]+$` |
| How a bump lands | Pushed from zenvik's release workflow (render → check on Windows → push), not Scoop's scheduled updater |
| Credential | SSH deploy key with write access to the bucket only, secret `SCOOP_BUCKET_DEPLOY_KEY` in zenvik's `release` environment |
| Downgrades | Never: a shared version check (`scripts/newer-version.sh`) guards both the tap and the bucket |

Out of scope:
- winget;
- Windows code signing;
- ARM64 Windows builds;
- keeping zenvik's config inside the Scoop folder. The CLI and the app go
  on using `%AppData%\zenvik\config.toml`, and the app's queue stays in
  `%LocalAppData%\zenvik`. Uninstalling leaves both.

## Release artifacts (as of v1.2.0)

- `zenvik_<ver>_windows_amd64.zip` → `zenvik_<ver>_windows_amd64/` holding
  `zenvik.exe`, `LICENSE` and `README.md`.
- `zenvik-gui_<ver>_windows_amd64.zip` → `zenvik-gui_<ver>_windows_amd64/`
  holding `zenvik-gui.exe`, `mkvmerge.exe`, `LICENSE`, `README.md`,
  `MKVTOOLNIX-COPYING.txt`, `MKVTOOLNIX-NOTICE.txt` and
  `MKVTOOLNIX-LICENSES/`.
- Both are listed in the release's `SHA256SUMS`, as `<sha256>  <name>`.

## The bucket repo `chad3814/scoop-bucket`

    bucket/zenvik.json
    bucket/zenvik-gui.json
    check.ps1
    .github/workflows/ci.yml
    README.md

### bucket/zenvik.json

```json
{
    "version": "1.2.0",
    "description": "Remux Blu-ray and DVD disc images to MKV",
    "homepage": "https://github.com/chad3814/zenvik",
    "license": "MIT",
    "depends": "extras/mkvtoolnix",
    "architecture": {
        "64bit": {
            "url": "https://github.com/chad3814/zenvik/releases/download/v1.2.0/zenvik_1.2.0_windows_amd64.zip",
            "hash": "<sha256 from SHA256SUMS>",
            "extract_dir": "zenvik_1.2.0_windows_amd64"
        }
    },
    "bin": "zenvik.exe",
    "checkver": "github",
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "https://github.com/chad3814/zenvik/releases/download/v$version/zenvik_$version_windows_amd64.zip",
                "extract_dir": "zenvik_$version_windows_amd64"
            }
        },
        "hash": {
            "url": "https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS",
            "regex": "$sha256\\s+$basename"
        }
    }
}
```

### bucket/zenvik-gui.json

```json
{
    "version": "1.2.0",
    "description": "Desktop app to remux Blu-ray and DVD disc images to MKV",
    "homepage": "https://github.com/chad3814/zenvik",
    "license": "MIT,GPL-2.0-only",
    "notes": "Zenvik needs the Microsoft Edge WebView2 runtime, which Windows 11 includes. It bundles mkvmerge from MKVToolNix (GPLv2; see MKVTOOLNIX-NOTICE.txt in the app folder).",
    "architecture": {
        "64bit": {
            "url": "https://github.com/chad3814/zenvik/releases/download/v1.2.0/zenvik-gui_1.2.0_windows_amd64.zip",
            "hash": "<sha256 from SHA256SUMS>",
            "extract_dir": "zenvik-gui_1.2.0_windows_amd64"
        }
    },
    "shortcuts": [["zenvik-gui.exe", "Zenvik"]],
    "checkver": "github",
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "https://github.com/chad3814/zenvik/releases/download/v$version/zenvik-gui_$version_windows_amd64.zip",
                "extract_dir": "zenvik-gui_$version_windows_amd64"
            }
        },
        "hash": {
            "url": "https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS",
            "regex": "$sha256\\s+$basename"
        }
    }
}
```

The license reads `MIT,GPL-2.0-only`: zenvik is MIT, and the folder also
holds MKVToolNix's GPLv2 `mkvmerge.exe`.

### check.ps1

`check.ps1`, run from the bucket's root on Windows with Scoop installed, is
the one definition of "this bucket works". The bucket's CI runs it, and so
does zenvik's release before a bump is pushed. It stops at the first
failure, naming the step.

1. Add the `extras` bucket if it isn't already added.
2. Parse both manifests as JSON. Each must have `version`,
   `architecture.64bit.url`, `hash` and `extract_dir`, and both must have
   the same `version`.
3. `scoop install .\bucket\zenvik.json`.
   - `zenvik --version` must print `zenvik v<version>`.
   - `mkvmerge --version` must start with `mkvmerge v`; it comes from the
     Extras dependency, through Scoop's shims.
4. `scoop install .\bucket\zenvik-gui.json`.
   - `<scoop>\apps\zenvik-gui\current\mkvmerge.exe --version` must start
     with `mkvmerge v`.
   - `zenvik-gui.exe` must exist beside it. It isn't launched: CI has no
     desktop.
5. `scoop uninstall zenvik-gui` and `scoop uninstall zenvik`.
   `mkvtoolnix` stays installed: removing it isn't the check's job.

### .github/workflows/ci.yml

On push and pull request, on `windows-latest`, with `shell: pwsh`:
1. Install Scoop (`irm get.scoop.sh | iex`, passing `-RunAsAdmin`, since
   runners are admin), and add its shims folder to `GITHUB_PATH`.
2. An informational step with `continue-on-error: true`: before `extras`
   is added, try `scoop install .\bucket\zenvik.json` and log whether Scoop
   resolves `extras/mkvtoolnix` without the bucket being added. Then
   uninstall whatever it installed. This answers whether README's `scoop
   bucket add extras` line is needed. The line stays in README either way,
   because it's harmless.
3. `.\check.ps1`.

### README.md

Gives the four install commands from Goal, says what each package installs
(the CLI pulls in MKVToolNix; the app bundles mkvmerge and needs
WebView2), and notes that zenvik's release workflow updates the bucket.

## Automation in the zenvik repo

### scripts/scoop-render.sh

    scripts/scoop-render.sh <tag> <outdir>

Writes `<outdir>/bucket/zenvik.json` and `<outdir>/bucket/zenvik-gui.json`
for the tag, exactly in the shapes above, as JSON formatted with 4-space
indents.

- Downloads the release's `SHA256SUMS` from `ZENVIK_RELEASE_BASE_URL`
  (default `https://github.com/chad3814/zenvik`; tests use `file://`).
  Takes the two Windows zip lines by exact file name.
- Fails, writing nothing, if:
  - the tag isn't final (exit 2);
  - the download fails, either line is missing, or a hash isn't 64
    lowercase hex digits (exit 1).

  It writes into a temporary folder and moves the result into place at
  the end.
- URLs written into the files always point at
  `https://github.com/chad3814/zenvik`.

### scripts/scoop-render_test.sh

Offline, using `file://` fixtures:
- the version, URLs, `extract_dir` and each zip's hash land in the right
  manifest. The fixture lists the GUI line before the CLI line, to catch a
  swap.
- decoy lines for version `9.9.90` and for macOS DMGs are ignored;
- older files in `<outdir>` are replaced;
- a pre-release tag gives exit 2;
- a missing line, a malformed hash or a missing `SHA256SUMS` gives exit 1
  with `<outdir>` left empty;
- both outputs parse as JSON (`jq`, or `python3 -m json.tool` if `jq` is
  absent).

### scripts/newer-version.sh

    scripts/newer-version.sh vX.Y.Z <have-version>

The comparison now inside `homebrew-should-bump.sh`, moved out:
- exit 0 when `<have-version>` is empty, or the tag is newer;
- exit 3 when it isn't newer, with a message naming `<have-version>`;
- exit 2 on a usage error or a non-final tag.

`homebrew-should-bump.sh` keeps its interface and its tests, and calls
`newer-version.sh` for the comparison. `newer-version.sh` gets its own
test, `scripts/newer-version_test.sh`, covering numeric ordering (1.10 vs
1.9), equal versions, older versions, an empty current version and usage
errors.

### release.yml

Three new jobs, gated like the Homebrew ones (`if: ${{
!contains(github.ref_name, '-') }}` on the first; the others follow through
`needs:`):

1. **`scoop-render`** (`ubuntu-latest`, `needs: publish`): runs
   `scripts/scoop-render.sh "$GITHUB_REF_NAME" scoop` and uploads `scoop/`
   as artifact `scoop`.
2. **`scoop-check`** (`windows-latest`, `needs: scoop-render`, `shell:
   pwsh`): installs Scoop, clones `https://github.com/chad3814/scoop-bucket`
   read-only, copies the artifact's `bucket/` over the clone's, and runs
   the clone's `check.ps1`. It holds no credentials.
3. **`scoop-push`** (`ubuntu-latest`, `needs: scoop-check`, `environment:
   release`, `concurrency: { group: scoop-bucket-push, cancel-in-progress:
   false }`):
   - loads `SCOOP_BUCKET_DEPLOY_KEY` into `ssh-agent` (never written to
     the log or the workspace), with GitHub's host keys taken from
     `api.github.com/meta`;
   - clones `git@github.com:chad3814/scoop-bucket.git`;
   - reads the current version with `jq -r .version bucket/zenvik.json` and
     calls `scripts/newer-version.sh`. Exit 3 stops the job successfully;
   - copies the rendered `bucket/` in;
   - if anything changed, commits `zenvik X.Y.Z` as `github-actions[bot]`
     and pushes to `main`.

The Scoop jobs and the Homebrew jobs don't depend on each other. If any
job fails, the release stands, the bucket keeps its previous version, and
re-running the failed job retries.

### Docs

- **README.md:** the Scoop commands in the install section, beside the
  Homebrew line.
- **CLAUDE.md:** a bullet covering the bucket, `scoop-render.sh` and its
  test, `newer-version.sh`, the three jobs and `SCOOP_BUCKET_DEPLOY_KEY`.
  Never print or commit the key.

## Setup (once, during implementation)

1. **Create** `chad3814/scoop-bucket` (public), after asking. Its first
   commit holds the v1.2.0 manifests rendered by `scoop-render.sh`, plus
   `check.ps1`, CI and README. CI must pass on it. That proves both
   packages install from the real v1.2.0 release on Windows, and the
   informational step answers the Extras question.
2. **Add the deploy key** in one shell step, after asking:
   - `ssh-keygen -q -t ed25519 -N ''` into a temporary folder;
   - `gh repo deploy-key add --allow-write` for the public half on the
     bucket;
   - `gh secret set SCOOP_BUCKET_DEPLOY_KEY --env release` reading the
     private half from the file;
   - delete the folder.
3. **Merge and push** zenvik's branch, after asking.
4. **Watch the first automated bump** at the next final release: the three
   jobs, then the bucket's commit and its CI. The user can then try
   `scoop update; scoop install chad3814/zenvik` on Windows.

## Risks

- **Scoop on GitHub's Windows runners:** the installer refuses to run as
  admin unless passed `-RunAsAdmin`. Both workflows pass it.
- **`depends: extras/mkvtoolnix`** fails if Extras renames or drops
  `mkvtoolnix`. `check.ps1` would catch that before any push.
- **WebView2:** the app can't be launched in CI, so a WebView2 problem
  wouldn't show up there. The manifest's `notes` tell users about the
  runtime.
- **SmartScreen:** the binaries are unsigned, so Windows may warn on first
  run of either. That's out of scope here.
