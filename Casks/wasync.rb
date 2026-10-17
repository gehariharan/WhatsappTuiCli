# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017181519"
  sha256 arm:   "09760fab6ed335ff9ed0ccf6ab5b858f67dd5dc7b220ccdaeb0aa08fa19b6609",
         intel: "dfb3c814a2e63c8a3caefd5a1d64c423af67b26a62b2df79774f20a2dbe5b40e"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
