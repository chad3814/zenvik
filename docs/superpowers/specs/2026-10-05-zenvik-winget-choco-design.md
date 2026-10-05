# zenvik on winget and Chocolatey — design

Status: approved in conversation 2026-10-05; this document records it.

## Goal

Windows users can install zenvik with any of the three common package
managers. Every final release updates all three after proving the packages
install:

    winget install chad3814.Zenvik           # CLI
    winget install chad3814.ZenvikGUI        # desktop app
    choco install zenvik                     # CLI
    choco install zenvik-gui                 # desktop app
    scoop install chad3814/zenvik            # (existing bucket; no longer needs Extras)

## Decisions

| Question | Decision |
|---|---|
| MKVToolNix on Windows | The Windows CLI zip bundles MKVToolNix's `mkvmerge.exe`, as the app zip already does. No package declares an MKVToolNix dependency. |
| mkvmerge lookup | `mkvmerge_path` from the config, then `mkvmerge.exe` beside the real (symlink-resolved) `zenvik.exe` on Windows, then PATH. The standard-install-folder fallback discussed earlier is dropped. |
| winget IDs | `chad3814.Zenvik` (CLI), `chad3814.ZenvikGUI` (app); publisher shown as Chad Walker |
| Chocolatey IDs | `zenvik`, `zenvik-gui` (both unclaimed as of 2026-10-05) |
| winget submission | Pull requests from the fork `chad3814/winget-pkgs` into `microsoft/winget-pkgs`, opened through GitHub's API. Auth is `WINGET_TOKEN`, a classic token in the `release` environment, already added; a probe showed it can open PRs upstream. No komac. |
| Chocolatey submission | `choco push` to the community feed with `CHOCO_API_KEY`, which the user adds to the `release` environment |
| Which releases | Final tags only, `^v[0-9]+\.[0-9]+\.[0-9]+$` |
| First submissions | At the next final release, v1.3.0, the first whose Windows CLI zip includes `mkvmerge.exe` |
| Scoop | Its CLI manifest drops `depends: extras/mkvtoolnix`; its `check.ps1` checks the bundled `mkvmerge.exe` |

Out of scope:
- MSI or MSIX installers (and with them, winget Start-menu shortcuts);
- Windows code signing, planned next;
- ARM64 Windows builds;
- automating or waiting on winget or Chocolatey moderation.

## Part 1 — the Windows CLI bundles mkvmerge

### Release build

`scripts/release-build.sh`, for the `windows/amd64` target:
- sources `third_party/mkvtoolnix.env` and runs `scripts/fetch-mkvmerge.sh
  windows/amd64 <tmp>`, the pinned, checksum-verified fetch the app build
  uses;
- runs `scripts/check-mkvtoolnix-source.sh "$MKVTOOLNIX_VERSION"`. When
  `CI=true` and the hosted source release is missing, it fails; locally it
  only warns. This matches `release-gui.sh`;
- copies `mkvmerge.exe`, `MKVTOOLNIX-COPYING.txt`, `MKVTOOLNIX-NOTICE.txt`
  and `MKVTOOLNIX-LICENSES/` into the stage beside `zenvik.exe`, `LICENSE`
  and `README.md`;
- leaves macOS and Linux archives unchanged.

The `cli` job in `release.yml` installs `7zip` with
`sudo apt-get install -y 7zip` before building. `fetch-mkvmerge.sh` uses
`7z` to unpack MKVToolNix's `.7z`.

### mkvmerge lookup (`internal/mux`)

`Find(ctx, path)` keeps its signature. When `path` is empty:
1. on Windows, it takes `os.Executable()`, resolves symlinks with
   `filepath.EvalSymlinks`, and uses `mkvmerge.exe` in that directory if
   it's a regular file;
2. otherwise it uses `exec.LookPath("mkvmerge")`, as before.

A non-empty `path` (the config's `mkvmerge_path`, or the GUI's own
resolution) is used as is. Resolving symlinks matters because winget's
portable installs reach `zenvik.exe` through a link in
`%LOCALAPPDATA%\Microsoft\WinGet\Links`. Scoop and Chocolatey shims launch
the real file.

`Mkvmerge` gains a `Source string` field: `"config"`, `"bundled"` or
`"PATH"`. `zenvik doctor` prints it beside the path and version.

The lookup is testable without Windows: the OS, the executable path and
the file-exists check are injected through package variables, as
`gui/mkvtoolnix.go` does.

### Scoop bucket

- `scripts/scoop-render.sh` drops `depends` from `zenvik.json`. Its test
  asserts there's no `depends` key.
- In the bucket repo, `check.ps1` stops adding Extras and checks the CLI's
  bundled `<scoop prefix zenvik>\mkvmerge.exe --version`. The README's
  `scoop bucket add extras` line goes, along with the CI's informational
  Extras step. That's a commit to the bucket, made with the user's
  approval.
- The bucket's manifests change at the next release, through the existing
  `scoop-push`.

## Part 2 — winget

### Manifests

`scripts/winget-render.sh <tag> <outdir>` writes, for version `<ver>`:

    <outdir>/manifests/c/chad3814/Zenvik/<ver>/chad3814.Zenvik.yaml
    <outdir>/manifests/c/chad3814/Zenvik/<ver>/chad3814.Zenvik.installer.yaml
    <outdir>/manifests/c/chad3814/Zenvik/<ver>/chad3814.Zenvik.locale.en-US.yaml
    <outdir>/manifests/c/chad3814/ZenvikGUI/<ver>/chad3814.ZenvikGUI.yaml
    <outdir>/manifests/c/chad3814/ZenvikGUI/<ver>/chad3814.ZenvikGUI.installer.yaml
    <outdir>/manifests/c/chad3814/ZenvikGUI/<ver>/chad3814.ZenvikGUI.locale.en-US.yaml

The schema is manifest 1.9.0. Each file starts with the
`# yaml-language-server: $schema=…` comment, and
`ManifestType: version | installer | defaultLocale`.

Installer manifest (CLI shown; the app swaps the names):

```yaml
PackageIdentifier: chad3814.Zenvik
PackageVersion: <ver>
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: zenvik_<ver>_windows_amd64\zenvik.exe
    PortableCommandAlias: zenvik
ReleaseDate: <publish date, YYYY-MM-DD>
Installers:
  - Architecture: x64
    InstallerUrl: https://github.com/chad3814/zenvik/releases/download/v<ver>/zenvik_<ver>_windows_amd64.zip
    InstallerSha256: <SHA-256 from SHA256SUMS, uppercase>
ManifestType: installer
ManifestVersion: 1.9.0
```

The app uses `zenvik-gui_<ver>_windows_amd64\zenvik-gui.exe` with alias
`zenvik-gui`. `ReleaseDate` is the UTC date the render runs (the release
was just published).

The locale manifest has:
- `PackageLocale: en-US`, `Publisher: Chad Walker`, and
  `PublisherUrl: https://github.com/chad3814`;
- `PackageName: zenvik` or `Zenvik`, `PackageUrl`, `License: MIT` with its
  `LicenseUrl`, and a `ShortDescription`;
- `ReleaseNotesUrl` pointing at the release, and `Moniker: zenvik` for the
  CLI only;
- for the app, `License: MIT, GPL-2.0 (bundled mkvmerge)`, and a
  `Description` that mentions WebView2 and the bundled `mkvmerge`.

The version manifest has `DefaultLocale: en-US`.

`winget-render.sh` takes hashes from the release's `SHA256SUMS`, like the
other render scripts. It honours `ZENVIK_RELEASE_BASE_URL` for tests,
refuses non-final tags (exit 2), and fails with exit 1 and nothing written
on any missing or malformed hash.

### Release jobs

- **`winget-render`** (`ubuntu-latest`, `needs: publish`, final tags only):
  renders into `winget/` and uploads the artifact `winget`.
- **`winget-check`** (`windows-latest`, `needs: winget-render`,
  `shell: pwsh`, no secrets):
  1. If `winget` isn't on the runner, install it:
     `Install-Module Microsoft.WinGet.Client -Force`, then
     `Repair-WinGetPackageManager -Latest`.
  2. `winget settings --enable LocalManifestFiles`.
  3. For each package directory, run `winget validate --manifest <dir>`,
     then `winget install --manifest <dir> --accept-package-agreements
     --accept-source-agreements --disable-interactivity`.
  4. CLI: `%LOCALAPPDATA%\Microsoft\WinGet\Links\zenvik.exe --version`
     must print `zenvik v<ver>`.
  5. App: the package folder must hold `zenvik-gui.exe`, and its bundled
     `mkvmerge.exe --version` must run. The package folder is found under
     `%LOCALAPPDATA%\Microsoft\WinGet\Packages\chad3814.ZenvikGUI_*`.
  6. Uninstall both with `winget uninstall --id … --disable-interactivity`.

  Every native command's exit code is checked.
- **`winget-submit`** (`ubuntu-latest`, `needs: winget-check`,
  `environment: release`, `concurrency: { group: winget-submit,
  cancel-in-progress: false }`, `GH_TOKEN: ${{ secrets.WINGET_TOKEN }}`).
  It does this through `scripts/winget-submit.sh <tag> <rendered dir>`, so
  the logic is testable. For each package:
  1. List the version directories under
     `manifests/c/chad3814/<Package>` in `microsoft/winget-pkgs`. A 404
     means a new package. If `newer-version.sh` says this version isn't
     newer than the highest existing one, skip it.
  2. If an open PR in `microsoft/winget-pkgs` has `head`
     `chad3814:<PackageIdentifier>-<ver>`, skip it.
  3. Read upstream `master`'s commit SHA. Create or reset
     `refs/heads/<PackageIdentifier>-<ver>` on `chad3814/winget-pkgs` at
     that SHA; fork networks share objects.
  4. Create one commit on that branch with the package's three files, using
     the Git Data API: blobs, a tree based on master's tree, the commit,
     then the ref update.
  5. Open the PR into `microsoft/winget-pkgs` `master`, from
     `chad3814:<PackageIdentifier>-<ver>`.
     - Its title is `New version: <PackageIdentifier> version <ver>`, or
       `New package: …` the first time.
     - Its body is winget-pkgs' PR checklist, with the boxes that apply
       ticked and a link to the release.

  The token never appears in logs. `gh` reads it from `GH_TOKEN`, and the
  script never echoes it.

## Part 3 — Chocolatey

### Packages

`scripts/choco-render.sh <tag> <outdir>` writes:

    <outdir>/zenvik/zenvik.nuspec
    <outdir>/zenvik/tools/chocolateyinstall.ps1
    <outdir>/zenvik-gui/zenvik-gui.nuspec
    <outdir>/zenvik-gui/tools/chocolateyinstall.ps1
    <outdir>/zenvik-gui/tools/chocolateyuninstall.ps1

**`.nuspec`:**
- identity: `id`, `version`, `title` (`zenvik` / `Zenvik`),
  `authors: Chad Walker`, `owners: chad3814`;
- links: `projectUrl`, `licenseUrl` (MIT on GitHub),
  `requireLicenseAcceptance: false`, `projectSourceUrl`, `docsUrl` (the
  README), `bugTrackerUrl` (issues), and `releaseNotes` (the release URL);
- `packageSourceUrl`: `https://github.com/chad3814/zenvik/blob/main/scripts/choco-render.sh`;
- `tags`: `bluray dvd mkv remux mkvmerge`;
- text: `summary` and `description`. The app's description mentions
  WebView2 and the bundled GPLv2 `mkvmerge`.

**`chocolateyinstall.ps1`:**
- `Install-ChocolateyZipPackage` with `-Url64bit` (the release zip),
  `-Checksum64` (from `SHA256SUMS`), `-ChecksumType64 sha256` and
  `-UnzipLocation $toolsDir`. Chocolatey records the unpacked files and
  removes them on uninstall.
- It then writes `mkvmerge.exe.ignore` beside the bundled `mkvmerge.exe`,
  so Chocolatey doesn't put a shim for it on PATH.
- For the app, it also writes `zenvik-gui.exe.gui`, which gives a windowed
  shim, and calls `Install-ChocolateyShortcut` for
  `Start Menu\Programs\Zenvik.lnk`.

The app's `chocolateyuninstall.ps1` removes that shortcut.

`choco-render.sh` follows the same rules as the other render scripts:
`SHA256SUMS` hashes, `ZENVIK_RELEASE_BASE_URL`, final tags only, and
nothing written on failure.

### Release jobs

- **`choco-render`** (`ubuntu-latest`, `needs: publish`, final tags only):
  renders into `choco/` and uploads the artifact `choco`.
- **`choco-check`** (`windows-latest`, `needs: choco-render`, `shell:
  pwsh`, no secrets). For each package:
  1. `choco pack` it into `nupkg/`.
  2. `choco install <id> --source "$PWD\nupkg" -y --no-progress`.
  3. CLI: `zenvik --version` (through Chocolatey's shim) must print
     `zenvik v<ver>`, `mkvmerge.exe` must exist beside the real
     `zenvik.exe` under `$env:ChocolateyInstall\lib\zenvik\tools\…`, and
     `$env:ChocolateyInstall\bin\mkvmerge.exe` must not exist.
  4. App: the shortcut must exist, and the bundled `mkvmerge.exe --version`
     must run.
  5. `choco uninstall <id> -y`.

  Afterwards it uploads `nupkg/*.nupkg` as the artifact `nupkg`. Every
  native command's exit code is checked.
- **`choco-push`** (`windows-latest`, `needs: choco-check`, `environment:
  release`, `concurrency: { group: choco-push, cancel-in-progress: false
  }`). For each `.nupkg`:
  1. Look up `https://community.chocolatey.org/api/v2/Packages(Id='<id>',Version='<ver>')`.
     A 200 means the version is already submitted, approved or in
     moderation, so skip it.
  2. Find the highest existing version with `FindPackagesById()`. If
     `newer-version.sh`'s comparison says this version isn't newer, skip
     it. The comparison is re-implemented in PowerShell from the same rule:
     equal or older means skip.
  3. Run `choco push <file> --source https://push.chocolatey.org/
     --api-key $env:CHOCO_API_KEY`. The key is only passed in the
     environment.

## Docs

- **README.md:** the install section lists winget, Chocolatey and Scoop
  (without the `extras` line). It says the Windows CLI includes
  `mkvmerge.exe` from MKVToolNix, under GPLv2, with the notices and source
  link in the zip.
- **CLAUDE.md:**
  - a bullet for winget: the render, check and submit scripts and jobs,
    and `WINGET_TOKEN`;
  - a bullet for Chocolatey: the render script, the jobs, and
    `CHOCO_API_KEY`;
  - the Bundled mkvmerge bullet updated to cover the Windows CLI.

  Never print or commit either credential.

## Testing

- **`internal/mux`:** the lookup order, with an injected OS, executable
  and file check:
  - a configured path wins over a bundled one;
  - on Windows, a bundled one wins over PATH;
  - the bundled step is skipped on other OSes;
  - a symlinked executable resolves to the real directory;
  - `Source` is set to match.
- **`scripts/release-build_test.sh`** (new): build the Windows target with
  a local MKVToolNix cache. It checks the zip holds `zenvik.exe`,
  `mkvmerge.exe`, `MKVTOOLNIX-NOTICE.txt`, `MKVTOOLNIX-COPYING.txt` and
  `MKVTOOLNIX-LICENSES/`, and that the Linux tarball has none of them.
  This needs the network unless `MKVTOOLNIX_CACHE` is primed, so it runs
  only with `ZENVIK_NET_TESTS=1`, like `fetch-mkvmerge_test.sh`, and in
  the release `cli` job, which builds the zip anyway.
- **Render scripts:** `scripts/winget-render_test.sh` and
  `scripts/choco-render_test.sh` are offline, using `file://` fixtures.
  They check:
  - the version, URLs and each hash in its own package;
  - the uppercase SHA-256 for winget;
  - nested paths and aliases, and that `.ignore` and `.gui` are written;
  - decoy lines, replacing older files, and the refusals;
  - that the YAML has the expected keys (read with `python3`'s line
    matching, so no YAML library is needed), and that the nuspec parses
    as XML;
  - for Scoop, the render test asserts there's no `depends`.
- **`scripts/winget-submit_test.sh`:** a fake `gh` on PATH records the
  calls and replies from fixtures. It covers:
  - a new package, which gets the "New package" title;
  - an existing older version, which gets "New version";
  - the same or a newer version upstream, which is skipped;
  - an existing open PR, which is skipped;
  - the branch and head names, the one-commit tree with exactly three
    files, and the token never being echoed.
- **On real Windows:** `winget-check` and `choco-check` in the release run
  at v1.3.0. Before that, the Scoop bucket's CI on its updated
  `check.ps1`.

## Setup

1. **`CHOCO_API_KEY`:** the user creates a community.chocolatey.org account
   and adds the API key as `CHOCO_API_KEY` in zenvik's `release`
   environment. I check that the names `WINGET_TOKEN` and `CHOCO_API_KEY`
   are there with `gh secret list --env release`, without reading values.
   `WINGET_TOKEN` is a classic token; `public_repo` scope is enough, and
   it's suggested if it currently has the full `repo` scope.
2. **Scoop bucket:** commit the `check.ps1`, README and CI changes, after
   asking.
3. **zenvik:** merge and push, after asking.
4. **v1.3.0:** tag it, after asking, and watch every job.
   - The Scoop bucket loses Extras.
   - Two winget PRs open. Their first review is by humans, and I link them
     for the user.
   - Two Chocolatey packages enter moderation; I link them.

## Risks

- **winget on the runner:** `windows-latest` may lack winget, so the check
  installs it with `Repair-WinGetPackageManager`. If that fails, the job
  fails before anything is submitted.
- **winget-pkgs PR conventions** (title, branch name, checklist) may
  change. The moderators' and bots' comments on the first PR will show
  that.
- **Chocolatey moderation** may ask for changes, such as metadata or an
  icon. Those are fixed in `choco-render.sh`, and a new version is pushed.
- **Bundling GPLv2 `mkvmerge.exe` in the CLI** brings the CLI zip under the
  same notice and source obligations as the app. Both are met by the
  existing notices and the hosted `mkvtoolnix-src-<ver>` release.
- **The token's breadth:** a classic token can push to the user's public
  repos. It's confined to the `release` environment and one job.
