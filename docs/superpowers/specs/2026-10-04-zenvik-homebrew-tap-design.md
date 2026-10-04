# zenvik Homebrew tap — design

Status: approved in conversation 2026-10-04; this document records it.

## Goal

People install zenvik with Homebrew:

    brew install chad3814/tap/zenvik              # CLI; pulls in MKVToolNix
    brew install --cask chad3814/tap/zenvik-gui   # desktop app (bundles mkvmerge)

Every final zenvik release updates the tap automatically, after the new
formula and cask have been proven to install.

## Decisions

| Question | Decision |
|---|---|
| Where | Own tap, public repo `chad3814/homebrew-tap` (homebrew-core is out of reach until the project is notable) |
| CLI | Formula `zenvik`, built from the tagged source with Go, `depends_on "mkvtoolnix"` |
| GUI | Cask `zenvik-gui` (not `zenvik`, so formula and cask never collide), installs the release DMG |
| Which releases | Final tags only, `^v[0-9]+\.[0-9]+\.[0-9]+$`; pre-releases (`-rc1`) never touch the tap |
| Bump credential | SSH deploy key with write access to the tap only, secret `HOMEBREW_TAP_DEPLOY_KEY` in the zenvik `release` environment |
| How a bump lands | Checked, then pushed straight to the tap's `main`; no pull request |
| Platforms | Formula: macOS and Linux. Cask: macOS 13+ |

Out of scope: homebrew-core, bottles (prebuilt formula binaries, so the
formula always compiles), Windows package managers.

## The tap repo `chad3814/homebrew-tap`

    Formula/zenvik.rb
    Casks/zenvik-gui.rb
    check.sh                    # the one definition of "this tap works"
    .github/workflows/ci.yml
    README.md

### Formula/zenvik.rb

```ruby
class Zenvik < Formula
  desc "Remux Blu-ray and DVD disc images to MKV"
  homepage "https://github.com/chad3814/zenvik"
  url "https://github.com/chad3814/zenvik/archive/refs/tags/v1.1.1.tar.gz"
  sha256 "<sha256 of that tarball>"
  license "MIT"
  head "https://github.com/chad3814/zenvik.git", branch: "main"

  livecheck do
    url :stable
    strategy :github_latest
  end

  depends_on "go" => :build
  depends_on "mkvtoolnix"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=v#{version}"), "./cmd/zenvik"
  end

  test do
    assert_match "zenvik v#{version}", shell_output("#{bin}/zenvik --version")
    assert_match "no such file", shell_output("#{bin}/zenvik info #{testpath}/missing.iso 2>&1", 1)
  end
end
```

The version stamp matches `scripts/release-build.sh` (`-X main.version=$tag`),
so `zenvik --version` reads the same whether installed by Homebrew or from a
release archive. The test needs no disc: it proves the binary starts, knows
its version, and fails cleanly (exit 1) on a missing image.

### Casks/zenvik-gui.rb

```ruby
cask "zenvik-gui" do
  arch arm: "arm64", intel: "amd64"

  version "1.1.1"
  sha256 arm:   "<sha256 of zenvik-gui_1.1.1_darwin_arm64.dmg>",
         intel: "<sha256 of zenvik-gui_1.1.1_darwin_amd64.dmg>"

  url "https://github.com/chad3814/zenvik/releases/download/v#{version}/zenvik-gui_#{version}_darwin_#{arch}.dmg"
  name "Zenvik"
  desc "Desktop app to remux Blu-ray and DVD disc images to MKV"
  homepage "https://github.com/chad3814/zenvik"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: :ventura

  app "Zenvik.app"

  zap trash: [
    "~/Library/Caches/dev.cwalker.zenvik",
    "~/Library/Caches/zenvik/gui-queue.json",
    "~/Library/HTTPStorages/dev.cwalker.zenvik",
    "~/Library/Preferences/dev.cwalker.zenvik.plist",
    "~/Library/Saved Application State/dev.cwalker.zenvik.savedState",
    "~/Library/WebKit/dev.cwalker.zenvik",
  ]
end
```

The SHA-256s are the ones in the release's `SHA256SUMS`. No MKVToolNix
dependency: the app bundles mkvmerge. `zap` removes only the app's own state.
It keeps `~/Library/Application Support/zenvik/config.toml`, which the CLI
shares, and the rest of `~/Library/Caches/zenvik`, where the CLI keeps its
mount records.

### check.sh

`check.sh [--formula-only]`, run from the tap's root, proves the tap's
current files work. It is used both by the tap's own CI and by the zenvik
release before a bump is pushed:

1. Links the working tree in as the tap: a symlink
   `$(brew --repository)/Library/Taps/chad3814/homebrew-tap` → the checkout,
   so uncommitted files are what get checked.
2. Formula: `brew style`, `brew audit --strict --online --formula
   chad3814/tap/zenvik`, `brew install --build-from-source
   chad3814/tap/zenvik`, `brew test chad3814/tap/zenvik`.
3. Cask (macOS, unless `--formula-only`): `brew audit --strict --online --cask
   chad3814/tap/zenvik-gui`, `brew install --cask chad3814/tap/zenvik-gui`.
   Then `spctl -a -vv -t exec` on the installed `Zenvik.app` must report
   `source=Notarized Developer ID`, and its
   `Contents/Helpers/mkvmerge --version` must run. Finally `brew uninstall
   --cask --zap chad3814/tap/zenvik-gui`.
4. Exits non-zero at the first failure, naming the step.

### .github/workflows/ci.yml

On push and pull request: `check.sh` on `macos-latest`, and
`check.sh --formula-only` on `ubuntu-latest` (Homebrew set up with
`Homebrew/actions/setup-homebrew@main`).

### README.md

The two install commands, what each installs (the CLI pulls in MKVToolNix;
the app bundles mkvmerge), and a note that the files are updated
automatically by zenvik's release workflow.

## Bump automation (zenvik repo)

### scripts/homebrew-render.sh

    scripts/homebrew-render.sh <tag> <outdir>

Writes `<outdir>/Formula/zenvik.rb` and `<outdir>/Casks/zenvik-gui.rb` for
the tag, exactly in the shapes above.

- Downloads `archive/refs/tags/<tag>.tar.gz` and hashes it. Downloads the
  release's `SHA256SUMS` and takes the two `zenvik-gui_<ver>_darwin_*.dmg`
  lines from it, so the cask's hashes are the published ones.
- `ZENVIK_RELEASE_BASE_URL` (default `https://github.com/chad3814/zenvik`)
  overrides where it downloads from, so tests can serve local `file://`
  fixtures. The URLs written into the files always point at
  `https://github.com/chad3814/zenvik`.
- Fails, writing nothing, if:
  - the tag isn't a final release (exit 2);
  - a download fails;
  - `SHA256SUMS` lacks either DMG line;
  - a hash isn't 64 hex digits.

  The files are written to a temporary folder and moved into place only at
  the end.

### scripts/homebrew-render_test.sh

Offline cases, using a `file://` base URL with a fixture tarball and
`SHA256SUMS`:

- the version, URLs, source hash and both DMG hashes land in the right
  places;
- a pre-release tag is refused with exit 2;
- a missing DMG line fails, leaving `<outdir>` empty;
- a malformed hash fails;
- a missing tarball fails.

When `brew` is on `PATH`, `brew style` must pass on the rendered files.

### release.yml

Three new jobs, all gated on `if: ${{ !contains(github.ref_name, '-') }}`
(the release workflow only runs for `v*` tags):

1. **`homebrew-render`** (ubuntu-latest, `needs: publish`): runs
   `scripts/homebrew-render.sh "$GITHUB_REF_NAME" homebrew` and uploads
   `homebrew/` as an artifact.
2. **`homebrew-check`** (matrix: macos-latest full, ubuntu-latest
   `--formula-only`; `needs: homebrew-render`):
   - clones the tap read-only over HTTPS;
   - copies the rendered files over the clone's;
   - runs the tap's own `check.sh`.

   It holds no credentials.
3. **`homebrew-push`** (ubuntu-latest, `needs: homebrew-check`,
   `environment: release`):
   - loads `HOMEBREW_TAP_DEPLOY_KEY` into `ssh-agent`; the key is never
     written to the log or the workspace;
   - clones the tap over SSH and copies the rendered files in;
   - if anything changed, commits `zenvik X.Y.Z` as `github-actions[bot]`
     and pushes to `main`;
   - if nothing changed, does nothing.

   Only this job can reach the secret.

If any of these fail, the GitHub release (already published) stands and the
tap keeps the previous version. Re-running the failed job retries. A
manually triggered workflow is deliberately absent: the `release`
environment only deploys from `v*` tags, and an old tag doesn't contain a
new workflow file.

### Docs

- **README.md:** the Homebrew commands in the install section, for both the
  CLI and the app.
- **CLAUDE.md:** a bullet covering the tap, `homebrew-render.sh` and its
  test, the three jobs, and the `HOMEBREW_TAP_DEPLOY_KEY` secret. Never print
  or commit the key.

## Setup (once, during implementation)

1. **Create** `chad3814/homebrew-tap` (public) with the files above. The
   formula and cask are for v1.1.1, rendered by `homebrew-render.sh`. The
   first push runs the tap's CI, which must pass, proving both install from
   the real v1.1.1 release.
2. **Add the deploy key**, in one shell step with no key material in output:
   - `ssh-keygen -t ed25519 -N '' -C zenvik-release` into a temporary folder;
   - `gh repo deploy-key add --allow-write` for the public half on the tap;
   - `gh secret set HOMEBREW_TAP_DEPLOY_KEY --env release -R chad3814/zenvik`
     reading the private half from the file;
   - delete the folder.
3. **Wait for the first automated bump**, at the next final release. Watch
   the three jobs, then confirm the tap's commit, its CI, and that
   `brew upgrade` installs the new version.

## Risks

- **GitHub source tarballs** are generated on demand. Their bytes have
  changed once before, in 2023, which broke pinned hashes. The formula's
  hash is taken right after the release, and the tap's CI would catch a
  later change; re-rendering fixes it.
- **Homebrew's Go** must keep up with zenvik's `go` directive (1.27 now;
  Homebrew has 1.27.1). If a future zenvik requires a newer Go than Homebrew
  has, the formula install fails in `homebrew-check`, before any push.
- **MKVToolNix in Homebrew** pulls in Qt, so the macOS check job takes
  several minutes. That's accepted; it runs after the release is already
  published.
