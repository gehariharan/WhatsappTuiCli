# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017104443"
  sha256 arm:   "ab54625e47a336a356804f594da078015806f65f630b87985e58264df3ea0d89",
         intel: "4eaa047c79e986c22eed708399e6e6eeaf0bc134a33c06293d593d057a1461c7"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
