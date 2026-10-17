# wasync Homebrew tap

Prebuilt Intel and Apple Silicon executables for macOS 15 or later. No Go or C
compiler is required. Each Mac downloads its native architecture automatically.

This preview tap and its release assets are private. Use a GitHub account with
access to `gehariharan/homebrew-tap` and `gehariharan/WhatsappTuiCli`:

```sh
brew install gh
gh auth login
gh auth setup-git
brew tap gehariharan/tap
# Homebrew versions with tap trust:
brew trust --cask gehariharan/tap/wasync
brew install --cask gehariharan/tap/wasync
wasync version
wasync
```

For automated runners, pass a repository-scoped token as `GH_TOKEN`; the private
release download strategy delegates authentication to `gh`. No token goes in a
URL or cask. Homebrew verifies the downloaded archive's SHA-256 checksum.

Preview binaries have ad hoc signatures, not Apple Developer ID signatures, and
are not notarized. The normal install keeps Homebrew's quarantine protection.
If macOS blocks this unsigned preview, the explicit testing-only installation is:

```sh
brew install --cask --no-quarantine gehariharan/tap/wasync
```

That flag disables quarantine for this installation; it is not a signing fix.
Public production installers still require Developer ID signing and notarization.

Agent queries work alongside the TUI and return JSON:

```sh
wasync query groups --enabled
wasync query messages --group 'GROUP_ID' --limit 50
wasync query search 'meeting'
```

Quit capture before upgrading, then run `brew update` and
`brew upgrade --cask gehariharan/tap/wasync`. Uninstalling the cask preserves
per-user WhatsApp credentials, group preferences and the local archive. Do not use
`--zap` if you intend to retain your data (this cask defines no zap action).

The main application's release workflow updates this tap with a deploy key
restricted to this repository. Source and release notes:
https://github.com/gehariharan/WhatsappTuiCli
