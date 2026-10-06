# Windows code signing (SignPath Foundation)

Final releases' Windows `zenvik.exe` and `zenvik-gui.exe` are signed through [SignPath.io](https://about.signpath.io), with a certificate from [SignPath Foundation](https://signpath.org). The release workflow's `windows-sign` job does it once the repository variable `SIGNPATH_ORGANIZATION_ID` is set; until then releases publish unsigned. Pre-releases are never signed.

## One-time setup

1. Make sure the README's "Code signing policy" section is published on `main`. SignPath's reviewers look for it.
2. Apply at https://signpath.org/apply.
3. Once accepted, in SignPath:
   - create the project with slug `zenvik`;
   - paste [`signpath-artifact-configuration.xml`](signpath-artifact-configuration.xml) as its default artifact configuration;
   - use the signing policy with slug `release-signing`, with yourself as approver;
   - link the predefined **GitHub.com** trusted build system to the project, and install the SignPath GitHub app on `chad3814/zenvik`;
   - create an API token for a user with submitter permissions.
4. In GitHub (`chad3814/zenvik` → Settings):
   - Environments → `release` → add the secret `SIGNPATH_API_TOKEN`;
   - Variables → add the repository variable `SIGNPATH_ORGANIZATION_ID` (your SignPath organization ID). This turns signing on.

## Every final release

1. `windows-sign` submits the Windows zips and waits.
2. SignPath emails you: approve the request in its web app within **4 hours**.
3. The signed zips go to `windows-sign-check`, which verifies the signatures on Windows, and then to `publish`.

If the request isn't approved in time, or is rejected, `windows-sign` fails and nothing publishes. Re-run the failed jobs from the release run to submit again.

To fall back to unsigned releases (for example during a SignPath outage), delete or empty `SIGNPATH_ORGANIZATION_ID`.

If you change `scripts/release-build.sh` or `scripts/release-gui.sh` so the zips' folder names change, update the artifact configuration here and in SignPath. `scripts/signpath-config_test.sh` checks this copy.
