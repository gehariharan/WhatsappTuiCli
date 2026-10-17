# Generated from release archive checksums. Installs a prebuilt executable.
require_relative "../lib/wasync_github_release_download_strategy"

cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017075203"
  sha256 arm:   "d453c263043e351619d1f9be31ebc07a1f5039aaf177bb6b46457227518de261",
         intel: "0d62203ca3d2c0f2fdb5dfe2cc4f96f189878db87272be1b60db60d1f3d0c979"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz",
      using: WasyncGitHubReleaseDownloadStrategy
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia

  depends_on formula: "gh"

  binary "wasync"
end
