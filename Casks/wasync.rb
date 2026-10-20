# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261020202713"
  sha256 arm:   "ede8c42227d602341956f836200e3b74d08865f08dd6dbcab8d7880531e2ca53",
         intel: "cbb4e180bf75ea846a857af336708fd9714df014928ae6734b09e953a9c798b3"

  url "https://github.com/gehariharan/wasync/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "wasync"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/wasync"

  depends_on macos: :sequoia


  binary "wasync"
end
