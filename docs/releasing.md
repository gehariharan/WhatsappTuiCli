# Releasing wasync

Both `wasync` and `whatsapp-axi` ship from this repository. The root module owns
the TUI, WhatsApp connection and archive; the nested `axi` module owns the agent
CLI. They share a product release version while keeping their API boundary.

## Validation

PR CI runs core tests on Linux x64/ARM64, macOS Intel/Apple Silicon and Windows,
Windows installer/MSIX tests, AXI tests and reproducible packaging checks, the
real synthetic API/offline pair smoke test, and Homebrew publisher tests.

Locally use `bash scripts/check.sh` with Go 1.27.2 on PATH. CI and packaging
never use a paired account. Review both modules and run no-mistakes before merge.

## Preview publication

Main updates start `auto-release.yml`, except documentation and generated
Homebrew-only commits. It chooses one increasing preview version and invokes
the common `release.yml`. That workflow creates `vVERSION` and `axi/vVERSION`
at the same commit, tests/builds both binaries, executes native AXI smoke tests,
generates both source formulas from the root source archive, and creates a draft.

`auto-release.yml` then signs/notarizes both Mac binaries and the existing core
installers on the `wasync-signing` Mac runner. Certificates and notary credentials
stay in that runner's Keychain. The certificate organization and core installer
identifier are unchanged by the repository rename. AXI's signing identifier is
`com.gehariharan.wasynctuicli.axi`.

`publish-signed.yml` independently verifies the signed draft on both Mac CPUs,
checks the full asset checksum manifest and generated formula contents, and
publishes it. It updates `Casks/wasync.rb`, `Formula/wasync.rb` and
`Formula/whatsapp-axi.rb` on this same repository's main branch with the normal
workflow token, then runs Homebrew installation checks. A failed publishing step
can be retried; it must not change already published binaries or tags.

The root `release.yml` also supports a manually requested version or root tag,
producing a draft only. To publish such a draft, run the signing and verification
stages; do not bypass notarization by publishing it directly. Windows previews
retain their existing unsigned status. Linux core binaries target glibc 2.35+;
AXI binaries are static.

## Homebrew migration

The repository itself is the tap. New users and users of the old standalone
tap run:

```sh
brew tap --custom-remote gehariharan/tap https://github.com/gehariharan/wasync.git
```

On Linux, `brew install gehariharan/tap/whatsapp-axi` installs both source-built
tools. On macOS, install the signed core with `brew install --cask
gehariharan/tap/wasync`, then `brew install gehariharan/tap/whatsapp-axi`.
The source-built core formula is an alternative to the Mac cask, not a second
installation to use alongside it.

Retain previous release assets, user data directories, installer identifiers and
existing command names. Do not delete old repositories or reuse the former
`WhatsappTuiCli` name: GitHub redirects preserve old source and download links.
