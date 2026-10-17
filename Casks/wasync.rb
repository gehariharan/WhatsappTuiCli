# Generated from release archive checksums. Installs a prebuilt executable.
require_relative "../lib/wasync_github_release_download_strategy"

cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.3"
  sha256 arm:   "1b53ca8a16644cae77681e209ce23ef3dfdb668a08ed5a12b27a675c8df9292c",
         intel: "39635e9abaee1d352ab398e0c357e3db6c9b2ca4c6780a7db8289062513a31c6"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz",
      using: WasyncGitHubReleaseDownloadStrategy
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia

  depends_on formula: "gh"

  binary "wasync"
end
