# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261019055444"
  sha256 arm:   "5b7597cb028ce7136b5ddd13f1f74a4dcb64ee088426dbec153f3cc77a8d5194",
         intel: "c3ca87c7e725689aeecea42b37c819e02bc301bfdf3d510ef9e6b46ad9c853c2"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
