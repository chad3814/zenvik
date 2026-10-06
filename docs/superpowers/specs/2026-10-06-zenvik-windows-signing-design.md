# zenvik Windows code signing — design

Status: approved in conversation 2026-10-06; this document records it.

## Goal

Every final release's Windows `zenvik.exe` and `zenvik-gui.exe` are
Authenticode-signed through SignPath Foundation's free open-source program,
so Windows names a publisher, "SignPath Foundation", instead of "Unknown
publisher". The work merges before SignPath accepts the project, and stays
dormant until it's configured.

## Decisions

| Question | Decision |
|---|---|
| Signing service | SignPath.io with a SignPath Foundation certificate (free for OSS). Its publisher name is "SignPath Foundation". |
| What's signed | `zenvik.exe` in `zenvik_<ver>_windows_amd64.zip` and `zenvik-gui.exe` in `zenvik-gui_<ver>_windows_amd64.zip`. The bundled `mkvmerge.exe` (MKVToolNix's build) is left unsigned, which SignPath's terms allow for upstream OSS binaries. |
| When | Every final tag, `^v[0-9]+\.[0-9]+\.[0-9]+$`. Pre-releases publish unsigned without waiting. |
| Approval | SignPath requires a person to approve each signing request. The release waits up to 4 hours in `windows-sign`; on timeout or rejection that job fails and nothing publishes. Re-running it retries. |
| Dormancy | Signing runs only when the repository variable `SIGNPATH_ORGANIZATION_ID` is set. Otherwise the unsigned zips pass straight through, and releases behave as before. |
| Credential | `SIGNPATH_API_TOKEN` (submitter permissions) in the `release` environment, read only by `windows-sign` |
| SignPath settings | Project slug `zenvik`; signing policy slug `release-signing`; the default artifact configuration, kept in the repo at `signing/signpath-artifact-configuration.xml` |
| SignPath action | `signpath/github-action-submit-signing-request@v3` |

Out of scope:
- signing pre-releases;
- signing `mkvmerge.exe`, which isn't ours;
- Azure Trusted Signing, a paid alternative that would carry the user's own name;
- macOS, which is already signed and notarized.

## SignPath's requirements (from signpath.org/terms and its GitHub docs, read 2026-10-06)

- An OSI-approved license with no proprietary components. zenvik is MIT, and the bundled `mkvmerge.exe` is GPL OSS.
- "Binary artifacts must be built from source code in a verifiable way", and every job leading up to the signing request runs on GitHub-hosted runners.
- "Each signing request must be approved by a team member."
- The homepage must carry a "Code signing policy": team roles, a privacy statement, and the line "Free code signing provided by SignPath.io, certificate by SignPath Foundation".
- The artifact to sign comes from an `actions/upload-artifact` step in the same workflow run, passed as `github-artifact-id`. upload-artifact zips it, so the artifact configuration's root is `<zip-file>`.
- "You may include unsigned binaries of upstream OSS projects … in your signed packages", though SignPath reserves the right to require signed files only in the future.

## Release workflow changes

### Artifact names

`publish` today downloads every artifact into one folder (`merge-multiple`).
The signed and unsigned Windows zips share file names, so the release
artifacts get a `release-` prefix, and `publish` downloads only
`pattern: release-*`:

| Job | Today | New |
|---|---|---|
| `cli` | `cli` (Linux `.tar.gz` and Windows `.zip`) | `release-cli` (Linux `.tar.gz`) and `windows-unsigned-cli` (Windows `.zip`) |
| `cli-darwin` | `cli-darwin` | `release-cli-darwin` |
| `gui-darwin` | `gui-darwin-<i>` | `release-gui-darwin-<i>` |
| `gui-other` (Linux) | `gui-other-<i>` | `release-gui-linux` |
| `gui-other` (Windows) | `gui-other-<i>` | `windows-unsigned-gui` |
| `windows-sign` (new) | — | `release-windows` (signed or passed-through zips) |

`gui-other`'s artifact name comes from its matrix entry: add `artifact:
release-gui-linux` and `artifact: windows-unsigned-gui` to the two matrix
`include` entries.

### `scripts/windows-sign-mode.sh <tag> <organization-id>`

Prints `sign` when the tag is final and the organization ID is non-empty.
Otherwise it prints `pass`, with a reason on stderr: "pre-release" or "not
configured". The decision lives in a script so it can be tested.

### `windows-sign` (new job)

- `runs-on: ubuntu-latest`, `needs: [cli, gui-other]`, `environment:
  release`, `timeout-minutes: 300`. Output `signed: true | false`.
1. `scripts/windows-sign-mode.sh "$GITHUB_REF_NAME" "${{
   vars.SIGNPATH_ORGANIZATION_ID }}"` sets the mode.
2. Download artifacts matching `windows-unsigned-*` into `unsigned/`, with
   `merge-multiple`.
3. Mode `pass`: upload `unsigned/*.zip` as `release-windows`, and set
   `signed=false`.
4. Mode `sign`:
   1. Upload `unsigned/*.zip` as `windows-to-sign`, taking the step's
      `artifact-id`.
   2. Run `signpath/github-action-submit-signing-request@v3` with:
      - `api-token: ${{ secrets.SIGNPATH_API_TOKEN }}`;
      - `organization-id: ${{ vars.SIGNPATH_ORGANIZATION_ID }}`;
      - `project-slug: zenvik`, `signing-policy-slug: release-signing`;
      - `github-artifact-id`: that ID;
      - `wait-for-completion: true`, with the action's completion timeout
        set to 4 hours (14400 seconds), if the action has that input;
      - `output-artifact-directory: signed`.
   3. Upload `signed/*.zip` as `release-windows`, and set `signed=true`.
   4. If `SIGNPATH_API_TOKEN` is empty, fail with a clear message.

### `windows-sign-check` (new job)

`runs-on: windows-latest`, `needs: windows-sign`, `shell: pwsh`. It
downloads `release-windows` and unpacks both zips.

- **Always:** `zenvik.exe --version` must run, and `zenvik-gui.exe` and
  both `mkvmerge.exe` copies must exist.
- **When `needs.windows-sign.outputs.signed == 'true'`:**
  `Get-AuthenticodeSignature` must give `Status: Valid` for `zenvik.exe`
  and `zenvik-gui.exe`, with a `SignerCertificate.Subject` containing
  `SignPath Foundation`.

The job always runs and passes when there's nothing to verify, so
`publish`'s `needs` never sees a skipped job.

### `publish`

- `needs: [cli, cli-darwin, gui-darwin, gui-other, windows-sign,
  windows-sign-check]`.
- Downloads artifacts with `pattern: release-*` and `merge-multiple: true`.
- Everything else stays the same: `SHA256SUMS` is computed over what was
  downloaded, so it covers the signed zips. The package-manager jobs read
  hashes from `SHA256SUMS` and need no change.

## Repository files

### `signing/signpath-artifact-configuration.xml`

The default artifact configuration to paste into SignPath:

```xml
<?xml version="1.0" encoding="utf-8"?>
<artifact-configuration xmlns="http://signpath.io/artifact-configuration/v1">
  <zip-file>
    <zip-file path="zenvik_*_windows_amd64.zip">
      <directory path="zenvik_*_windows_amd64">
        <pe-file path="zenvik.exe">
          <authenticode-sign />
        </pe-file>
      </directory>
    </zip-file>
    <zip-file path="zenvik-gui_*_windows_amd64.zip">
      <directory path="zenvik-gui_*_windows_amd64">
        <pe-file path="zenvik-gui.exe">
          <authenticode-sign />
        </pe-file>
      </directory>
    </zip-file>
  </zip-file>
</artifact-configuration>
```

The element names and wildcard rules are checked against SignPath's
artifact-configuration reference while writing the plan. `mkvmerge.exe`
isn't listed, so it passes through unchanged.

### `signing/README.md`

The one-time setup, in order:
1. Make sure the README's code signing policy is published. SignPath's
   reviewers look for it.
2. Apply at https://signpath.org/apply.
3. Once accepted, in SignPath:
   - create the project `zenvik` and paste the artifact configuration as
     its default;
   - use the `release-signing` policy, with the user as approver;
   - link the predefined GitHub.com connector to the project, and install
     the SignPath GitHub app on `chad3814/zenvik`;
   - create a submitter API token.
4. In GitHub, add the `release` environment secret `SIGNPATH_API_TOKEN` and
   the repository variable `SIGNPATH_ORGANIZATION_ID`. The variable is what
   turns signing on.

It also says what each final release then needs: approve the request in
SignPath within 4 hours. If approval doesn't come in time, re-run
`windows-sign` (and the jobs after it) from the release run.

### README: "Code signing policy" section

- "Free code signing provided by [SignPath.io](https://about.signpath.io),
  certificate by [SignPath Foundation](https://signpath.org)."
- **What's signed:**
  - `zenvik.exe` and `zenvik-gui.exe` in each final release's Windows
    downloads, built by this repository's GitHub Actions release workflow
    from the tagged source;
  - the bundled `mkvmerge.exe` is MKVToolNix's own build, included
    unchanged and not signed by this project.
- **Team roles:** Chad Walker ([@chad3814](https://github.com/chad3814)) is
  committer and author, reviewer, and approver. Every signing request is
  approved by hand.
- **Privacy:** zenvik and the Zenvik app send nothing about you, your files
  or your discs. Once a day they ask for the latest version number, and the
  request carries the program's version in its User-Agent. They ask:
  - GitHub's API (`api.github.com/repos/chad3814/zenvik/releases/latest`);
  - or, for winget and Chocolatey installs, that package manager's own
    listing: winget's manifest folder on GitHub, or Chocolatey's community
    feed.

  `update_check = false` in the config file, or the `ZENVIK_NO_UPDATE_CHECK`
  environment variable, turns this off. Nothing else connects to the network
  unless you ask it to.

### CLAUDE.md

A bullet covering:
- the `windows-sign` and `windows-sign-check` jobs, `signing/`, and
  `scripts/windows-sign-mode.sh` with its test;
- `SIGNPATH_API_TOKEN` and `SIGNPATH_ORGANIZATION_ID`;
- the README policy section;
- the `release-` artifact prefix that `publish` relies on.

Never print the token.

## Testing

- **`scripts/windows-sign-mode_test.sh`:**
  - final tag with an ID → `sign`;
  - final tag without an ID → `pass` ("not configured");
  - `-rc` tag with an ID → `pass` ("pre-release");
  - usage errors.
- **actionlint** on the workflow.
- **An XML well-formedness check** of the artifact configuration
  (`python3 -c 'import xml.dom.minidom …'`).
- **The next release before SignPath is configured** proves the
  pass-through and the renamed artifacts. Its downloads and `SHA256SUMS`
  must match a normal release, and every package-manager job must pass.
- **The first signed release** proves signing. `windows-sign-check` must
  verify both signatures and `zenvik.exe` must run before `publish`.

## Risks

- **Approval latency:** a release waits for a person. If the 4-hour limit
  passes, re-run the job.
- **A SignPath outage** blocks final releases while signing is configured.
  Unsetting `SIGNPATH_ORGANIZATION_ID` returns to unsigned releases until
  it's fixed.
- **SignPath may later require every PE file in a package to be signed,**
  including `mkvmerge.exe`. That would need upstream-signed builds, or
  listing `mkvmerge.exe` for signing if their policy allows signing
  third-party OSS.
- **SmartScreen:** a signed binary still builds reputation from download
  numbers, so early users may see a warning.
- **Renaming artifacts** touches every job that downloads them. Only
  `publish` downloads release artifacts by pattern; the package-manager jobs
  download their own render artifacts, which are unchanged.
