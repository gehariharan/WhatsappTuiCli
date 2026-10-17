# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017164052"
  sha256 arm:   "50e00d0c3d9ebb97d33444a601b29b0c16884c337afe264470d3ebf98bac6a8f",
         intel: "3f7b4225c88c32949eeb757032d6a3fbd8ee600019c28387caca0d65f1326034"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
