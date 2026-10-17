# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017094556"
  sha256 arm:   "971544009dadb909435262e53531f1cb56a851e3d77e33a00283bce10e4822de",
         intel: "cf177631d748b760161cbcc04cee62176f2df09dea542d2fb5d713c47996b100"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
