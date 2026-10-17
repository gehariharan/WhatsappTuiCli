# Generated from release archive checksums. Installs a prebuilt executable.
require_relative "../lib/wasync_github_release_download_strategy"

cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017074247"
  sha256 arm:   "ad93408960acff4ebe9388eb3f17555b22cc4daef8b29ca1d4f28ba655166f33",
         intel: "3536448ac2eae7be1dda48863e31574e802648d54c3f0677d6e0efc494f65e7d"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz",
      using: WasyncGitHubReleaseDownloadStrategy
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia

  depends_on formula: "gh"

  binary "wasync"
end
