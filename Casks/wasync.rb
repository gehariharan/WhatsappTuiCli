# Generated from release archive checksums. Installs a prebuilt executable.
require_relative "../lib/wasync_github_release_download_strategy"

cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.5"
  sha256 arm:   "647a6320747807e35ceb81ee8078ea71b7808bb0ba82958c683ff246eb848449",
         intel: "a3f979789f78280a0c968e7f46c80aa174ebe3d7807184da42daf767577124f6"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz",
      using: WasyncGitHubReleaseDownloadStrategy
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia

  depends_on formula: "gh"

  binary "wasync"
end
