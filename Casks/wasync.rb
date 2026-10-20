# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261020012622"
  sha256 arm:   "ba6ac223df5c00a514710013dfd624b307ecdd20c1e1690168588e7b3d2b2800",
         intel: "08620104b8bf896e39fd7e4b0f485e77f6e4f1712f7866bb9598bf51542ec06c"

  url "https://github.com/gehariharan/wasync/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "wasync"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/wasync"

  depends_on macos: :sequoia


  binary "wasync"
end
