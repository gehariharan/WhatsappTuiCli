# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017170000"
  sha256 arm:   "1a3fe9b02364db7aa02d1cba067e67380ed8d3b110a34fe16bf25782ed67c603",
         intel: "67982a6f3e760922a39a7c3095663164973ce62b34a0208bb3c498b5a2f7941d"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
