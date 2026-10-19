# wasync Homebrew tap

Prebuilt Intel and Apple Silicon executables for macOS 15 or later. No Go or C
compiler is required. Each Mac downloads its native architecture automatically.

The tap and release assets are public. No GitHub account or compiler is needed:

```sh
brew tap gehariharan/tap
# Homebrew versions with tap trust:
brew trust --cask gehariharan/tap/wasync
brew install --cask gehariharan/tap/wasync
wasync version
wasync
```

Homebrew verifies the downloaded archive's SHA-256 checksum. Public downloads
use ordinary HTTPS without authentication. The legacy private-release download
helper remains available for private forks; the public cask does not use it.

Releases from `0.1.0-preview.5` onward are Developer ID signed and notarized.
The cask keeps Homebrew's normal protection; no hook changes quarantine or
Gatekeeper policy. Earlier unsigned/ad hoc previews may be blocked by macOS;
upgrade to a signed release.

The E2E workflow has an explicit unsigned-preview test option limited to
GitHub-hosted runners. It is not part of this installer and does not prove that
an unsigned download will run on an unmodified user's Mac.

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
restricted to this repository. Pushes to the application's `main` branch build,
sign, notarize, verify and publish new preview installers automatically, then
update this tap and run Homebrew E2E on Intel and Apple Silicon. Signing uses
the maintainer's Mac runner, which must be online and logged in.
Source and release notes:
https://github.com/gehariharan/WhatsappTuiCli

<!-- scripts/publish-tap.sh copies this file only when the tap has no README.md.
After that the tap README is maintained in the tap repository so it can
document every cask (wasync and whatsapp-axi). -->
