# Generated from release archive checksums. Installs a prebuilt executable.
cask "wasync" do
  arch arm: "arm64", intel: "amd64"

  version "0.1.0-preview.20261017232722"
  sha256 arm:   "64d6e8a19a480ad007e3c4d4b57942c3cdcac3bba83bdd429043af4b4eeeb43f",
         intel: "8da73222c7ff955328b60967f79fdee0e8d9d71f84cc8859418dad40712969a4"

  url "https://github.com/gehariharan/WhatsappTuiCli/releases/download/v#{version}/wasync_#{version}_darwin_#{arch}.tar.gz"
  name "WhatsappTuiCli"
  desc "Local WhatsApp group archive with a terminal UI and agent query CLI"
  homepage "https://github.com/gehariharan/WhatsappTuiCli"

  depends_on macos: :sequoia


  binary "wasync"
end
