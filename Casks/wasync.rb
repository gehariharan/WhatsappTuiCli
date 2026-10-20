# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261020015629"
  sha256 arm:   "9f2c83f2dbf98eadd3869c6bcd9ff2e094c2856bd0cc04d24791f812e225bd65",
         intel: "09363ba80687e46d5ee567bc270acdd2213103c1a33053ddf2de8883b5b2b938"

  url "https://github.com/gehariharan/wasync/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "wasync"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/wasync"

  depends_on macos: :sequoia


  binary "wasync"
end
