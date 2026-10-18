# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261018060349"
  sha256 arm:   "9a0f3e5636f856678888a1408acc8479e0bb9e6e423b3d48064a86498c271bfb",
         intel: "7b5e899c2ff8697b7e078b6dc2cfc30c443031a939877176e06bdd1e8bbdd051"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
