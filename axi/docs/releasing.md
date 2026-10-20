# Releasing whatsapp-axi

**Nothing is published until a maintainer pushes a `vX.Y.Z` tag.** Pull requests, pushes to `main` and
manual runs of the Release workflow (dry run by default) only build and check. No workflow writes to the
Homebrew tap; the formula is published by hand (step 7) until a later workflow is agreed.

The release workflow uses only the default `GITHUB_TOKEN`. No signing keys or other secrets are involved:
whatsapp-axi is not signed or notarized (maintainer decision), so macOS users install it from source
through Homebrew or `go install`.

## What runs when

| Workflow | Runs on | Does |
|---|---|---|
| `ci.yml` job `test` | pull requests and pushes to `main` | `gofmt`, `go vet`, `go test` (race on Linux and macOS) on Linux, macOS, Windows |
| `ci.yml` job `package` | pull requests and pushes to `main` | `scripts/package.sh --dry-run 0.0.0-ci`, `scripts/check-dist.sh`, rebuilds and diffs `SHA256SUMS` (reproducibility), `scripts/test-homebrew-formula.sh` |
| `release.yml` | push of a tag matching `v[0-9]*` | tests, packages, smoke-runs each binary on its own OS/CPU, creates the GitHub release, attaches the Homebrew formula |
| `release.yml` (manual) | Actions > Release > Run workflow | `dry_run` ticked (default): same build and checks, files uploaded as a workflow artifact for 14 days, no release. Unticking `dry_run` only works when run from a tag and then releases that tag (for rerunning a failed release). |

Release jobs, in order:

1. **version**: takes the version from the tag (`v0.1.0` gives `0.1.0`), checks it is `X.Y.Z` or `X.Y.Z-pre`,
   and for a real release checks that the tagged commit is on `main`.
2. **test**: the same checks as CI on Linux, macOS and Windows.
3. **package**: `scripts/package.sh VERSION` (refuses a dirty tree or a HEAD not tagged `vVERSION`), then
   `scripts/check-dist.sh VERSION`. Uploads the files as the artifact `whatsapp-axi-VERSION`.
4. **smoke**: on linux amd64/arm64, macOS arm64/amd64 and windows amd64, checks the archive's checksum and
   that the binary's `--version` prints `VERSION`. (windows/arm64 has no hosted runner in this matrix; it
   is only checked by `check-dist.sh`.)
5. **release** (only with `contents: write`, only for a tag): refuses if a release for the tag already
   exists, verifies `SHA256SUMS`, runs `gh release create vVERSION --verify-tag` with the six archives and
   `SHA256SUMS` (marked prerelease when the version has a `-` suffix), then downloads GitHub's source
   tarball for the tag, renders `whatsapp-axi.rb` with `scripts/homebrew-formula.py` and uploads it to
   the release.

## Release files

`scripts/package.sh` cross-builds with `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X ..."`, setting
`internal/buildinfo.Version`, `Commit` (12 hex digits) and `Date` (the commit time, UTC). It writes
`dist/VERSION/`:

- `whatsapp-axi_VERSION_{darwin,linux}_{amd64,arm64}.tar.gz`
- `whatsapp-axi_VERSION_windows_{amd64,arm64}.zip`
- `SHA256SUMS`

Each archive holds `whatsapp-axi` (`whatsapp-axi.exe` on Windows), `README.md` and `LICENSE`. Archives are
reproducible for the same commit and Go toolchain (fixed file order, owner and mtime; gzip without
timestamp); CI checks this. The script runs the binary for the host's own OS/CPU with `--version`.

The macOS archives are built and published for completeness but are **not signed or notarized**. A
browser download is quarantined and Gatekeeper blocks it. macOS users should use the Homebrew formula or
`go install`; the README says so.

## Checklist

1. **Choose the version.** Semantic versioning, no leading `v` inside files: `0.1.0` for the first
   release; `0.1.0-rc.1` for a prerelease (GitHub marks it prerelease; Homebrew's livecheck ignores it).
   Before 1.0, bump the minor version for a changed output or flag, the patch version for fixes.
   Check the wasync release it needs is published (the README and `internal/compat/compatibility.json`
   name the minimum API and features).
2. **Check `main` is green** and you are on its latest commit:

   ```sh
   git switch main && git pull --ff-only
   gofmt -l . && go vet ./... && go test -race ./...
   ```

3. **Optional local dry run** (needs GNU tar; on macOS `brew install gnu-tar`):

   ```sh
   scripts/package.sh --dry-run 0.1.0 && scripts/check-dist.sh 0.1.0
   rm -rf dist
   ```

   Or run Actions > Release > Run workflow on `main` with `dry_run` ticked and download the artifact.
4. **Tag and push. This is the step that publishes.**

   ```sh
   git tag -a v0.1.0 -m "whatsapp-axi 0.1.0"
   git push origin v0.1.0
   ```

5. **Watch the Release workflow** (`gh run watch`). If any job before `release` fails, nothing was
   published: fix on `main`, delete the tag (`git push origin :refs/tags/v0.1.0 && git tag -d v0.1.0`) and
   tag again, or tag the next patch version.
6. **Verify the release.**

   ```sh
   mkdir /tmp/axi && cd /tmp/axi
   gh release download v0.1.0 -R gehariharan/whatsapp-axi
   sha256sum -c SHA256SUMS            # macOS: shasum -a 256 -c SHA256SUMS
   tar -xzf whatsapp-axi_0.1.0_linux_amd64.tar.gz && ./whatsapp-axi version
   ```

   `version` prints the version, commit and date. Check the release notes and that `whatsapp-axi.rb` is
   attached.
7. **Publish the Homebrew formula** to `gehariharan/homebrew-tap` by hand (no workflow does this yet):

   ```sh
   git clone git@github.com:gehariharan/homebrew-tap.git && cd homebrew-tap
   mkdir -p Formula
   gh release download v0.1.0 -R gehariharan/whatsapp-axi -p whatsapp-axi.rb -O Formula/whatsapp-axi.rb
   brew style Formula/whatsapp-axi.rb
   brew install --build-from-source ./Formula/whatsapp-axi.rb && brew test whatsapp-axi
   git add Formula/whatsapp-axi.rb && git commit -m "whatsapp-axi 0.1.0" && git push
   ```

   To regenerate it yourself: `curl -fsSL https://github.com/gehariharan/whatsapp-axi/archive/refs/tags/v0.1.0.tar.gz | sha256sum`,
   then `scripts/homebrew-formula.py 0.1.0 SHA256 Formula/whatsapp-axi.rb --commit $(git rev-parse --short=12 v0.1.0)`.
   Only touch `Formula/whatsapp-axi.rb`; `Casks/wasync.rb` and the tap README belong to wasync's release.
   The formula needs Homebrew's `go` to be at least the `go` version in `go.mod`.

   A later workflow may automate this; it would need a tap deploy key, which is a new secret and needs the
   maintainer's approval first.

## Rollback

- **Bad release, not yet installed widely:** `gh release delete v0.1.0 --cleanup-tag -R gehariharan/whatsapp-axi`
  removes the release and its tag. If the formula was published, revert its tap commit first.
- **Already installed:** do not reuse the version. Fix on `main` and release the next patch version
  (`0.1.1`); update the formula to it. Optionally mark the bad release as a prerelease or edit its notes to
  point at the fix.
- **Formula only is broken:** revert or fix `Formula/whatsapp-axi.rb` in the tap; the GitHub release is
  unaffected.
- Never move a published tag: the formula's `sha256` pins GitHub's tarball for that tag.
