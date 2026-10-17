# Generated from release archive checksums. Installs a prebuilt executable.
require_relative "../lib/wasync_github_release_download_strategy"

cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.4"
  sha256 arm:   "8bb0243ba8d3fe667f102ce767d3586da9132f717ee47eaebcb27972e33bc0ae",
         intel: "5f814ba2fbb264e032ef9ef479f2afccb09de85eef3c87aa514d6633964f46af"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz",
      using: WasyncGitHubReleaseDownloadStrategy
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia

  depends_on formula: "gh"

  binary "wasync"
end
